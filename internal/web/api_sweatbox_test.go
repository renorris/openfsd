package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/serviceapi"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPISweatboxStateUnauth(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sweatbox/state", nil)
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d want 401", w.Code)
	}
}

func TestAPISweatboxStateForbiddenForObserver(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.observer.CID, env.observerPass)
	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/state", nil, access)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestAPISweatboxStateProxiesFSD(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/state", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var st serviceapi.SweatboxStateJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.Equal(t, "KBTV", st.ICAO)
	require.Len(t, st.Aircraft, 1)
	assert.Equal(t, "AAL123", st.Aircraft[0].Callsign)
	assert.Equal(t, 65.0, st.Elapsed)
}

func TestAPISweatboxStateCookieSession(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "pw", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "pw")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sweatbox/state", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var st serviceapi.SweatboxStateJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.Equal(t, "KBTV", st.ICAO)
}

func TestAPISweatboxStateDisabled404(t *testing.T) {
	mux := http.NewServeMux()
	// No /sweatbox/state → FSD returns 404
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/state", nil, access)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPISweatboxStateUnreachable502(t *testing.T) {
	env := setupTestAPI(t)
	// Default FsdHttpServiceAddress is unreachable
	access, _ := env.login(t, env.admin.CID, env.adminPass)
	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/state", nil, access)
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "unreachable")
}

func TestAPISweatboxOpsProxiesFSD(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sweatbox/ops", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(serviceapi.SweatboxOpsJSON{
			ElapsedSec: 12,
			ArrCount:   3,
			DepCount:   1,
			OpsPerMin:  0.5,
			Message:    "ok",
		})
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/ops", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"arr_count":3`)
}

func TestSweatboxPagePEHooksWhenAvailable(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	// PE root + availability flag
	if !strings.Contains(body, `data-js="sweatbox"`) {
		t.Fatal("expected data-js=sweatbox root")
	}
	if !strings.Contains(body, `data-available="1"`) {
		t.Fatal("expected data-available=1 when FSD state OK")
	}
	if !strings.Contains(body, `data-poll-ms="1500"`) {
		t.Fatal("expected poll interval attribute")
	}

	// Status hooks
	for _, hook := range []string{
		`data-js="icao"`,
		`data-js="paused"`,
		`data-js="elapsed"`,
		`data-js="arr-dep"`,
		`data-js="aircraft-tbody"`,
		`data-js="pause-btn"`,
		`data-js="unpause-btn"`,
		`data-js="live-status"`,
	} {
		if !strings.Contains(body, hook) {
			t.Fatalf("missing PE hook %s", hook)
		}
	}

	// Script loaded only on available path
	if !strings.Contains(body, `/static/js/openfsd/sweatbox.js`) {
		t.Fatal("expected sweatbox.js script tag")
	}

	// No Leaflet (map not on this page)
	if strings.Contains(body, "leaflet") {
		t.Fatal("did not expect Leaflet on sweatbox page")
	}

	// Forms still present (command authority server-side)
	if !strings.Contains(body, `method="post" action="/sweatbox/command"`) {
		t.Fatal("expected command form")
	}
	if !strings.Contains(body, `method="post" action="/sweatbox/delete"`) {
		t.Fatal("expected delete form")
	}
}

func TestSweatboxPageNoPEScriptWhenUnavailable(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	w, _ := authedGET(t, ts, "/sweatbox", cookies)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	if !strings.Contains(body, `data-available="0"`) {
		t.Fatalf("expected data-available=0 when FSD down, body=%s", clip(body, 400))
	}
	// Script is inside the Available branch only
	if strings.Contains(body, `/static/js/openfsd/sweatbox.js`) {
		t.Fatal("should not load sweatbox.js when control plane unavailable")
	}
}
