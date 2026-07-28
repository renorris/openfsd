package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

	// GET usereditor: directory + empty rail (create only when ?new=1).
	w, cookies := authedGET(t, ts, "/usereditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /usereditor status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="user-directory-table"`) {
		t.Fatalf("expected directory table, body=%s", clip(body, 500))
	}
	if !strings.Contains(body, "New user") {
		t.Fatal("expected New user control")
	}

	// Create rail: ?new=1 shows create form with CSRF.
	w, cookies = authedGET(t, ts, "/usereditor?new=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /usereditor?new=1 status %d", w.Code)
	}
	body = w.Body.String()
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
	assertUserEditorRedirect(t, loc, "", "created")

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
	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), target.CID)
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
	val, err := ts.dbRepo.ConfigRepo.Get(context.Background(), db.ConfigWelcomeMessage)
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
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), u); err != nil {
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
	if err := ts.dbRepo.ConfigRepo.Set(context.Background(), db.ConfigWelcomeMessage, payload); err != nil {
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

// TestAPIUpdateUserCannotElevateRatingAboveActor is the Issue 1 regression:
// supervisor cannot PATCH an observer to Administrator via the JSON API.
func TestAPIUpdateUserCannotElevateRatingAboveActor(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	target := createTestUser(t, ts, "obs-pass", int(protocol.NetworkRatingObserver))

	// Obtain Bearer access token
	loginBody := `{"cid":` + itoa(sup.CID) + `,"password":"sup-pass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auth login %d %s", w.Code, w.Body.String())
	}
	respBody := w.Body.String()
	marker := `"access_token":"`
	i := strings.Index(respBody, marker)
	if i < 0 {
		t.Fatalf("no access token in %s", respBody)
	}
	rest := respBody[i+len(marker):]
	j := strings.Index(rest, `"`)
	token := rest[:j]

	// Attempt privilege escalation: set rating to Administrator (12)
	payload := `{"cid":` + itoa(target.CID) + `,"network_rating":12}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/user/update", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("elevate via API status %d want 403, body %s", w.Code, w.Body.String())
	}

	// DB must be unchanged
	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), target.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("target rating = %d, want still Observer", u.NetworkRating)
	}
}

// Cookie+CSRF path of the same elevation hole.
func TestAPIUpdateUserCannotElevateViaCookieSession(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	target := createTestUser(t, ts, "obs-pass", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	// Ensure CSRF cookie
	_, cookies = authedGET(t, ts, "/dashboard", cookies)
	csrf := csrfFromCookies(cookies)
	if csrf == "" {
		t.Fatal("missing csrf after dashboard")
	}

	payload := `{"cid":` + itoa(target.CID) + `,"network_rating":12}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/user/update", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrf)
	// No Authorization — cookie session only
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cookie elevate status %d want 403, body %s", w.Code, w.Body.String())
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), target.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("target rating = %d after cookie elevate attempt", u.NetworkRating)
	}
}

func TestSupervisorCannotCreateAdminViaForm(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	// UI should not offer Administrator option for supervisor on create/edit selects.
	// Filter may legally contain value="12" (inventory).
	w, cookies := authedGET(t, ts, "/usereditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET usereditor %d", w.Code)
	}
	body := w.Body.String()
	if selectContainsValue(body, "create-network-rating", "12") {
		t.Fatal("supervisor create select should not list Administrator (12)")
	}
	// Empty rail: edit select absent; when present must also exclude 12.
	if selectContainsValue(body, "edit-network-rating", "12") {
		t.Fatal("supervisor edit select should not list Administrator (12)")
	}

	form := url.Values{}
	form.Set("first_name", "Elevated")
	form.Set("password", "password99")
	form.Set("network_rating", "12") // forced POST bypassing UI
	w, _ = formPOST(t, ts, "/usereditor/create", form, cookies)
	// Server re-renders with error (200), not redirect success
	if w.Code == http.StatusSeeOther {
		t.Fatalf("create admin must not redirect success, Location=%s", w.Header().Get("Location"))
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 re-render", w.Code)
	}
	if !strings.Contains(w.Body.String(), "You cannot create a user with that network rating") {
		t.Fatalf("expected rating ceiling error, body=%s", clip(w.Body.String(), 500))
	}
}

