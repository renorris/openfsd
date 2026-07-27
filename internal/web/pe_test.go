package web

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
	_ "modernc.org/sqlite"
)

type testServer struct {
	*Server
	engine *gin.Engine
	sqlDB  *sql.DB
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		t.Fatalf("repos: %v", err)
	}
	if err := db.InitDefaultConfig(repos.ConfigRepo); err != nil {
		t.Fatalf("init config: %v", err)
	}

	cfg := &ServerConfig{
		ListenAddr:            ":0",
		FsdHttpServiceAddress: "http://127.0.0.1:9",
		CookieSecure:          "",
	}
	srv, err := NewServer(cfg, repos)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	engine, err := srv.setupRoutes()
	if err != nil {
		t.Fatalf("setupRoutes: %v", err)
	}
	return &testServer{Server: srv, engine: engine, sqlDB: sqlDB}
}

func createTestUser(t *testing.T, ts *testServer, password string, rating int) *db.User {
	t.Helper()
	first := "Test"
	last := "User"
	u := &db.User{
		Password:      password,
		FirstName:     &first,
		LastName:      &last,
		NetworkRating: rating,
	}
	if err := ts.dbRepo.UserRepo.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func extractCookie(resp *http.Response, name string) string {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	// Also scan raw Set-Cookie if Response.Cookies() missed MaxAge-only clears.
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, name+"=") {
			part := strings.SplitN(sc, ";", 2)[0]
			return strings.TrimPrefix(part, name+"=")
		}
	}
	return ""
}

func getLoginCSRF(t *testing.T, ts *testServer) (csrf string, cookies []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /login status %d", w.Code)
	}
	body := w.Body.String()
	// Parse hidden csrf_token value
	const marker = `name="csrf_token" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("csrf field not found in login HTML")
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("csrf value unclosed")
	}
	csrf = rest[:j]
	if csrf == "" {
		t.Fatal("empty csrf token")
	}
	return csrf, w.Result().Cookies()
}

func cookieHeader(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Value == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func mergeCookies(existing []*http.Cookie, resp *http.Response) []*http.Cookie {
	byName := map[string]*http.Cookie{}
	for _, c := range existing {
		byName[c.Name] = c
	}
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(byName, c.Name)
			continue
		}
		byName[c.Name] = c
	}
	out := make([]*http.Cookie, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	return out
}

func TestNoJSLoginSuccess(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "s3cret-pass", int(protocol.NetworkRatingObserver))

	csrf, cookies := getLoginCSRF(t, ts)

	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "s3cret-pass")
	form.Set("csrf_token", csrf)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status %d, body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", loc)
	}
	session := extractCookie(w.Result(), sessionCookieName)
	if session == "" {
		t.Fatal("expected session cookie on login")
	}
	// Default HTTP: Secure should be absent
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, sessionCookieName+"=") && strings.Contains(strings.ToLower(sc), "secure") {
			t.Fatalf("session cookie should not be Secure on HTTP default: %s", sc)
		}
	}
}

func TestNoJSLoginBadPassword(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "s3cret-pass", int(protocol.NetworkRatingObserver))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "wrong")
	form.Set("csrf_token", csrf)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Bad CID and/or password") {
		t.Fatalf("expected error near form, body=%s", body)
	}
	if extractCookie(w.Result(), sessionCookieName) != "" {
		t.Fatal("should not set session on bad password")
	}
}

func TestUnauthDashboardRedirect(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestUnauthUserEditorRedirect(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/usereditor", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestLoginMissingCSRFForbidden(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", 1)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	// no csrf_token

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", w.Code)
	}
}

func TestLogoutMissingCSRFForbidden(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", 1)
	// Establish session via login
	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status %d", w.Code)
	}
	cookies = mergeCookies(cookies, w.Result())

	// Logout without CSRF
	req = httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status %d, want 403", w.Code)
	}
}

func TestAuthedDashboardRendersUser(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	cookies = mergeCookies(cookies, w.Result())

	req = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Welcome, Test!") {
		t.Fatalf("expected server-rendered name, body snippet: %s", clip(body, 400))
	}
	if !strings.Contains(body, "CID: "+itoa(user.CID)) {
		t.Fatalf("expected CID in body")
	}
	if !strings.Contains(body, "Supervisor") {
		t.Fatalf("expected rating label")
	}
	if !strings.Contains(body, "/usereditor") {
		t.Fatalf("expected user editor link for supervisor")
	}
}

// TestThemeChromeInLayout ensures every layout-based page ships the shared
// dark-mode assets and toggle (no per-page theme wiring).
func TestThemeChromeInLayout(t *testing.T) {
	ts := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /login status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`/static/js/openfsd/theme.js`,
		`/static/css/openfsd/theme.css`,
		`data-js="theme-toggle"`,
		`ofs-theme-toggle`,
		// Theme-aware nav/control classes (not btn-outline-dark)
		`btn-outline-secondary`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("login HTML missing %q; snippet: %s", want, clip(body, 500))
		}
	}
	if strings.Contains(body, "btn-outline-dark") {
		t.Fatal("login HTML still uses btn-outline-dark (poor dark-mode contrast)")
	}

	// Static assets must be served (embed).
	for _, path := range []string{
		"/static/js/openfsd/theme.js",
		"/static/css/openfsd/theme.css",
	} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		w = httptest.NewRecorder()
		ts.engine.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status %d", path, w.Code)
		}
		if w.Body.Len() < 32 {
			t.Fatalf("GET %s body too small", path)
		}
	}
}

func TestAPICookieAuthRequiresCSRF(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	cookies = mergeCookies(cookies, w.Result())

	// Cookie-only API POST without CSRF → 403
	req = httptest.NewRequest(http.MethodPost, "/api/v1/user/load", strings.NewReader(`{"cid":`+itoa(user.CID)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		body, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d want 403, body %s", w.Code, body)
	}

	// With CSRF header → 200
	// Refresh CSRF from dashboard page
	req = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	cookies = mergeCookies(cookies, w.Result())
	csrfVal := ""
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			csrfVal = c.Value
		}
	}
	if csrfVal == "" {
		t.Fatal("missing csrf cookie after dashboard")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/user/load", strings.NewReader(`{"cid":`+itoa(user.CID)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrfVal)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cookie+CSRF API status %d body %s", w.Code, w.Body.String())
	}
}

func TestAPIBearerAuthStillWorksWithoutCSRF(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))

	// Get access token via JSON login API
	body := `{"cid":` + itoa(user.CID) + `,"password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auth login %d %s", w.Code, w.Body.String())
	}
	// crude extract access_token
	respBody := w.Body.String()
	marker := `"access_token":"`
	i := strings.Index(respBody, marker)
	if i < 0 {
		t.Fatalf("no access token in %s", respBody)
	}
	rest := respBody[i+len(marker):]
	j := strings.Index(rest, `"`)
	token := rest[:j]

	req = httptest.NewRequest(http.MethodPost, "/api/v1/user/load", strings.NewReader(`{"cid":`+itoa(user.CID)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Bearer API status %d body %s", w.Code, w.Body.String())
	}
}

func TestObserverCannotAccessUserEditor(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "pw")

	req := httptest.NewRequest(http.MethodGet, "/usereditor", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q want /dashboard", loc)
	}
}

