package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/protocol"
)

func TestSweatboxPageUnauthRedirect(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/sweatbox", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location=%q want /login", loc)
	}
}

func TestSweatboxPageObserverRedirect(t *testing.T) {
	ts := newTestServer(t)
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestSweatboxPageSupervisorRedirect(t *testing.T) {
	ts := newTestServer(t)
	sup := createTestUser(t, ts, "pw", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, sup.CID, "pw")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q want /dashboard", loc)
	}
}

func TestSweatboxPageAdminShowsUnavailableWhenFSDDown(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Sweatbox instructor") {
		t.Fatalf("expected page title content, body=%s", clip(body, 400))
	}
	if !strings.Contains(body, "Unavailable") && !strings.Contains(body, "not enabled") {
		t.Fatalf("expected unavailable/disabled messaging, body=%s", clip(body, 600))
	}
	// Nav link present for admins
	if !strings.Contains(body, `href="/sweatbox"`) {
		t.Fatal("expected sweatbox nav link")
	}
	// Forms must not be required when control plane is down — no command form action expected if unavailable.
	// (Primary path still works: clear message without JS.)
	if !strings.Contains(body, "SWEATBOX_ENABLED") {
		t.Fatalf("expected SWEATBOX_ENABLED guidance, body=%s", clip(body, 600))
	}
}

func TestSweatboxNavOnlyForAdmin(t *testing.T) {
	ts := newTestServer(t)
	// Observer dashboard: no sweatbox nav
	obs := createTestUser(t, ts, "pw", int(protocol.NetworkRatingObserver))
	cookies := formLogin(t, ts, obs.CID, "pw")
	w, _ := authedGET(t, ts, "/dashboard", cookies)
	body := w.Body.String()
	// Layout nav should not include Sweatbox for non-admin (CanEditConfig false).
	// Dashboard may still mention connections; check header area for Config/Sweatbox pair.
	if strings.Contains(body, `href="/sweatbox">Sweatbox</a>`) {
		t.Fatal("observer must not see Sweatbox nav")
	}

	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies = formLogin(t, ts, admin.CID, "admin-pass")
	w, _ = authedGET(t, ts, "/dashboard", cookies)
	body = w.Body.String()
	if !strings.Contains(body, `href="/sweatbox"`) {
		t.Fatal("admin dashboard should link to sweatbox")
	}
}

func TestSweatboxPOSTRequiresCSRF(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("command", "ops")
	// no csrf_token
	req := httptest.NewRequest(http.MethodPost, "/sweatbox/command", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", w.Code)
	}
}

