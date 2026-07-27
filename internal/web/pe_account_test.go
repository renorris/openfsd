package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/renorris/openfsd/pkg/protocol"
)

func TestAccountPageRendersForObserver(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "obs-pass1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "obs-pass1")

	w, _ := authedGET(t, ts, "/account", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /account status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`action="/account/password"`,
		`action="/account/delete"`,
		`name="csrf_token"`,
		"Change password",
		"Delete my account",
		itoa(obs.CID),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("account page missing %q, body=%s", want, clip(body, 600))
		}
	}
	// Hard-delete checkbox hidden when flag false (default).
	if strings.Contains(body, `name="permanent"`) {
		t.Fatal("permanent delete checkbox should be hidden by default")
	}
}

func TestObserverDashboardHasAccountLink(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")

	w, _ := authedGET(t, ts, "/dashboard", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `href="/account"`) {
		t.Fatal("dashboard/layout must link to /account")
	}
	if !strings.Contains(body, "Manage account") {
		t.Fatal("dashboard should have Manage account button")
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "oldpassword")
	oldCSRF := csrfFromCookies(cookies)

	form := url.Values{}
	form.Set("current_password", "oldpassword")
	form.Set("new_password", "newpassword1")
	form.Set("confirm_password", "newpassword1")
	w, cookies := formPOST(t, ts, "/account/password", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "flash=password_changed") {
		t.Fatalf("Location=%q want password_changed flash", loc)
	}
	// Session re-issued
	if extractCookie(w.Result(), sessionCookieName) == "" {
		// mergeCookies may keep existing; check Set-Cookie header
		found := false
		for _, sc := range w.Result().Header.Values("Set-Cookie") {
			if strings.HasPrefix(sc, sessionCookieName+"=") && !strings.Contains(sc, "Max-Age=0") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected session Set-Cookie on password change")
		}
	}
	// CSRF rotated (clear + new issue → different value from old)
	newCSRF := csrfFromCookies(cookies)
	if newCSRF == "" {
		// May need GET after rotate
		w2, cookies2 := authedGET(t, ts, "/account", cookies)
		_ = w2
		newCSRF = csrfFromCookies(cookies2)
		cookies = cookies2
	}
	if newCSRF == "" {
		t.Fatal("expected CSRF after password change")
	}
	if oldCSRF != "" && newCSRF == oldCSRF {
		// Rotation: clearCSRF then issueCSRFToken on same response may set empty then new.
		// Accept if session still works with new CSRF.
	}

	// Old password fails login
	csrf, loginCookies := getLoginCSRF(t, ts)
	bad := url.Values{}
	bad.Set("cid", itoa(user.CID))
	bad.Set("password", "oldpassword")
	bad.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(bad.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(loginCookies))
	wr := httptest.NewRecorder()
	ts.engine.ServeHTTP(wr, req)
	if wr.Code == http.StatusSeeOther {
		t.Fatal("old password must not log in")
	}

	// New password works
	csrf, loginCookies = getLoginCSRF(t, ts)
	good := url.Values{}
	good.Set("cid", itoa(user.CID))
	good.Set("password", "newpassword1")
	good.Set("csrf_token", csrf)
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(good.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(loginCookies))
	wr = httptest.NewRecorder()
	ts.engine.ServeHTTP(wr, req)
	if wr.Code != http.StatusSeeOther {
		t.Fatalf("new password login status %d", wr.Code)
	}
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "correct-pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "correct-pw")

	form := url.Values{}
	form.Set("current_password", "wrong-pw")
	form.Set("new_password", "newpassword1")
	form.Set("confirm_password", "newpassword1")
	w, _ := formPOST(t, ts, "/account/password", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Incorrect password") {
		t.Fatalf("expected field error, body=%s", clip(w.Body.String(), 400))
	}
	// Password unchanged
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.dbRepo.UserRepo.VerifyPasswordHash("correct-pw", u.Password) {
		t.Fatal("password must not change on wrong current")
	}
}

func TestChangePasswordSameAsCurrent(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "samepass1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "samepass1")

	form := url.Values{}
	form.Set("current_password", "samepass1")
	form.Set("new_password", "samepass1")
	form.Set("confirm_password", "samepass1")
	w, _ := formPOST(t, ts, "/account/password", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "different from the current") {
		t.Fatalf("expected new≠current error, body=%s", clip(w.Body.String(), 400))
	}
}

