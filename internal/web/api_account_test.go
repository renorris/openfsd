package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/renorris/openfsd/pkg/protocol"
)

func apiAccountJSON(t *testing.T, ts *testServer, method, path string, body any, bearer string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if len(cookies) > 0 {
		req.Header.Set("Cookie", cookieHeader(cookies))
		if csrf := csrfFromCookies(cookies); csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
	}
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	return w
}

func loginAccessToken(t *testing.T, ts *testServer, cid int, password string) string {
	t.Helper()
	body := map[string]any{"cid": cid, "password": password}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status %d body %s", w.Code, w.Body.String())
	}
	var res APIV1Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(res.Data)
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("empty access_token")
	}
	return tokens.AccessToken
}

func decodeAccountEnvelope(t *testing.T, w *httptest.ResponseRecorder) APIV1Response {
	t.Helper()
	var res APIV1Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return res
}

func errString(res APIV1Response) string {
	if res.Err == nil {
		return ""
	}
	return *res.Err
}

// setCookieIsClear reports whether a Set-Cookie header line clears the named cookie.
func setCookieIsClear(sc, name string) bool {
	if !strings.HasPrefix(sc, name+"=") {
		return false
	}
	return strings.Contains(sc, "Max-Age=0") ||
		strings.Contains(sc, "Max-Age=-1") ||
		strings.Contains(sc, name+"=;") ||
		strings.HasPrefix(sc, name+"=;")
}

// setCookieIsLiveSession reports a non-clear session Set-Cookie (re-issue).
func setCookieIsLiveSession(sc string) bool {
	if !strings.HasPrefix(sc, sessionCookieName+"=") {
		return false
	}
	return !setCookieIsClear(sc, sessionCookieName)
}

func TestAPIAccountPasswordBearerSuccess(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "oldpassword")

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/password", map[string]any{
		"current_password": "oldpassword",
		"new_password":     "newpassword1",
		"confirm_password": "newpassword1",
	}, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	if res.Err != nil {
		t.Fatalf("err=%v", *res.Err)
	}
	// Design §C: 200 envelope data: null.
	if res.Data != nil {
		t.Fatalf("data=%v want null", res.Data)
	}
	if !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatalf("body missing \"data\":null: %s", w.Body.String())
	}
	// Bearer: no session cookie side effects.
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, sessionCookieName+"=") {
			t.Fatalf("Bearer password change must not Set-Cookie session: %s", sc)
		}
		if strings.HasPrefix(sc, csrfCookieName+"=") {
			t.Fatalf("Bearer password change must not touch CSRF cookie: %s", sc)
		}
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.dbRepo.UserRepo.VerifyPasswordHash("newpassword1", u.Password) {
		t.Fatal("password not updated")
	}
	if ts.dbRepo.UserRepo.VerifyPasswordHash("oldpassword", u.Password) {
		t.Fatal("old password still valid")
	}
}

func TestAPIAccountPasswordCookieSessionReissue(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "oldpassword")
	// Ensure CSRF is present (from HTML GET).
	wGET, cookies := authedGET(t, ts, "/account", cookies)
	if wGET.Code != http.StatusOK {
		t.Fatalf("GET /account %d", wGET.Code)
	}

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/password", map[string]any{
		"current_password": "oldpassword",
		"new_password":     "newpassword1",
		"confirm_password": "newpassword1",
	}, "", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	// Password must actually change (not only cookie side effects).
	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.dbRepo.UserRepo.VerifyPasswordHash("newpassword1", u.Password) {
		t.Fatal("password not updated on cookie dual-accept path")
	}

	// Session re-issued for cookie dual-accept (rememberMe=false → Max-Age=86400).
	foundSession := false
	clearedCSRF := false
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if setCookieIsLiveSession(sc) {
			foundSession = true
			// sessionDefaultTTL = 24h when rememberMe=false.
			if !strings.Contains(sc, "Max-Age=86400") {
				t.Fatalf("session re-issue want Max-Age=86400 (24h), got %s", sc)
			}
		}
		if setCookieIsClear(sc, csrfCookieName) {
			clearedCSRF = true
		}
	}
	if !foundSession {
		t.Fatal("expected session Set-Cookie re-issue on cookie password change")
	}
	if !clearedCSRF {
		t.Fatal("expected CSRF cookie clear on cookie password change (HTML parity)")
	}
}