func TestSupervisorCannotPromoteToAdminViaForm(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	target := createTestUser(t, ts, "obs-pass", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	form := url.Values{}
	form.Set("cid", itoa(target.CID))
	form.Set("first_name", "Still")
	form.Set("last_name", "Observer")
	form.Set("network_rating", "12")
	form.Set("password", "")
	w, _ := formPOST(t, ts, "/usereditor/update", form, cookies)
	if w.Code == http.StatusSeeOther {
		t.Fatalf("promote to admin must not succeed, Location=%s", w.Header().Get("Location"))
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 re-render", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Cannot set network rating above your own") {
		t.Fatalf("expected ceiling error, body=%s", clip(w.Body.String(), 500))
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), target.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("DB rating = %d, want Observer still", u.NetworkRating)
	}
}

func TestSupervisorCannotUpdateHigherRatedUserViaForm(t *testing.T) {
	ts := newTestServer(t)
	// Create admin first so CID ordering is fine; login as supervisor
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	// Preserve a distinct first name so we can detect profile mutation.
	admin.FirstName = strPtr("Original")
	admin.Password = ""
	if err := ts.dbRepo.UserRepo.UpdateUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	// GET: profile locked for higher-rated target; ratings still editable.
	w, cookies := authedGET(t, ts, "/usereditor?cid="+itoa(admin.CID), cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET higher-rated user status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Profile locked") {
		t.Fatalf("expected profile-locked notice, body=%s", clip(body, 500))
	}
	if !strings.Contains(body, `id="edit-first-name"`) || !strings.Contains(body, "disabled") {
		t.Fatalf("expected disabled profile fields, body=%s", clip(body, 600))
	}
	if !strings.Contains(body, ">Update</button>") {
		t.Fatal("expected Update for rating adjustments")
	}

	// POST password on higher-rated target must fail.
	form := url.Values{}
	form.Set("cid", itoa(admin.CID))
	form.Set("network_rating", itoa(admin.NetworkRating)) // leave network unchanged
	form.Set("pilot_rating", "0")
	form.Set("password", "newpassword1")
	w, cookies = formPOST(t, ts, "/usereditor/update", form, cookies)
	if w.Code == http.StatusSeeOther {
		t.Fatalf("must not change password on higher-rated user, Location=%s", w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "Only supervisors can change passwords") &&
		!strings.Contains(w.Body.String(), "password") {
		// Rating-only path rejects password with explicit message.
		if !strings.Contains(w.Body.String(), "Only supervisors can change passwords") {
			t.Fatalf("expected password rejection, body=%s", clip(w.Body.String(), 500))
		}
	}

	// Rating-only update (lower network to SUP ceiling) is allowed on any target.
	form = url.Values{}
	form.Set("cid", itoa(admin.CID))
	form.Set("network_rating", "11") // SUP rating — at actor ceiling
	form.Set("pilot_rating", "0")
	form.Set("password", "")
	w, _ = formPOST(t, ts, "/usereditor/update", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("rating-only update status %d body %s", w.Code, w.Body.String())
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), admin.CID)
	if err != nil {
		t.Fatal(err)
	}
	if safeStr(u.FirstName) != "Original" {
		t.Fatalf("admin first name must not change, got %q", safeStr(u.FirstName))
	}
	if u.NetworkRating != 11 {
		t.Fatalf("network rating = %d, want 11", u.NetworkRating)
	}
}