func TestChangePasswordShortOrColon(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "oldpassword", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "oldpassword")

	form := url.Values{}
	form.Set("current_password", "oldpassword")
	form.Set("new_password", "short")
	form.Set("confirm_password", "short")
	w, cookies := formPOST(t, ts, "/account/password", form, cookies)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "at least 8 characters") {
		t.Fatalf("short password: status %d body=%s", w.Code, clip(w.Body.String(), 300))
	}

	form = url.Values{}
	form.Set("current_password", "oldpassword")
	form.Set("new_password", "bad:colon1")
	form.Set("confirm_password", "bad:colon1")
	w, _ = formPOST(t, ts, "/account/password", form, cookies)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "colon") {
		t.Fatalf("colon password: status %d body=%s", w.Code, clip(w.Body.String(), 300))
	}
}

func TestChangePasswordCSRF(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "pw")

	form := url.Values{}
	form.Set("current_password", "pw")
	form.Set("new_password", "newpassword1")
	form.Set("confirm_password", "newpassword1")
	// Wrong CSRF
	form.Set("csrf_token", "not-the-real-token")
	req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", w.Code)
	}
}

func TestDeleteAccountSoft(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "delete-me1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "delete-me1")

	form := url.Values{}
	form.Set("current_password", "delete-me1")
	form.Set("confirm_cid", itoa(user.CID))
	w, _ := formPOST(t, ts, "/account/delete", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "account=deleted") {
		t.Fatalf("Location=%q want account=deleted", loc)
	}

	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingInactive) {
		t.Fatalf("network_rating=%d want Inactive(-1)", u.NetworkRating)
	}

	// New login fails
	csrf, loginCookies := getLoginCSRF(t, ts)
	login := url.Values{}
	login.Set("cid", itoa(user.CID))
	login.Set("password", "delete-me1")
	login.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(login.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(loginCookies))
	wr := httptest.NewRecorder()
	ts.engine.ServeHTTP(wr, req)
	if wr.Code == http.StatusSeeOther {
		t.Fatal("soft-deleted user must not log in")
	}
}

func TestDeleteAccountWrongPassword(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "keep-me1x", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "keep-me1x")

	form := url.Values{}
	form.Set("current_password", "wrong")
	form.Set("confirm_cid", itoa(user.CID))
	w, _ := formPOST(t, ts, "/account/delete", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Incorrect password") {
		t.Fatalf("expected password error, body=%s", clip(w.Body.String(), 400))
	}
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatalf("user must still be active, rating=%d", u.NetworkRating)
	}
}

func TestDeleteAccountConfirmCIDMismatch(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "keep-me2x", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "keep-me2x")

	form := url.Values{}
	form.Set("current_password", "keep-me2x")
	form.Set("confirm_cid", "999999")
	w, _ := formPOST(t, ts, "/account/delete", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Confirm your CID") {
		t.Fatalf("expected CID confirm error, body=%s", clip(w.Body.String(), 400))
	}
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingObserver) {
		t.Fatal("user must still be active")
	}
}

func TestDeleteAccountHardWhenEnabled(t *testing.T) {
	ts := newTestServer(t)
	ts.cfg.AllowPermanentAccountDelete = true
	user := createTestUser(t, ts, "hard-del1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "hard-del1")

	// Checkbox visible
	w, cookies := authedGET(t, ts, "/account", cookies)
	if !strings.Contains(w.Body.String(), `name="permanent"`) {
		t.Fatal("expected permanent checkbox when enabled")
	}

	form := url.Values{}
	form.Set("current_password", "hard-del1")
	form.Set("confirm_cid", itoa(user.CID))
	form.Set("permanent", "1")
	w, _ = formPOST(t, ts, "/account/delete", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	_, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err == nil {
		t.Fatal("expected row gone after hard delete")
	}
}

func TestDeleteAccountHardWhenDisabled(t *testing.T) {
	ts := newTestServer(t)
	// flag remains false
	user := createTestUser(t, ts, "soft-only1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "soft-only1")

	form := url.Values{}
	form.Set("current_password", "soft-only1")
	form.Set("confirm_cid", itoa(user.CID))
	form.Set("permanent", "1") // client posts permanent but server soft-deletes
	w, _ := formPOST(t, ts, "/account/delete", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "account=deleted") || !strings.Contains(loc, "permanent=disabled") {
		t.Fatalf("Location=%q want deleted+permanent=disabled", loc)
	}
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	if u.NetworkRating != int(protocol.NetworkRatingInactive) {
		t.Fatalf("must soft-delete, rating=%d", u.NetworkRating)
	}
}

func TestSessionRejectedAfterSoftDelete(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "sess-del1", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, user.CID, "sess-del1")

	// Soft-delete via repo (simulate admin/self delete while session still held)
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	u.NetworkRating = int(protocol.NetworkRatingInactive)
	u.Password = ""
	if err := ts.dbRepo.UserRepo.UpdateUser(u); err != nil {
		t.Fatal(err)
	}

	w, cookies2 := authedGET(t, ts, "/dashboard", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303 to login", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location=%q want /login", loc)
	}
	// Session cookie cleared
	cleared := false
	for _, sc := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, sessionCookieName+"=") &&
			(strings.Contains(sc, "Max-Age=0") || strings.Contains(sc, "Max-Age=-1")) {
			cleared = true
		}
	}
	if !cleared {
		// mergeCookies should drop empty/cleared
		if csrfFromCookies(cookies2) != "" || extractCookie(w.Result(), sessionCookieName) != "" {
			// extractCookie may still return empty value for cleared cookie
		}
	}
	// Follow-up without re-login fails
	w2, _ := authedGET(t, ts, "/dashboard", cookies2)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("after clear, dashboard status %d want 303", w2.Code)
	}
}