func TestAPIAccountPasswordValidation(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "oldpassword")

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "empty current",
			body: map[string]any{
				"current_password": "",
				"new_password":     "newpassword1",
				"confirm_password": "newpassword1",
			},
			want: "current password is required",
		},
		{
			name: "wrong current",
			body: map[string]any{
				"current_password": "nope-nope",
				"new_password":     "newpassword1",
				"confirm_password": "newpassword1",
			},
			want: "incorrect password",
		},
		{
			name: "short new",
			body: map[string]any{
				"current_password": "oldpassword",
				"new_password":     "short",
				"confirm_password": "short",
			},
			want: "at least 8 characters",
		},
		{
			name: "colon new",
			body: map[string]any{
				"current_password": "oldpassword",
				"new_password":     "bad:colon1",
				"confirm_password": "bad:colon1",
			},
			want: "colon",
		},
		{
			name: "mismatch confirm",
			body: map[string]any{
				"current_password": "oldpassword",
				"new_password":     "newpassword1",
				"confirm_password": "newpassword2",
			},
			want: "Passwords do not match",
		},
		{
			name: "missing confirm",
			body: map[string]any{
				"current_password": "oldpassword",
				"new_password":     "newpassword1",
			},
			want: "Passwords do not match",
		},
		{
			name: "same as current",
			body: map[string]any{
				"current_password": "oldpassword",
				"new_password":     "oldpassword",
				"confirm_password": "oldpassword",
			},
			want: "different from the current password",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/password", tc.body, token, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d want 400 body %s", w.Code, w.Body.String())
			}
			res := decodeAccountEnvelope(t, w)
			if !strings.Contains(errString(res), tc.want) {
				t.Fatalf("err=%q want substring %q", errString(res), tc.want)
			}
			// Password unchanged on every validation failure.
			u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
			if err != nil {
				t.Fatal(err)
			}
			if !ts.dbRepo.UserRepo.VerifyPasswordHash("oldpassword", u.Password) {
				t.Fatal("password must not change on validation failure")
			}
		})
	}
}

func TestAPIAccountPasswordUnauthenticated(t *testing.T) {
	ts := newTestServer(t)
	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/password", map[string]any{
		"current_password": "x",
		"new_password":     "newpassword1",
		"confirm_password": "newpassword1",
	}, "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d want 401", w.Code)
	}
}

func TestAPIAccountPasswordCookieRequiresCSRF(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "oldpassword")
	_, cookies = authedGET(t, ts, "/account", cookies)

	b, _ := json.Marshal(map[string]any{
		"current_password": "oldpassword",
		"new_password":     "newpassword1",
		"confirm_password": "newpassword1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	// Intentionally omit X-CSRF-Token
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403 body %s", w.Code, w.Body.String())
	}
}

func TestAPIAccountDeleteSoftBearer(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "delete-me1", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "delete-me1")

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "delete-me1",
		"confirm_cid":      user.CID,
	}, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	if res.Err != nil {
		t.Fatalf("err=%v", *res.Err)
	}
	data, _ := json.Marshal(res.Data)
	if !strings.Contains(string(data), `"soft_deleted"`) {
		t.Fatalf("data=%s want soft_deleted", data)
	}
	// Bearer: no cookie clear side effects required (none set).
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, sessionCookieName+"=") {
			t.Fatalf("Bearer delete must not Set-Cookie session: %s", sc)
		}
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingInactive) {
		t.Fatalf("rating=%d want Inactive", u.NetworkRating)
	}

	// Subsequent Bearer use is rejected (revalidation).
	w2 := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/password", map[string]any{
		"current_password": "delete-me1",
		"new_password":     "newpassword1",
		"confirm_password": "newpassword1",
	}, token, nil)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("post-soft-delete bearer status %d want 401", w2.Code)
	}
}

func TestAPIAccountDeleteHardWhenEnabled(t *testing.T) {
	ts := newTestServer(t)
	ts.cfg.AllowPermanentAccountDelete = true
	user := createTestUser(t, ts, "hard-del1", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "hard-del1")

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "hard-del1",
		"confirm_cid":      user.CID,
		"permanent":        true,
	}, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	data, _ := json.Marshal(res.Data)
	if !strings.Contains(string(data), `"hard_deleted"`) {
		t.Fatalf("data=%s want hard_deleted", data)
	}
	_, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err == nil {
		t.Fatal("expected row gone after hard delete")
	}
}

