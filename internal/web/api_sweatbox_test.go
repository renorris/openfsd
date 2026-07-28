package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/db"
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

func TestAPISweatboxStateAllowedForInstructor(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL

	i1Pass := "i1pass123"
	i1 := &db.User{
		Password:      i1Pass,
		FirstName:     strPtr("Inst"),
		LastName:      strPtr("One"),
		NetworkRating: int(protocol.NetworkRatingInstructor1),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(context.Background(), i1))

	access, _ := env.login(t, i1.CID, i1Pass)
	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/state", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var st serviceapi.SweatboxStateJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.Equal(t, "KBTV", st.ICAO)
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

func createInstructor1(t *testing.T, env *testAPIEnv) (cid int, access string) {
	t.Helper()
	pass := "i1pass123"
	u := &db.User{
		Password:      pass,
		FirstName:     strPtr("Inst"),
		LastName:      strPtr("One"),
		NetworkRating: int(protocol.NetworkRatingInstructor1),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(context.Background(), u))
	access, _ = env.login(t, u.CID, pass)
	return u.CID, access
}

func TestAPISweatboxSessionEnveloped(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	_, access := createInstructor1(t, env)

	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/session", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err)
	require.Equal(t, "v1", res.Version)

	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var st serviceapi.SweatboxStateJSON
	require.NoError(t, json.Unmarshal(data, &st))
	assert.Equal(t, "KBTV", st.ICAO)
	require.Len(t, st.Aircraft, 1)
	assert.Equal(t, "AAL123", st.Aircraft[0].Callsign)

	assertGoldenJSON(t, "2026-07-28/sweatbox_session_ok.json", w.Body.Bytes())
}

func TestAPISweatboxSessionDisabled404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	fsd := httptest.NewServer(mux)
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/session", nil, access)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "not enabled")
}

func TestAPISweatboxSessionForbiddenForObserver(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.observer.CID, env.observerPass)
	w := env.doJSON(t, http.MethodGet, "/api/v1/sweatbox/session", nil, access)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestAPISweatboxAirportJSONProxiesTextPlain(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	_, access := createInstructor1(t, env)

	apt := "icao=KBTV\n//test apt"
	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/airport", map[string]any{
		"text":    apt,
		"replace": true,
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// FSD received text/plain body and replace query
	assert.Equal(t, apt, m.airportBody)
	assert.Contains(t, m.airportPath, "replace=1")
	assert.Equal(t, "text/plain; charset=utf-8", m.airportContentType)

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err)
	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var loaded serviceapi.SweatboxAirportLoadResponse
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, "KBTV", loaded.ICAO)
	assert.Equal(t, 3, loaded.Surfaces)
	assertGoldenJSON(t, "2026-07-28/sweatbox_airport_ok.json", w.Body.Bytes())
}

func TestAPISweatboxAirportConflict409(t *testing.T) {
	m := &sweatboxMock{airportConflict: true}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/airport", map[string]any{
		"text": "icao=KBTV\n",
	}, access)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "aircraft")
}

func TestAPISweatboxAirportEmptyText400(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)
	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/airport", map[string]any{
		"text": "   ",
	}, access)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAPISweatboxScenarioOK(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	air := "AAL123 B738 ..."
	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/scenario", map[string]any{
		"text": air,
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, air, m.scenarioBody)
	assert.Equal(t, "text/plain; charset=utf-8", m.scenarioContentType)

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err)
	assertGoldenJSON(t, "2026-07-28/sweatbox_scenario_ok.json", w.Body.Bytes())
}

func TestAPISweatboxCommandSoftFailStays200(t *testing.T) {
	m := &sweatboxMock{commandSoftFail: true}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/command", map[string]any{
		"callsign": "AAL123",
		"command":  "xyz",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err, "soft-fail must not set envelope err")
	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var cmd serviceapi.SweatboxCommandResponse
	require.NoError(t, json.Unmarshal(data, &cmd))
	assert.False(t, cmd.OK)
	assert.Contains(t, cmd.Message, "Unknown command")
	assertGoldenJSON(t, "2026-07-28/sweatbox_command_softfail.json", w.Body.Bytes())
}

func TestAPISweatboxCommandOK(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/command", map[string]any{
		"callsign": "AAL123",
		"command":  "taxi A",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "AAL123", m.lastCommand.Callsign)
	assert.Equal(t, "taxi A", m.lastCommand.Command)

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err)
	assertGoldenJSON(t, "2026-07-28/sweatbox_command_ok.json", w.Body.Bytes())
}

func TestAPISweatboxCommandMissing400(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)
	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/command", map[string]any{
		"callsign": "AAL123",
		"command":  "  ",
	}, access)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAPISweatboxPauseUnpause204To200(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/pause", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, m.paused)
	assertGoldenJSON(t, "2026-07-28/sweatbox_pause_ok.json", w.Body.Bytes())

	w = env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/unpause", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, m.paused)
	assertGoldenJSON(t, "2026-07-28/sweatbox_unpause_ok.json", w.Body.Bytes())
}

func TestAPISweatboxDeleteAircraft(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodDelete, "/api/v1/sweatbox/aircraft/AAL123", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertGoldenJSON(t, "2026-07-28/sweatbox_delete_ok.json", w.Body.Bytes())
}