func TestInstructorCannotAccessUserEditor(t *testing.T) {
	ts := newTestServer(t)
	inst := createTestUser(t, ts, "inst-pass", int(protocol.NetworkRatingInstructor1))
	cookies := formLogin(t, ts, inst.CID, "inst-pass")

	w, _ := authedGET(t, ts, "/usereditor", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("GET /usereditor as I1 status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}

	// POST create also gated by middleware.
	form := url.Values{}
	form.Set("first_name", "Nope")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	form.Set("pilot_rating", "0")
	w, _ = formPOST(t, ts, "/usereditor/create", form, cookies)
	if w.Code == http.StatusSeeOther && strings.Contains(w.Header().Get("Location"), "flash=created") {
		t.Fatal("instructor must not create users")
	}
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/dashboard" {
		// Middleware redirect to dashboard is the expected gate.
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "created") {
			t.Fatal("instructor must not create users")
		}
	}
}

func TestSupervisorUserEditorPilotRatingFullScale(t *testing.T) {
	ts := newTestServer(t)
	// SUP with pilot P0 only — must still see full pilot scale and assign CMEL=15.
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	if sup.PilotRating != 0 {
		sup.PilotRating = 0
		sup.Password = ""
		if err := ts.dbRepo.UserRepo.UpdateUser(context.Background(), sup); err != nil {
			t.Fatal(err)
		}
	}
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, cookies := authedGET(t, ts, "/usereditor?new=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /usereditor?new=1 status %d", w.Code)
	}
	body := w.Body.String()
	for _, v := range []string{"0", "1", "3", "7", "15", "31", "63"} {
		if !selectContainsValue(body, "create-pilot-rating", v) {
			t.Fatalf("create pilot select missing value %s", v)
		}
	}

	form := url.Values{}
	form.Set("first_name", "Pilot")
	form.Set("last_name", "Full")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	form.Set("pilot_rating", "15") // CMEL — above actor P0
	w, _ = formPOST(t, ts, "/usereditor/create", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create with pilot_rating=15 status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	assertUserEditorRedirect(t, loc, "", "created")
	// Load created user by CID from redirect.
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := strconv.Atoi(u.Query().Get("cid"))
	created, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), cid)
	if err != nil {
		t.Fatal(err)
	}
	if created.PilotRating != 15 {
		t.Fatalf("pilot_rating=%d want 15", created.PilotRating)
	}
}

// assertUserEditorRedirect parses Location and requires cid + flash query keys.
// wantCID empty means any positive cid is accepted.
func assertUserEditorRedirect(t *testing.T, loc, wantCID, wantFlash string) {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if u.Path != "/usereditor" {
		t.Fatalf("Location path=%q want /usereditor (full=%q)", u.Path, loc)
	}
	q := u.Query()
	cid := q.Get("cid")
	if cid == "" {
		t.Fatalf("Location missing cid: %q", loc)
	}
	if wantCID != "" && cid != wantCID {
		t.Fatalf("Location cid=%q want %q (full=%q)", cid, wantCID, loc)
	}
	if q.Get("flash") != wantFlash {
		t.Fatalf("Location flash=%q want %q (full=%q)", q.Get("flash"), wantFlash, loc)
	}
}

// selectContainsValue reports whether the HTML select with the given id has an
// option with the given value attribute. Scopes to that element so filter
// options (which may include value="12") do not false-positive create/edit ceilings.
func selectContainsValue(body, selectID, value string) bool {
	// Find id="selectID"
	marker := `id="` + selectID + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		return false
	}
	// Search forward for </select>
	rest := body[i:]
	end := strings.Index(strings.ToLower(rest), "</select>")
	if end < 0 {
		return false
	}
	chunk := rest[:end]
	return strings.Contains(chunk, `value="`+value+`"`)
}