func TestSweatboxCommandEmptyRedirectsErrorFlash(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("command", "   ")
	w, cookies := formPOST(t, ts, "/sweatbox/command", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303 body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "flash=err") {
		t.Fatalf("Location=%q want flash=err", loc)
	}

	w, _ = authedGET(t, ts, loc, cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("follow flash status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Command is required") {
		t.Fatalf("expected command required flash, body=%s", clip(w.Body.String(), 500))
	}
}

func TestSweatboxWithMockFSDStateAndForms(t *testing.T) {
	// Mock FSD service HTTP sweatbox surface.
	var lastCommand server.SweatboxCommandRequest
	var airportBody string
	var paused bool
	mux := http.NewServeMux()
	mux.HandleFunc("/sweatbox/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		st := server.SweatboxStateJSON{
			ICAO:     "KBTV",
			Paused:   paused,
			Elapsed:  65,
			ArrCount: 1,
			DepCount: 2,
			Aircraft: []server.SweatboxAircraftJSON{
				{
					Callsign:    "AAL123",
					Type:        "B738",
					Rules:       "I",
					Squawk:      "2200",
					Heading:     360,
					Alt:         335,
					Speed:       0,
					Status:      "parked",
					Instruction: "",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("/sweatbox/airport", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		airportBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{"icao": "KBTV", "surfaces": 3, "errors": []string{}})
	})
	mux.HandleFunc("/sweatbox/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&lastCommand)
		_ = json.NewEncoder(w).Encode(server.SweatboxCommandResponse{OK: true, Message: "ok: " + lastCommand.Command})
	})
	mux.HandleFunc("/sweatbox/pause", func(w http.ResponseWriter, r *http.Request) {
		paused = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/sweatbox/unpause", func(w http.ResponseWriter, r *http.Request) {
		paused = false
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/sweatbox/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/sweatbox/aircraft", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL

	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	// GET page with live state
	w, cookies := authedGET(t, ts, "/sweatbox", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /sweatbox status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "KBTV") {
		t.Fatalf("expected ICAO in page, body=%s", clip(body, 600))
	}
	if !strings.Contains(body, "AAL123") {
		t.Fatal("expected aircraft callsign in table")
	}
	if !strings.Contains(body, `method="post" action="/sweatbox/command"`) {
		t.Fatal("expected command form")
	}
	if !strings.Contains(body, `method="post" action="/sweatbox/airport"`) {
		t.Fatal("expected airport form")
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatal("expected CSRF fields")
	}
	if !strings.Contains(body, "0:01:05") {
		t.Fatalf("expected formatted elapsed, body=%s", clip(body, 400))
	}

	// Command form POST → PRG flash
	form := url.Values{}
	form.Set("command", "ops")
	form.Set("callsign", "AAL123")
	w, cookies = formPOST(t, ts, "/sweatbox/command", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("command status %d body %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "flash=ok") {
		t.Fatalf("Location=%q", loc)
	}
	if lastCommand.Command != "ops" || lastCommand.Callsign != "AAL123" {
		t.Fatalf("lastCommand=%+v", lastCommand)
	}
	w, cookies = authedGET(t, ts, loc, cookies)
	if !strings.Contains(w.Body.String(), "ok: ops") {
		t.Fatalf("expected command flash, body=%s", clip(w.Body.String(), 400))
	}

	// Airport paste
	form = url.Values{}
	form.Set("apt_text", "icao=KBTV\nmagnetic variation=16\n")
	w, cookies = formPOST(t, ts, "/sweatbox/airport", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("airport status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=airport_ok") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
	if !strings.Contains(airportBody, "icao=KBTV") {
		t.Fatalf("airportBody=%q", airportBody)
	}

	// Pause
	form = url.Values{}
	w, cookies = formPOST(t, ts, "/sweatbox/pause", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("pause status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=paused") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
	if !paused {
		t.Fatal("expected mock paused")
	}

	// Delete aircraft
	form = url.Values{}
	form.Set("callsign", "AAL123")
	w, cookies = formPOST(t, ts, "/sweatbox/delete", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("delete status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=deleted") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}

	// Delete-all without confirm
	form = url.Values{}
	w, cookies = formPOST(t, ts, "/sweatbox/delete-all", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("delete-all no confirm status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=err") {
		t.Fatalf("Location=%q want err without confirm", w.Header().Get("Location"))
	}

	// Delete-all with confirm
	form = url.Values{}
	form.Set("confirm", "on")
	w, _ = formPOST(t, ts, "/sweatbox/delete-all", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("delete-all status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=deleted_all") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
}

func TestSweatboxDisabledMessagingWhenFSD404(t *testing.T) {
	mux := http.NewServeMux()
	// No /sweatbox routes → 404
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL

	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Sweatbox disabled") && !strings.Contains(body, "not enabled") {
		t.Fatalf("expected disabled messaging, body=%s", clip(body, 500))
	}
	if strings.Contains(body, `action="/sweatbox/command"`) {
		t.Fatal("forms should not render when sweatbox is disabled")
	}
}

func TestSweatboxXSSEscapedInFlashAndTable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sweatbox/state", func(w http.ResponseWriter, r *http.Request) {
		st := server.SweatboxStateJSON{
			ICAO: "KBTV",
			Aircraft: []server.SweatboxAircraftJSON{
				{
					Callsign:    `<script>alert(1)</script>`,
					Type:        `B738"><img src=x>`,
					Status:      `parked`,
					Instruction: `<b>bad</b>`,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("/sweatbox/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(server.SweatboxCommandResponse{
			OK:      true,
			Message: `<script>alert("xss")</script>`,
		})
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	w, cookies := authedGET(t, ts, "/sweatbox", cookies)
	body := w.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("unescaped callsign script in HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, "&#34;") {
		// html/template escapes < as &lt;
		if strings.Contains(body, "<script>alert") {
			t.Fatal("executable script present")
		}
	}

	form := url.Values{}
	form.Set("command", "ops")
	w, cookies = formPOST(t, ts, "/sweatbox/command", form, cookies)
	w, _ = authedGET(t, ts, w.Header().Get("Location"), cookies)
	body = w.Body.String()
	if strings.Contains(body, `<script>alert("xss")</script>`) {
		t.Fatal("unescaped flash script")
	}
}

func TestFormatSweatboxElapsed(t *testing.T) {
	if got := formatSweatboxElapsed(0); got != "0:00:00" {
		t.Fatalf("got %q", got)
	}
	if got := formatSweatboxElapsed(3661); got != "1:01:01" {
		t.Fatalf("got %q", got)
	}
}
