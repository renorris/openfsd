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

	req = httptest.NewRequest(http.MethodGet, "/usereditor", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q want /dashboard", loc)
	}
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