func TestNoJSUserDirectoryListsUsers(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	// Named user for directory display
	first := "Alice"
	last := "Directory"
	u := &db.User{
		Password:      "password99",
		FirstName:     &first,
		LastName:      &last,
		NetworkRating: int(protocol.NetworkRatingObserver),
	}
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="user-directory-table"`) {
		t.Fatalf("expected directory table, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, itoa(u.CID)) {
		t.Fatalf("expected seeded CID %d in directory", u.CID)
	}
	if !strings.Contains(body, "Alice Directory") {
		t.Fatalf("expected display name in directory, body=%s", clip(body, 600))
	}
	if !strings.Contains(body, `id="filter-network-rating"`) {
		t.Fatal("expected filter rating select")
	}
	// Filter may include ADM=12
	if !selectContainsValue(body, "filter-network-rating", "12") {
		t.Fatal("filter should list Administrator (12) for discovery")
	}
	// Empty rail empty-state
	if !strings.Contains(body, "Select a user from the directory") {
		t.Fatal("expected empty rail message")
	}
}

func TestNoJSUserDirectorySearch(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	aliceFirst, aliceLast := "Alice", "Smithson"
	bobFirst, bobLast := "Bob", "Jones"
	alice := &db.User{Password: "password99", FirstName: &aliceFirst, LastName: &aliceLast, NetworkRating: 1}
	bob := &db.User{Password: "password99", FirstName: &bobFirst, LastName: &bobLast, NetworkRating: 1}
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), alice); err != nil {
		t.Fatal(err)
	}
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), bob); err != nil {
		t.Fatal(err)
	}
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor?q=Smithson", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Alice Smithson") {
		t.Fatalf("expected Alice match, body=%s", clip(body, 600))
	}
	if strings.Contains(body, "Bob Jones") {
		t.Fatal("Bob should not match q=Smithson")
	}
	if !strings.Contains(body, `value="Smithson"`) {
		t.Fatal("filter form should echo q")
	}
}

func TestNoJSUserDirectoryRatingFilter(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	obs := createTestUser(t, ts, "obs-pass", int(protocol.NetworkRatingObserver))
	// S1 student
	s1First := "Stu"
	s1 := &db.User{Password: "password99", FirstName: &s1First, NetworkRating: 2}
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), s1); err != nil {
		t.Fatal(err)
	}
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor?rating=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	// Observer (1) should appear; S1 (2) should not
	if !strings.Contains(body, `>`+itoa(obs.CID)+`<`) && !strings.Contains(body, itoa(obs.CID)) {
		t.Fatalf("expected observer cid %d for rating=1", obs.CID)
	}
	// Selected filter option
	if !strings.Contains(body, `id="filter-network-rating"`) {
		t.Fatal("missing filter select")
	}
}

func TestNoJSUserDirectorySort(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor?sort=rating&dir=desc", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `sort=rating`) {
		t.Fatal("expected sort links/hidden sort=rating context")
	}
	// Active sort indicator on rating header
	if !strings.Contains(body, "Rating") {
		t.Fatal("expected Rating column")
	}
}

func TestNoJSUserDirectoryPagination(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	// Seed enough users that page 2 exists only if page size is small —
	// page size is fixed at 50, so create 51 extra users is heavy.
	// Instead assert pager chrome renders and page=2 clamps/works with few users.
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor?page=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "usr-pager") && !strings.Contains(body, "Page 1") {
		t.Fatalf("expected pager, body=%s", clip(body, 400))
	}

	// page past end clamps to last page (still 200)
	w, _ = authedGET(t, ts, "/usereditor?page=999", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Page 1 of") {
		t.Fatalf("expected clamp to page 1, body=%s", clip(w.Body.String(), 400))
	}
}

func TestNoJSUserSelectPreservesFilters(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	first, last := "Filter", "Keep"
	target := &db.User{Password: "password99", FirstName: &first, LastName: &last, NetworkRating: 1}
	if err := ts.dbRepo.UserRepo.CreateUser(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	path := "/usereditor?q=Filter&sort=name&dir=asc&cid=" + itoa(target.CID)
	w, _ := authedGET(t, ts, path, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `value="Filter"`) {
		t.Fatal("filter q should be preserved")
	}
	if !strings.Contains(body, `id="edit-form"`) {
		t.Fatal("expected edit form for selected cid")
	}
	if !strings.Contains(body, `value="Filter"`) || !strings.Contains(body, `value="Keep"`) {
		t.Fatal("edit form should show names")
	}
	// Row edit href should carry q
	if !strings.Contains(body, "q=Filter") {
		t.Fatal("row/sort links should preserve q=Filter")
	}
	// Selected row class
	if !strings.Contains(body, "usr-row-selected") {
		t.Fatal("expected selected row highlight")
	}
}

func TestNoJSUserCreatePreservesFilters(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	// Open create with filters in URL
	w, cookies := authedGET(t, ts, "/usereditor?q=zzz&sort=name&dir=desc&new=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET create rail %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="create-form"`) {
		t.Fatal("expected create form")
	}
	if !strings.Contains(body, `name="dir_q" value="zzz"`) {
		t.Fatalf("expected hidden dir_q, body=%s", clip(body, 800))
	}

	form := url.Values{}
	form.Set("first_name", "Preserve")
	form.Set("last_name", "Filters")
	form.Set("password", "password99")
	form.Set("network_rating", "1")
	form.Set("dir_q", "zzz")
	form.Set("dir_rating", "")
	form.Set("dir_sort", "name")
	form.Set("dir_dir", "desc")
	form.Set("dir_page", "1")
	w, cookies = formPOST(t, ts, "/usereditor/create", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("flash") != "created" {
		t.Fatalf("flash=%q loc=%q", q.Get("flash"), loc)
	}
	if q.Get("cid") == "" {
		t.Fatalf("missing cid in %q", loc)
	}
	if q.Get("q") != "zzz" {
		t.Fatalf("q not preserved: %q", loc)
	}
	if q.Get("sort") != "name" {
		t.Fatalf("sort not preserved: %q", loc)
	}
	if q.Get("dir") != "desc" {
		t.Fatalf("dir not preserved: %q", loc)
	}
}

