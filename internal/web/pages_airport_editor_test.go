package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/renorris/openfsd/pkg/protocol"
)

func TestAirportEditorUnauthRedirect(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/airport-editor", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location=%q want /login", loc)
	}
}

func TestAirportEditorObserverRedirect(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")

	w, _ := authedGET(t, ts, "/airport-editor", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestAirportEditorSupervisorRedirect(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "pw")

	w, _ := authedGET(t, ts, "/airport-editor", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestAirportEditorAdminShell(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	w, _ := authedGET(t, ts, "/airport-editor", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	for _, want := range []string{
		`id="apted-root"`,
		`data-js="airport-editor"`,
		`name="apt_text"`,
		`name="air_text"`,
		`name="filename"`,
		`name="csrf_token"`,
		`action="/airport-editor/download-apt"`,
		`action="/airport-editor/download-air"`,
		`id="apted-map"`,
		`requires JavaScript`,
		`href="/airport-editor"`,
		// PR6: Leaflet + ES modules for map enhancement
		`/static/js/leaflet.js`,
		`/static/js/openfsd/leaflet.rotatedmarker.js`,
		`/static/js/openfsd/airport-editor/main.js`,
		`/static/css/leaflet.css`,
		`data-js="open-apt"`,
		`data-js="open-air"`,
		`data-js="fit"`,
		`data-js="layer"`,
		`data-js="rail-tabs"`,
		`data-js="inspector"`,
		`data-js="chip-icao"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, body=%s", want, clip(body, 800))
		}
	}
}

func TestAirportEditorNavOnlyForAdmin(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")
	w, _ := authedGET(t, ts, "/dashboard", cookies)
	body := w.Body.String()
	if strings.Contains(body, `href="/airport-editor"`) {
		t.Fatal("observer must not see Airport Editor nav")
	}

	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies = formLogin(t, ts, admin.CID, "admin-pass")
	w, _ = authedGET(t, ts, "/dashboard", cookies)
	body = w.Body.String()
	if !strings.Contains(body, `href="/airport-editor"`) {
		t.Fatal("admin dashboard should link to airport editor")
	}
}

func TestAirportEditorDownloadRequiresCSRF(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("apt_text", "icao=KBTV\n")
	form.Set("filename", "test.apt")
	// no csrf_token
	req := httptest.NewRequest(http.MethodPost, "/airport-editor/download-apt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", w.Code)
	}
}

func TestAirportEditorDownloadUnauthRedirect(t *testing.T) {
	ts := newTestServer(t)
	form := url.Values{}
	form.Set("apt_text", "icao=KBTV\n")
	form.Set("filename", "test.apt")
	form.Set("csrf_token", "not-a-real-token")
	req := httptest.NewRequest(http.MethodPost, "/airport-editor/download-apt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location=%q want /login", loc)
	}
	if cd := w.Header().Get("Content-Disposition"); strings.Contains(cd, "attachment") {
		t.Fatalf("unauth must not get attachment, Content-Disposition=%q", cd)
	}
}

func TestAirportEditorDownloadObserverRedirect(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")

	form := url.Values{}
	form.Set("apt_text", "icao=KBTV\n")
	form.Set("filename", "test.apt")
	// formPOST injects CSRF when present; observer should still be rating-gated.
	w, _ := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
	if cd := w.Header().Get("Content-Disposition"); strings.Contains(cd, "attachment") {
		t.Fatalf("observer must not get attachment, Content-Disposition=%q", cd)
	}
	if strings.Contains(w.Body.String(), "icao=KBTV") {
		t.Fatal("observer must not receive echo body")
	}
}

func TestAirportEditorDownloadAPTContentDisposition(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	payload := "icao=KBTV\nregistration=N\n"
	form := url.Values{}
	form.Set("apt_text", payload)
	form.Set("filename", "KBTV.apt")

	before := workdirSnapshot(t)
	w, _ := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	after := workdirSnapshot(t)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 body %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `attachment`) || !strings.Contains(cd, `filename="KBTV.apt"`) {
		t.Fatalf("Content-Disposition=%q", cd)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q want text/plain", ct)
	}
	if got := w.Body.String(); got != payload {
		t.Fatalf("body mismatch: got %q want %q", got, payload)
	}
	assertWorkdirUnchanged(t, before, after)
}

func TestAirportEditorDownloadAIRContentDisposition(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	payload := "AAL123:B738/F:J:I:KBTV:KBOS:29000::/v/:2200:S:44.47:-73.15:335:0:360\n"
	form := url.Values{}
	form.Set("air_text", payload)
	form.Set("filename", "scenario.air")

	before := workdirSnapshot(t)
	w, _ := formPOST(t, ts, "/airport-editor/download-air", form, cookies)
	after := workdirSnapshot(t)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 body %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `attachment`) || !strings.Contains(cd, `filename="scenario.air"`) {
		t.Fatalf("Content-Disposition=%q", cd)
	}
	if got := w.Body.String(); got != payload {
		t.Fatalf("body mismatch")
	}
	assertWorkdirUnchanged(t, before, after)
}

func TestAirportEditorDownloadOversizedBody(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	// Wire body exceeds MaxBytesReader (2 MiB + 4 KiB slack). Fail closed with size flash.
	oversized := strings.Repeat("A", airportEditorWebMaxBody+8192)
	form := url.Values{}
	form.Set("apt_text", oversized)
	form.Set("filename", "big.apt")

	before := workdirSnapshot(t)
	w, cookies := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	after := workdirSnapshot(t)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303 (size flash), body=%s", w.Code, clip(w.Body.String(), 200))
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "flash=err") {
		t.Fatalf("Location=%q want flash=err", loc)
	}
	if cd := w.Header().Get("Content-Disposition"); strings.Contains(cd, "attachment") {
		t.Fatalf("oversized must not attach, Content-Disposition=%q", cd)
	}
	w, _ = authedGET(t, ts, loc, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("follow flash status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Payload too large") {
		t.Fatalf("expected size flash, body=%s", clip(w.Body.String(), 500))
	}
	assertWorkdirUnchanged(t, before, after)
}

func TestAirportEditorDownloadLargeButUnderLimit(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	// Comfortably under 2 MiB after form encoding; exercises large-payload success path.
	payload := strings.Repeat("icao=TEST\n", 32*1024) // ~288 KiB
	form := url.Values{}
	form.Set("apt_text", payload)
	form.Set("filename", "large.apt")

	w, _ := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200 body %s", w.Code, clip(w.Body.String(), 200))
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `attachment`) || !strings.Contains(cd, `filename="large.apt"`) {
		t.Fatalf("Content-Disposition=%q", cd)
	}
	if got := w.Body.String(); got != payload {
		t.Fatalf("body length mismatch: got %d want %d", len(got), len(payload))
	}
}

func TestAirportEditorDownloadUnsafeFilenameFallsBack(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("apt_text", "icao=X\n")
	form.Set("filename", "../../etc/passwd")

	w, _ := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d want 200", w.Code)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="airport.apt"`) {
		t.Fatalf("expected default filename, Content-Disposition=%q", cd)
	}
}

func TestAirportEditorDownloadEmptyRedirectsError(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("apt_text", "   ")
	form.Set("filename", "empty.apt")
	w, cookies := formPOST(t, ts, "/airport-editor/download-apt", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "flash=err") {
		t.Fatalf("Location=%q want flash=err", loc)
	}
	w, _ = authedGET(t, ts, loc, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("follow flash status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Text is required") {
		t.Fatalf("expected empty-text flash, body=%s", clip(w.Body.String(), 500))
	}
}

func TestSafeAirportEditorFilename(t *testing.T) {
	cases := []struct {
		raw, def, ext, want string
	}{
		{"", "airport.apt", ".apt", "airport.apt"},
		{"KBTV.apt", "airport.apt", ".apt", "KBTV.apt"},
		{"foo.air", "airport.apt", ".apt", "airport.apt"}, // wrong ext
		{"../x.apt", "airport.apt", ".apt", "airport.apt"},
		{"bad name.apt", "airport.apt", ".apt", "airport.apt"},
		{"scenario.air", "scenario.air", ".air", "scenario.air"},
		{"A_B-1.apt", "airport.apt", ".apt", "A_B-1.apt"},
	}
	for _, tc := range cases {
		got := safeAirportEditorFilename(tc.raw, tc.def, tc.ext)
		if got != tc.want {
			t.Errorf("safeAirportEditorFilename(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
}

// workdirSnapshot lists basenames in the process working directory.
func workdirSnapshot(t *testing.T) map[string]struct{} {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	out := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		out[e.Name()] = struct{}{}
	}
	return out
}

func assertWorkdirUnchanged(t *testing.T, before, after map[string]struct{}) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("workdir entry count changed: before=%d after=%d", len(before), len(after))
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			t.Fatalf("workdir lost entry %q", name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Fatalf("workdir gained entry %q (download must not write files)", name)
		}
	}
}