func TestAPIAccountDeletePermanentFailClosed(t *testing.T) {
	ts := newTestServer(t)
	// AllowPermanentAccountDelete remains false (default).
	user := createTestUser(t, ts, "soft-only1", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "soft-only1")

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "soft-only1",
		"confirm_cid":      user.CID,
		"permanent":        true,
	}, token, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d want 400 body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	if !strings.Contains(errString(res), "permanent delete is disabled") {
		t.Fatalf("err=%q", errString(res))
	}
	// No mutation — intentional JSON divergence from HTML soft-fallback.
	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("rating=%d want OBS (no silent soft-delete)", u.NetworkRating)
	}
}

// Cookie dual-accept fail-closed: must not clear session (HTML permanent-disabled
// soft-deletes and logs out; JSON must keep the session and leave rating OBS).
func TestAPIAccountDeletePermanentFailClosedCookie(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "soft-only2", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "soft-only2")
	_, cookies = authedGET(t, ts, "/account", cookies)

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "soft-only2",
		"confirm_cid":      user.CID,
		"permanent":        true,
	}, "", cookies)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d want 400 body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	if !strings.Contains(errString(res), "permanent delete is disabled") {
		t.Fatalf("err=%q", errString(res))
	}
	u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("rating=%d want OBS (no silent soft-delete)", u.NetworkRating)
	}
	// Fail-closed must not log the user out.
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if setCookieIsClear(sc, sessionCookieName) {
			t.Fatalf("fail-closed must not clear session: %s", sc)
		}
		if setCookieIsClear(sc, csrfCookieName) {
			t.Fatalf("fail-closed must not clear CSRF: %s", sc)
		}
	}
	// Session still usable for a follow-up HTML GET.
	w2, _ := authedGET(t, ts, "/account", cookies)
	if w2.Code != http.StatusOK {
		t.Fatalf("session should remain valid after fail-closed, GET /account status %d", w2.Code)
	}
}

func TestAPIAccountDeleteValidation(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "keep-me1x", int(protocol.NetworkRatingObserver))
	token := loginAccessToken(t, ts, user.CID, "keep-me1x")

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "empty password",
			body: map[string]any{
				"current_password": "",
				"confirm_cid":      user.CID,
			},
			want: "current password is required",
		},
		{
			name: "wrong password",
			body: map[string]any{
				"current_password": "wrong-pass",
				"confirm_cid":      user.CID,
			},
			want: "incorrect password",
		},
		{
			name: "cid mismatch",
			body: map[string]any{
				"current_password": "keep-me1x",
				"confirm_cid":      999999,
			},
			want: "confirm_cid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", tc.body, token, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d want 400 body %s", w.Code, w.Body.String())
			}
			res := decodeAccountEnvelope(t, w)
			if !strings.Contains(errString(res), tc.want) {
				t.Fatalf("err=%q want %q", errString(res), tc.want)
			}
			u, err := ts.dbRepo.UserRepo.GetUserByCID(context.Background(), user.CID)
			if err != nil {
				t.Fatal(err)
			}
			if u.NetworkRating != int(protocol.NetworkRatingObserver) {
				t.Fatalf("must remain active, rating=%d", u.NetworkRating)
			}
		})
	}
}

func TestAPIAccountDeleteCookieClearsSession(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "delete-me2", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "delete-me2")
	_, cookies = authedGET(t, ts, "/account", cookies)

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "delete-me2",
		"confirm_cid":      user.CID,
		"permanent":        false,
	}, "", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	clearedSession := false
	clearedCSRF := false
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if setCookieIsClear(sc, sessionCookieName) {
			clearedSession = true
		}
		if setCookieIsClear(sc, csrfCookieName) {
			clearedCSRF = true
		}
	}
	if !clearedSession {
		t.Fatal("expected session cookie clear on cookie dual-accept delete")
	}
	if !clearedCSRF {
		t.Fatal("expected CSRF cookie clear on cookie dual-accept delete (HTML parity)")
	}
}

func TestAPIAccountDeleteOmitsPermanentIsSoft(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "delete-me3", int(protocol.NetworkRatingSupervisor))
	token := loginAccessToken(t, ts, user.CID, "delete-me3")

	w := apiAccountJSON(t, ts, http.MethodPost, "/api/v1/account/delete", map[string]any{
		"current_password": "delete-me3",
		"confirm_cid":      user.CID,
	}, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	res := decodeAccountEnvelope(t, w)
	data, _ := json.Marshal(res.Data)
	if !strings.Contains(string(data), `"soft_deleted"`) {
		t.Fatalf("data=%s", data)
	}
}