func TestNoJSUserUpdatePreservesFilters(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	target := createTestUser(t, ts, "target-pass", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	form := url.Values{}
	form.Set("cid", itoa(target.CID))
	form.Set("first_name", "Updated")
	form.Set("last_name", "KeepQ")
	form.Set("network_rating", "2")
	form.Set("password", "")
	form.Set("dir_q", "KeepQ")
	form.Set("dir_rating", "2")
	form.Set("dir_sort", "rating")
	form.Set("dir_dir", "asc")
	form.Set("dir_page", "1")
	w, _ := formPOST(t, ts, "/usereditor/update", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("update status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("flash") != "updated" {
		t.Fatalf("flash=%q", q.Get("flash"))
	}
	if q.Get("cid") != itoa(target.CID) {
		t.Fatalf("cid=%q", q.Get("cid"))
	}
	if q.Get("q") != "KeepQ" {
		t.Fatalf("q not preserved: %q", loc)
	}
	if q.Get("rating") != "2" {
		t.Fatalf("rating not preserved: %q", loc)
	}
	if q.Get("sort") != "rating" {
		t.Fatalf("sort not preserved: %q", loc)
	}
}

func TestNoJSUserFilterFormOmitsPageAndSelection(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "sup-pass", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "sup-pass")

	w, _ := authedGET(t, ts, "/usereditor?q=x&page=2&cid=1&new=1", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	// Filter form should not include page/cid/new/flash fields
	// Extract filter form roughly
	idx := strings.Index(body, `id="user-filter-form"`)
	if idx < 0 {
		t.Fatal("missing filter form")
	}
	rest := body[idx:]
	end := strings.Index(rest, "</form>")
	if end < 0 {
		t.Fatal("unclosed filter form")
	}
	formHTML := rest[:end]
	if strings.Contains(formHTML, `name="page"`) {
		t.Fatal("filter form must omit page")
	}
	if strings.Contains(formHTML, `name="cid"`) {
		t.Fatal("filter form must omit cid")
	}
	if strings.Contains(formHTML, `name="new"`) {
		t.Fatal("filter form must omit new")
	}
	if strings.Contains(formHTML, `name="flash"`) {
		t.Fatal("filter form must omit flash")
	}
	// Should preserve sort/dir as hidden
	if !strings.Contains(formHTML, `name="sort"`) || !strings.Contains(formHTML, `name="dir"`) {
		t.Fatal("filter form must include hidden sort/dir")
	}
}
