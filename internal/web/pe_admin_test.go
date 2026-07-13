package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

// csrfFromCookies returns the openfsd_csrf cookie value from a jar.
func csrfFromCookies(cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			return c.Value
		}
	}
	return ""
}

// authedGET performs GET with session cookies and merges any Set-Cookie.
func authedGET(t *testing.T, ts *testServer, path string, cookies []*http.Cookie) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	return w, mergeCookies(cookies, w.Result())
}

// formPOST posts application/x-www-form-urlencoded with CSRF + session cookies.
// Does not send Authorization header (cookie+CSRF path).
func formPOST(t *testing.T, ts *testServer, path string, form url.Values, cookies []*http.Cookie) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	csrf := csrfFromCookies(cookies)
	if csrf == "" {
		// Ensure CSRF by hitting a gated page first when possible.
		var w *httptest.ResponseRecorder
		w, cookies = authedGET(t, ts, "/dashboard", cookies)
		_ = w
		csrf = csrfFromCookies(cookies)
	}
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", csrf)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	// Explicitly no Authorization header — task 6 requirement.
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	return w, mergeCookies(cookies, w.Result())
}

func TestNoJSUserCreateAndLoad(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	// GET usereditor must be server-rendered (forms present).
	w, cookies := authedGET(t, ts, "/usereditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /usereditor status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `method="post" action="/usereditor/create"`) {
		t.Fatalf("expected create form POST action, body=%s", clip(body, 500))
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatal("expected csrf_token in usereditor")
	}

	// Create via form POST without Authorization header.
	form := url.Values{}
	form.Set("first_name", "Alice")
	form.Set("last_name", "Smith")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	w, cookies = formPOST(t, ts, "/usereditor/create", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/usereditor?cid=") || !strings.Contains(loc, "flash=created") {
		t.Fatalf("unexpected redirect Location=%q", loc)
	}

	// Follow redirect: edit form should show created user (server-rendered).
	w, cookies = authedGET(t, ts, loc, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("follow create redirect status %d", w.Code)
	}
	body = w.Body.String()
	if !strings.Contains(body, "User created successfully") {
		t.Fatalf("expected flash success, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, `value="Alice"`) {
		t.Fatalf("expected first name in edit form, body=%s", clip(body, 600))
	}
	if !strings.Contains(body, `value="Smith"`) {
		t.Fatal("expected last name in edit form")
	}
}

func TestNoJSUserUpdate(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	target := createTestUser(t, ts, "target-pass", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	form := url.Values{}
	form.Set("cid", itoa(target.CID))
	form.Set("first_name", "Updated")
	form.Set("last_name", "Name")
	form.Set("network_rating", "2")
	form.Set("password", "") // keep current
	w, cookies := formPOST(t, ts, "/usereditor/update", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("update status %d body %s", w.Code, w.Body.String())
	}

	w, _ = authedGET(t, ts, w.Header().Get("Location"), cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("follow update status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "User updated successfully") {
		t.Fatalf("expected update flash, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, `value="Updated"`) {
		t.Fatal("expected updated first name")
	}

	// Persist check
	u, err := ts.dbRepo.UserRepo.GetUserByCID(target.CID)
	if err != nil {
		t.Fatal(err)
	}
	if safeStr(u.FirstName) != "Updated" {
		t.Fatalf("db first name = %q", safeStr(u.FirstName))
	}
	if u.NetworkRating != 2 {
		t.Fatalf("db rating = %d", u.NetworkRating)
	}
}

func TestUserCreateMissingCSRFForbidden(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "pw")

	form := url.Values{}
	form.Set("first_name", "X")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	// deliberately no csrf_token and strip csrf cookie from jar for POST body only
	req := httptest.NewRequest(http.MethodPost, "/usereditor/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies)) // session present, csrf cookie may be present but form field missing
	// Remove csrf form field; validateCSRF needs both cookie and form/header.
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", w.Code)
	}
}

func TestObserverCannotCreateUserViaForm(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")

	form := url.Values{}
	form.Set("first_name", "Nope")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	w, _ := formPOST(t, ts, "/usereditor/create", form, cookies)
	// requireMinRatingHTML redirects to dashboard for low rating on the route group.
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestNoJSConfigUpdateCookieCSRFNoAuthHeader(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	// GET configeditor: fields server-rendered
	w, cookies := authedGET(t, ts, "/configeditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /configeditor status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `method="post" action="/configeditor"`) {
		t.Fatalf("expected config form POST, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, `name="cfg_WELCOME_MESSAGE"`) {
		t.Fatal("expected welcome message field")
	}

	form := url.Values{}
	form.Set("cfg_WELCOME_MESSAGE", "Hello openfsd operators")
	form.Set("cfg_FSD_SERVER_HOSTNAME", "fsd.example.test")
	form.Set("cfg_FSD_SERVER_IDENT", "TEST-FSD")
	form.Set("cfg_FSD_SERVER_LOCATION", "Nowhere")
	form.Set("cfg_API_SERVER_BASE_URL", "https://api.example.test")
	w, cookies = formPOST(t, ts, "/configeditor", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("config save status %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/configeditor?flash=saved" {
		t.Fatalf("Location=%q", loc)
	}

	// No Authorization header was used — verify via formPOST implementation + success.
	val, err := ts.dbRepo.ConfigRepo.Get(db.ConfigWelcomeMessage)
	if err != nil {
		t.Fatal(err)
	}
	if val != "Hello openfsd operators" {
		t.Fatalf("welcome message = %q", val)
	}

	w, _ = authedGET(t, ts, "/configeditor?flash=saved", cookies)
	body = w.Body.String()
	if !strings.Contains(body, "Configuration saved") {
		t.Fatal("expected saved flash")
	}
	if !strings.Contains(body, "Hello openfsd operators") {
		t.Fatal("expected saved value in form")
	}
}

func TestConfigUpdateMissingCSRFForbidden(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("cfg_WELCOME_MESSAGE", "x")
	req := httptest.NewRequest(http.MethodPost, "/configeditor", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", w.Code)
	}
}

func TestSupervisorCannotMutateConfigViaForm(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "pw")

	form := url.Values{}
	form.Set("cfg_WELCOME_MESSAGE", "nope")
	w, _ := formPOST(t, ts, "/configeditor", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestDashboardServerRenderedSummary(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "pw")

	w, _ := authedGET(t, ts, "/dashboard", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	// FSD service is unreachable in tests → unavailable message still server-rendered.
	if !strings.Contains(body, "Connections") {
		t.Fatalf("expected connection summary section, body=%s", clip(body, 500))
	}
	if !strings.Contains(body, "Connection summary temporarily unavailable") &&
		!strings.Contains(body, "connected") {
		t.Fatalf("expected summary content (available or unavailable), body=%s", clip(body, 500))
	}
	// Map is enhancement; script tags may be present but summary must not require them.
	if !strings.Contains(body, "Welcome, Test!") {
		t.Fatal("expected server-rendered welcome")
	}
	// Documented Leaflet exception comment should be present in template source path;
	// rendered HTML may strip HTML comments depending on template — check map element exists.
	if !strings.Contains(body, `id="map"`) {
		t.Fatal("expected map container for PE")
	}
}

func TestXSSUserNameEscapedInEditor(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	// Create a user with XSS payload in name via repository.
	xssFirst := `<script>alert(1)</script>`
	xssLast := `"><img src=x onerror=alert(2)>`
	u := &db.User{
		Password:      "password99",
		FirstName:     &xssFirst,
		LastName:      &xssLast,
		NetworkRating: int(protocol.NetworkRatingObserver),
	}
	if err := ts.dbRepo.UserRepo.CreateUser(u); err != nil {
		t.Fatal(err)
	}

	cookies := formLogin(t, ts, sup.CID, "pw")
	w, _ := authedGET(t, ts, "/usereditor?cid="+itoa(u.CID), cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()

	// Raw script must not appear unescaped as executable HTML.
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("unescaped script tag in HTML")
	}
	if strings.Contains(body, "<script>alert") {
		t.Fatal("executable script payload present")
	}
	// html/template should entity-escape < and " in attribute/text context.
	if strings.Contains(body, xssFirst) {
		t.Fatalf("XSS first-name payload appears unescaped: %s", clip(body, 800))
	}
	if strings.Contains(body, `"><img src=x onerror=alert(2)>`) {
		t.Fatalf("XSS last-name payload appears unescaped: %s", clip(body, 800))
	}
	if !strings.Contains(body, "&lt;") && !strings.Contains(body, "&#34;") && !strings.Contains(body, "&quot;") {
		t.Fatalf("expected HTML entity escaping of XSS payload, body=%s", clip(body, 800))
	}
}

func TestXSSConfigValueEscapedInEditor(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	payload := `"><script>alert("xss")</script>`
	if err := ts.dbRepo.ConfigRepo.Set(db.ConfigWelcomeMessage, payload); err != nil {
		t.Fatal(err)
	}

	cookies := formLogin(t, ts, admin.CID, "pw")
	w, _ := authedGET(t, ts, "/configeditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `<script>alert("xss")</script>`) {
		t.Fatal("unescaped script in config editor HTML")
	}
	if strings.Contains(body, "<script>alert") {
		t.Fatal("executable script in config value attribute/text")
	}
	// Escaped form should still round-trip meaning via entities
	if !strings.Contains(body, "cfg_WELCOME_MESSAGE") {
		t.Fatal("missing welcome field")
	}
}

func TestNoJSCreateTokenForm(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	// leave expiry blank → default one year
	w, _ := formPOST(t, ts, "/configeditor/create-token", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("create-token status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "API token created") {
		t.Fatalf("expected success message, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, `id="created-token-value"`) {
		t.Fatal("expected token display")
	}
	// Token content should be inside code element; ensure no Authorization was needed.
	if !strings.Contains(body, "eyJ") {
		// JWT typically starts with eyJ
		t.Fatalf("expected JWT-looking token in body: %s", clip(body, 600))
	}
}

func TestConfigUpdateViaAPIStillRequiresAuth(t *testing.T) {
	// Sanity: unauthenticated API update fails
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/update",
		strings.NewReader(`{"key_value_pairs":[{"key":"WELCOME_MESSAGE","value":"x"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		body, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d want 401, body %s", w.Code, body)
	}
}
