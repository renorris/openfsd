package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/renorris/openfsd/internal/serviceapi"
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

// sweatboxMock tracks FSD service HTTP interactions for instructor UI tests.
type sweatboxMock struct {
	paused          bool
	lastCommand     serviceapi.SweatboxCommandRequest
	airportBody     string
	airportPath     string
	scenarioBody    string
	commandSoftFail bool
	airportConflict bool
	scenarioBadJSON bool
}

func (m *sweatboxMock) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sweatbox/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		st := serviceapi.SweatboxStateJSON{
			ICAO:     "KBTV",
			Paused:   m.paused,
			Elapsed:  65,
			ArrCount: 1,
			DepCount: 2,
			Aircraft: []serviceapi.SweatboxAircraftJSON{
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
		m.airportPath = r.URL.RequestURI()
		b, _ := io.ReadAll(r.Body)
		m.airportBody = string(b)
		if m.airportConflict {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []string{"aircraft are present; use replace"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"icao": "KBTV", "surfaces": 3, "errors": []string{}})
	})
	mux.HandleFunc("/sweatbox/scenario", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.scenarioBody = string(b)
		if m.scenarioBadJSON {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not-json"))
			return
		}
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxScenarioResponse{
			Loaded: 3,
			Errors: []string{"line 9: skipped"},
		})
	})
	mux.HandleFunc("/sweatbox/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&m.lastCommand)
		if m.commandSoftFail {
			_ = json.NewEncoder(w).Encode(serviceapi.SweatboxCommandResponse{
				OK:      false,
				Message: "Unknown command: xyz",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxCommandResponse{
			OK:      true,
			Message: "ok: " + m.lastCommand.Command,
		})
	})
	mux.HandleFunc("/sweatbox/pause", func(w http.ResponseWriter, r *http.Request) {
		m.paused = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/sweatbox/unpause", func(w http.ResponseWriter, r *http.Request) {
		m.paused = false
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
	return mux
}

func TestSweatboxWithMockFSDStateAndForms(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
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
	if m.lastCommand.Command != "ops" || m.lastCommand.Callsign != "AAL123" {
		t.Fatalf("lastCommand=%+v", m.lastCommand)
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
	if !strings.Contains(m.airportBody, "icao=KBTV") {
		t.Fatalf("airportBody=%q", m.airportBody)
	}

	// Airport with replace=1 proxy query
	form = url.Values{}
	form.Set("apt_text", "icao=KBTV\n")
	form.Set("replace", "on")
	w, cookies = formPOST(t, ts, "/sweatbox/airport", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("airport replace status %d", w.Code)
	}
	if !strings.Contains(m.airportPath, "replace=1") {
		t.Fatalf("airportPath=%q want ?replace=1", m.airportPath)
	}

	// Scenario paste
	form = url.Values{}
	form.Set("air_text", "AAL123:B738/F:J:I:KBTV:KBOS:29000:DCT:rmk:2200:S:44.4:-73.1:335:0:360\n")
	w, cookies = formPOST(t, ts, "/sweatbox/scenario", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("scenario status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=scenario_ok") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
	if !strings.Contains(m.scenarioBody, "AAL123") {
		t.Fatalf("scenarioBody=%q", m.scenarioBody)
	}
	w, cookies = authedGET(t, ts, w.Header().Get("Location"), cookies)
	if !strings.Contains(w.Body.String(), "Scenario loaded: 3 aircraft") {
		t.Fatalf("expected scenario flash, body=%s", clip(w.Body.String(), 400))
	}
	if !strings.Contains(w.Body.String(), "1 warning") {
		t.Fatalf("expected warning count in flash, body=%s", clip(w.Body.String(), 400))
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
	if !m.paused {
		t.Fatal("expected mock paused")
	}

	// Unpause
	form = url.Values{}
	w, cookies = formPOST(t, ts, "/sweatbox/unpause", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("unpause status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=unpaused") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
	if m.paused {
		t.Fatal("expected mock unpaused")
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

func TestSweatboxMultipartFileUploadAndErrors(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")
	// Ensure CSRF cookie
	_, cookies = authedGET(t, ts, "/sweatbox", cookies)

	// Multipart airport file upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", csrfFromCookies(cookies))
	fw, err := mw.CreateFormFile("file", "KBTV.apt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("icao=KBTV\nmagnetic variation=16\n")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/sweatbox/airport", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("multipart airport status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=airport_ok") {
		t.Fatalf("Location=%q", w.Header().Get("Location"))
	}
	if !strings.Contains(m.airportBody, "icao=KBTV") {
		t.Fatalf("airportBody from multipart=%q", m.airportBody)
	}
	cookies = mergeCookies(cookies, w.Result())

	// Soft-fail command
	m.commandSoftFail = true
	form := url.Values{}
	form.Set("command", "xyz")
	w, cookies = formPOST(t, ts, "/sweatbox/command", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("soft-fail command status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "flash=err") {
		t.Fatalf("Location=%q want flash=err", loc)
	}
	w, cookies = authedGET(t, ts, loc, cookies)
	if !strings.Contains(w.Body.String(), "Unknown command: xyz") {
		t.Fatalf("expected soft-fail message, body=%s", clip(w.Body.String(), 400))
	}

	// Airport conflict surfaces firstJSONError from errors[]
	m.airportConflict = true
	form = url.Values{}
	form.Set("apt_text", "icao=KBTV\n")
	w, cookies = formPOST(t, ts, "/sweatbox/airport", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("conflict airport status %d", w.Code)
	}
	loc = w.Header().Get("Location")
	if !strings.Contains(loc, "flash=err") {
		t.Fatalf("Location=%q", loc)
	}
	w, cookies = authedGET(t, ts, loc, cookies)
	if !strings.Contains(w.Body.String(), "aircraft are present") {
		t.Fatalf("expected conflict error flash, body=%s", clip(w.Body.String(), 400))
	}

	// Scenario bad JSON → parse error flash
	m.scenarioBadJSON = true
	form = url.Values{}
	form.Set("air_text", "AAL1:B738:J:I:KBTV:KBOS:100:DCT::2200:S:0:0:0:0:0\n")
	w, cookies = formPOST(t, ts, "/sweatbox/scenario", form, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("bad scenario JSON status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "flash=err") {
		t.Fatalf("Location=%q want flash=err", w.Header().Get("Location"))
	}
	w, _ = authedGET(t, ts, w.Header().Get("Location"), cookies)
	if !strings.Contains(w.Body.String(), "Unable to parse scenario response") {
		t.Fatalf("expected parse error, body=%s", clip(w.Body.String(), 400))
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
		st := serviceapi.SweatboxStateJSON{
			ICAO: "KBTV",
			Aircraft: []serviceapi.SweatboxAircraftJSON{
				{
					Callsign:    `"><script>alert(1)</script>`,
					Type:        `B738"><img src=x>`,
					Status:      `parked`,
					Instruction: `<b>bad</b>`,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("/sweatbox/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxCommandResponse{
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

	// No raw executable script tags.
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("unescaped callsign script in HTML")
	}
	if strings.Contains(body, "<script>alert") {
		t.Fatal("executable script present in table HTML")
	}
	// Positive assertions: html/template entity-escapes both content and attribute contexts.
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected &lt;script&gt; entity escape in body: %s", clip(body, 800))
	}
	// Callsign is also used in delete form value="…"; quote must be escaped.
	if !strings.Contains(body, "&#34;") && !strings.Contains(body, "&quot;") {
		t.Fatalf("expected quote entity escape for attribute-safe callsign, body=%s", clip(body, 800))
	}
	// Hidden callsign input should not contain raw quote breakout.
	if strings.Contains(body, `name="callsign" value=""><script>`) {
		t.Fatal("unescaped callsign broke out of value attribute")
	}

	form := url.Values{}
	form.Set("command", "ops")
	w, cookies = formPOST(t, ts, "/sweatbox/command", form, cookies)
	w, _ = authedGET(t, ts, w.Header().Get("Location"), cookies)
	body = w.Body.String()
	if strings.Contains(body, `<script>alert("xss")</script>`) {
		t.Fatal("unescaped flash script")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped flash script entities, body=%s", clip(body, 500))
	}
}

func TestSweatboxFlashMsgTruncated(t *testing.T) {
	// Unit-level: truncateRunes keeps Location headers bounded.
	long := strings.Repeat("a", sweatboxFlashMsgMaxRunes+50)
	got := truncateRunes(long, sweatboxFlashMsgMaxRunes)
	if utf8.RuneCountInString(got) != sweatboxFlashMsgMaxRunes {
		t.Fatalf("rune count %d want %d", utf8.RuneCountInString(got), sweatboxFlashMsgMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsis suffix, got %q", got[len(got)-3:])
	}

	// Integration: oversized FSD command message is truncated in redirect.
	mux := http.NewServeMux()
	mux.HandleFunc("/sweatbox/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxStateJSON{ICAO: "KBTV", Aircraft: []serviceapi.SweatboxAircraftJSON{}})
	})
	mux.HandleFunc("/sweatbox/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxCommandResponse{
			OK:      true,
			Message: strings.Repeat("x", 500),
		})
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	form := url.Values{}
	form.Set("command", "ops")
	w, _ := formPOST(t, ts, "/sweatbox/command", form, cookies)
	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	msg := u.Query().Get("msg")
	if utf8.RuneCountInString(msg) > sweatboxFlashMsgMaxRunes {
		t.Fatalf("msg runes %d > max %d", utf8.RuneCountInString(msg), sweatboxFlashMsgMaxRunes)
	}
	if len(loc) > 2048 {
		t.Fatalf("Location header too long: %d", len(loc))
	}
}

func TestFirstJSONError(t *testing.T) {
	if got := firstJSONError([]byte(`{"message":"  hello  "}`), "fb"); got != "hello" {
		t.Fatalf("message field: %q", got)
	}
	if got := firstJSONError([]byte(`{"errors":["e1","e2"]}`), "fb"); got != "e1" {
		t.Fatalf("errors[0]: %q", got)
	}
	if got := firstJSONError([]byte(`{"error":"boom"}`), "fb"); got != "boom" {
		t.Fatalf("error field: %q", got)
	}
	if got := firstJSONError([]byte(`not-json`), "fb"); got != "fb" {
		t.Fatalf("fallback: %q", got)
	}
	if got := firstJSONError([]byte(`{}`), "fb"); got != "fb" {
		t.Fatalf("empty object fallback: %q", got)
	}
}

func TestIsRequestTooLarge(t *testing.T) {
	if !isRequestTooLarge(&http.MaxBytesError{Limit: 10}) {
		t.Fatal("MaxBytesError should match")
	}
	if !isRequestTooLarge(fmt.Errorf("wrap: %w", &http.MaxBytesError{Limit: 1})) {
		t.Fatal("wrapped MaxBytesError should match")
	}
	if isRequestTooLarge(fmt.Errorf("other")) {
		t.Fatal("unrelated error should not match")
	}
	if isRequestTooLarge(nil) {
		t.Fatal("nil should not match")
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