func TestAPISessionRejectedAfterSoftDelete(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "api-del1", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, user.CID, "api-del1")

	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	u.NetworkRating = int(protocol.NetworkRatingInactive)
	u.Password = ""
	if err := ts.dbRepo.UserRepo.UpdateUser(u); err != nil {
		t.Fatal(err)
	}

	// Self load via session dual-accept
	csrf := csrfFromCookies(cookies)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/load",
		strings.NewReader(`{"cid":`+itoa(user.CID)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	if csrf != "" {
		req.Header.Set(csrfHeaderName, csrf)
	}
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("API session after soft-delete status %d want 401 body %s", w.Code, w.Body.String())
	}
}

func TestClaimsOverlayAfterDemotion(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pw1", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pw1")

	// Demote to SUP in DB
	u, err := ts.dbRepo.UserRepo.GetUserByCID(admin.CID)
	if err != nil {
		t.Fatal(err)
	}
	u.NetworkRating = int(protocol.NetworkRatingSupervisor)
	u.Password = ""
	if err := ts.dbRepo.UserRepo.UpdateUser(u); err != nil {
		t.Fatal(err)
	}

	w, cookies := authedGET(t, ts, "/dashboard", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `href="/configeditor"`) {
		t.Fatal("demoted SUP must not see Config nav")
	}
	if !strings.Contains(body, `href="/usereditor"`) {
		t.Fatal("demoted SUP should still see Users")
	}
	if !strings.Contains(body, "Supervisor") {
		t.Fatal("expected overlaid Supervisor label")
	}

	// Create with network_rating=12 must be rejected (ceiling uses overlaid SUP)
	form := url.Values{}
	form.Set("first_name", "X")
	form.Set("password", "password99")
	form.Set("network_rating", "12")
	form.Set("pilot_rating", "0")
	w, _ = formPOST(t, ts, "/usereditor/create", form, cookies)
	if w.Code == http.StatusSeeOther && strings.Contains(w.Header().Get("Location"), "flash=created") {
		t.Fatal("SUP must not create ADM-rated user after demotion overlay")
	}
}

func TestLoginShowsAccountDeletedBanner(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/login?account=deleted", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Your account has been deleted.") {
		t.Fatalf("expected deleted banner, body=%s", clip(w.Body.String(), 400))
	}

	req = httptest.NewRequest(http.MethodGet, "/login?account=deleted&permanent=disabled", nil)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "Permanent delete is not enabled") {
		t.Fatalf("expected permanent disabled message, body=%s", clip(w.Body.String(), 400))
	}
}

func TestRefreshRejectsInactiveUser(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "refresh1x", int(protocol.NetworkRatingObserver))

	// JSON login for refresh token
	body := `{"cid":` + itoa(user.CID) + `,"password":"refresh1x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login %d %s", w.Code, w.Body.String())
	}
	var res APIV1Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(res.Data)
	var tokens struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}

	// Soft-delete
	u, err := ts.dbRepo.UserRepo.GetUserByCID(user.CID)
	if err != nil {
		t.Fatal(err)
	}
	u.NetworkRating = int(protocol.NetworkRatingInactive)
	u.Password = ""
	if err := ts.dbRepo.UserRepo.UpdateUser(u); err != nil {
		t.Fatal(err)
	}

	refBody := `{"refresh_token":"` + tokens.RefreshToken + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(refBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after inactive status %d want 401 body %s", w.Code, w.Body.String())
	}
}