// TestAPICookieAuthCSRFNotBypassedByGarbageBearer is the Issue 1 regression:
// valid session + Authorization: Bearer garbage + no CSRF must 403 (not skip CSRF).
func TestAPICookieAuthCSRFNotBypassedByGarbageBearer(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, user.CID, "pw")

	body := `{"cid":` + itoa(user.CID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403 (CSRF required when Bearer fails), body %s", w.Code, w.Body.String())
	}

	// lowercase scheme also must not bypass CSRF when token is garbage
	req = httptest.NewRequest(http.MethodPost, "/api/v1/user/load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set("Authorization", "bearer x")
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("lowercase bearer garbage status %d want 403, body %s", w.Code, w.Body.String())
	}
}

func TestSupervisorCannotAccessConfigEditor(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, user.CID, "pw")

	req := httptest.NewRequest(http.MethodGet, "/configeditor", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q want /dashboard", loc)
	}
}

func TestAdminCanAccessConfigEditor(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, user.CID, "pw")

	req := httptest.NewRequest(http.MethodGet, "/configeditor", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
}

func TestSuspendedUserCannotFormLogin(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSuspended))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 re-render", w.Code)
	}
	if extractCookie(w.Result(), sessionCookieName) != "" {
		t.Fatal("suspended user must not receive session cookie")
	}
	if !strings.Contains(w.Body.String(), "Bad CID and/or password") {
		t.Fatalf("expected generic error, body=%s", clip(w.Body.String(), 300))
	}
}

func TestInactiveUserCannotFormLogin(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingInactive))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if extractCookie(w.Result(), sessionCookieName) != "" {
		t.Fatal("inactive user must not receive session cookie")
	}
}

func TestFsdJwtRequiresPassword(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "correct-password", int(protocol.NetworkRatingObserver))

	// Wrong password → 401
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "wrong")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/fsd-jwt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status %d want 401, body %s", w.Code, w.Body.String())
	}

	// Correct password → 200 + token
	form.Set("password", "correct-password")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/fsd-jwt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("good password status %d want 200, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) && !strings.Contains(w.Body.String(), `"success": true`) {
		// gin may encode without spaces
		if !strings.Contains(w.Body.String(), "token") {
			t.Fatalf("expected token in response: %s", w.Body.String())
		}
	}
}

func TestRememberMeSetsLongerSessionMaxAge(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))

	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(user.CID))
	form.Set("password", "pw")
	form.Set("csrf_token", csrf)
	form.Set("remember_me", "on")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status %d", w.Code)
	}
	found := false
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if !strings.HasPrefix(sc, sessionCookieName+"=") {
			continue
		}
		found = true
		// Max-Age for 30 days = 2592000
		if !strings.Contains(sc, "Max-Age=2592000") && !strings.Contains(sc, "max-age=2592000") {
			t.Fatalf("remember-me Max-Age want 2592000, Set-Cookie=%s", sc)
		}
	}
	if !found {
		t.Fatal("missing session Set-Cookie")
	}
}

// formLogin performs a successful no-JS form login and returns merged cookies.
func formLogin(t *testing.T, ts *testServer, cid int, password string) []*http.Cookie {
	t.Helper()
	csrf, cookies := getLoginCSRF(t, ts)
	form := url.Values{}
	form.Set("cid", itoa(cid))
	form.Set("password", password)
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("form login status %d body %s", w.Code, w.Body.String())
	}
	return mergeCookies(cookies, w.Result())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