func TestAPISweatboxDeleteAllRequiresConfirm(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	// Missing confirm → 400
	w := env.doJSON(t, http.MethodDelete, "/api/v1/sweatbox/aircraft", nil, access)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "confirm")

	// JSON confirm
	w = env.doJSON(t, http.MethodDelete, "/api/v1/sweatbox/aircraft", map[string]any{
		"confirm": true,
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertGoldenJSON(t, "2026-07-28/sweatbox_delete_all_ok.json", w.Body.Bytes())
}

func TestAPISweatboxDeleteAllQueryConfirm(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	env := setupTestAPI(t)
	env.server.cfg.FsdHttpServiceAddress = fsd.URL
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodDelete, "/api/v1/sweatbox/aircraft?confirm=1", nil, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAPISweatboxMutationsUnreachable502(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/sweatbox/pause", nil, access)
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "unreachable")
}

func TestAPISweatboxMutationsForbiddenForObserver(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.observer.CID, env.observerPass)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/sweatbox/session", nil},
		{http.MethodPost, "/api/v1/sweatbox/airport", map[string]any{"text": "icao=KBTV\n"}},
		{http.MethodPost, "/api/v1/sweatbox/scenario", map[string]any{"text": "AAL1"}},
		{http.MethodPost, "/api/v1/sweatbox/command", map[string]any{"command": "ops"}},
		{http.MethodPost, "/api/v1/sweatbox/pause", nil},
		{http.MethodPost, "/api/v1/sweatbox/unpause", nil},
		{http.MethodDelete, "/api/v1/sweatbox/aircraft/AAL123", nil},
		{http.MethodDelete, "/api/v1/sweatbox/aircraft?confirm=1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := env.doJSON(t, tc.method, tc.path, tc.body, access)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Empty(t, w.Header().Get("Location"))
			res := decodeAPIV1(t, w)
			require.NotNil(t, res.Err)
			assert.Equal(t, "forbidden", *res.Err)
		})
	}
}

func TestAPISweatboxPauseCookieSessionCSRF(t *testing.T) {
	m := &sweatboxMock{}
	fsd := httptest.NewServer(m.handler())
	t.Cleanup(fsd.Close)

	ts := newTestServer(t)
	ts.cfg.FsdHttpServiceAddress = fsd.URL
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")
	_, cookies = authedGET(t, ts, "/dashboard", cookies)
	csrf := csrfFromCookies(cookies)
	require.NotEmpty(t, csrf)

	// Cookie session without CSRF → 403 JSON.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sweatbox/pause", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get("Location"))
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "CSRF")
	assert.False(t, m.paused)

	// Cookie session with CSRF header → 200.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sweatbox/pause", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrf)
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res = decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	assert.True(t, m.paused)
	assertGoldenJSON(t, "2026-07-28/sweatbox_pause_ok.json", w.Body.Bytes())
}

func TestAPISweatboxErrorStatusMapping(t *testing.T) {
	// Table-driven Stable §D error statuses not covered by success goldens.
	cases := []struct {
		name       string
		mock       sweatboxMock
		method     string
		path       string
		body       any
		wantStatus int
		errSubstr  string
	}{
		{
			name:       "airport FSD 413",
			mock:       sweatboxMock{airportTooLarge: true},
			method:     http.MethodPost,
			path:       "/api/v1/sweatbox/airport",
			body:       map[string]any{"text": "icao=KBTV\n"},
			wantStatus: http.StatusRequestEntityTooLarge,
			errSubstr:  "too large",
		},
		{
			name:       "airport FSD 200 invalid JSON → 502",
			mock:       sweatboxMock{airportBadJSON: true},
			method:     http.MethodPost,
			path:       "/api/v1/sweatbox/airport",
			body:       map[string]any{"text": "icao=KBTV\n"},
			wantStatus: http.StatusBadGateway,
			errSubstr:  "invalid JSON",
		},
		{
			name:       "scenario 409 no airport",
			mock:       sweatboxMock{scenarioConflict: true},
			method:     http.MethodPost,
			path:       "/api/v1/sweatbox/scenario",
			body:       map[string]any{"text": "AAL1 B738"},
			wantStatus: http.StatusConflict,
			errSubstr:  "airport",
		},
		{
			name:       "scenario 200 invalid JSON → 502",
			mock:       sweatboxMock{scenarioBadJSON: true},
			method:     http.MethodPost,
			path:       "/api/v1/sweatbox/scenario",
			body:       map[string]any{"text": "AAL1"},
			wantStatus: http.StatusBadGateway,
			errSubstr:  "invalid JSON",
		},
		{
			name:       "command 409 no airport",
			mock:       sweatboxMock{commandConflict: true},
			method:     http.MethodPost,
			path:       "/api/v1/sweatbox/command",
			body:       map[string]any{"command": "add"},
			wantStatus: http.StatusConflict,
			errSubstr:  "airport",
		},
		{
			name:       "delete-one 404",
			mock:       sweatboxMock{deleteNotFound: true},
			method:     http.MethodDelete,
			path:       "/api/v1/sweatbox/aircraft/NOEXIST",
			body:       nil,
			wantStatus: http.StatusNotFound,
			errSubstr:  "not found",
		},
		{
			name:       "delete-all 404 disabled",
			mock:       sweatboxMock{deleteAllNotFound: true},
			method:     http.MethodDelete,
			path:       "/api/v1/sweatbox/aircraft",
			body:       map[string]any{"confirm": true},
			wantStatus: http.StatusNotFound,
			errSubstr:  "not enabled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.mock
			fsd := httptest.NewServer(m.handler())
			t.Cleanup(fsd.Close)

			env := setupTestAPI(t)
			env.server.cfg.FsdHttpServiceAddress = fsd.URL
			access, _ := env.login(t, env.admin.CID, env.adminPass)

			w := env.doJSON(t, tc.method, tc.path, tc.body, access)
			require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			res := decodeAPIV1(t, w)
			require.NotNil(t, res.Err)
			assert.Contains(t, strings.ToLower(*res.Err), strings.ToLower(tc.errSubstr))
			assert.Nil(t, res.Data)
		})
	}
}
