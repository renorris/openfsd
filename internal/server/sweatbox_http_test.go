package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/require"
)

func TestSweatboxHTTP_RoutesAbsentWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kv := &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	srv, err := New(Deps{
		Config:          &Config{FsdListenAddrs: []string{":0"}, ServiceHTTPListenAddr: "127.0.0.1:0"},
		Users:           stubUserStore{},
		ConfigKV:        kv,
		Registry:        postoffice.New(),
		Metar:           &recordingMetar{},
		Clock:           realClock{},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		SweatboxEnabled: false,
	})
	require.NoError(t, err)
	require.Nil(t, srv.sweatbox)

	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/sweatbox/state"},
		{http.MethodGet, "/sweatbox/ops"},
		{http.MethodPost, "/sweatbox/airport"},
		{http.MethodPost, "/sweatbox/scenario"},
		{http.MethodPost, "/sweatbox/command"},
		{http.MethodPost, "/sweatbox/pause"},
		{http.MethodPost, "/sweatbox/unpause"},
		{http.MethodDelete, "/sweatbox/aircraft/AAL1"},
	}
	for _, p := range paths {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		e.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "%s %s", p.method, p.path)
	}
}

func TestSweatboxHTTP_AuthRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv, _, _ := newSweatboxEnv(t)
	srv.configKV = &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	e := srv.setupRoutes()

	// Missing bearer
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sweatbox/state", nil)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Observer forbidden
	tokObs := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingObserver)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sweatbox/state", nil)
	req.Header.Set("Authorization", "Bearer "+tokObs)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestSweatboxHTTP_Lifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv, h, _ := newSweatboxEnv(t)
	// newSweatboxEnv uses stubConfigStore without JWT secret — inject mapConfig.
	srv.configKV = &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)

	apt := readTestdata(t, "KBTV_example.apt")
	air := readTestdata(t, "KBTV_example.air")

	// Empty state
	{
		w := doService(t, e, tok, http.MethodGet, "/sweatbox/state", nil, "")
		require.Equal(t, http.StatusOK, w.Code)
		var st SweatboxStateJSON
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
		require.Empty(t, st.ICAO)
		require.True(t, st.Paused)
		require.NotNil(t, st.Aircraft)
		require.Empty(t, st.Aircraft)
	}

	// Scenario without airport → 409
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/scenario", air, "text/plain")
		require.Equal(t, http.StatusConflict, w.Code)
	}

	// Command add without airport → 409
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/command",
			[]byte(`{"command":"add I L J -90 5 3000 B738"}`), "application/json")
		require.Equal(t, http.StatusConflict, w.Code)
		var resp SweatboxCommandResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.False(t, resp.OK)
		require.Contains(t, resp.Message, "No airport")
	}

	// Load airport
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport", apt, "text/plain")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var res AirportLoadResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		require.Equal(t, "KBTV", res.ICAO)
		require.Greater(t, res.Surfaces, 0)
	}

	// Ops empty
	{
		w := doService(t, e, tok, http.MethodGet, "/sweatbox/ops", nil, "")
		require.Equal(t, http.StatusOK, w.Code)
		var ops SweatboxOpsJSON
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ops))
		require.Equal(t, 0, ops.ArrCount)
		require.Contains(t, ops.Message, "Elapsed:")
	}

	// Stats alias
	{
		w := doService(t, e, tok, http.MethodGet, "/sweatbox/stats", nil, "")
		require.Equal(t, http.StatusOK, w.Code)
	}

	// Load scenario
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/scenario", air, "text/plain")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var res SweatboxScenarioResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		require.Greater(t, res.Loaded, 0)
		require.True(t, h.Engine().Paused())
	}

	// State has aircraft
	var callsign string
	{
		w := doService(t, e, tok, http.MethodGet, "/sweatbox/state", nil, "")
		require.Equal(t, http.StatusOK, w.Code)
		var st SweatboxStateJSON
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
		require.Equal(t, "KBTV", st.ICAO)
		require.NotEmpty(t, st.Aircraft)
		callsign = st.Aircraft[0].Callsign
	}

	// Soft command error → 200 ok=false
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/command",
			[]byte(`{"command":"notacommand"}`), "application/json")
		require.Equal(t, http.StatusOK, w.Code)
		var resp SweatboxCommandResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.False(t, resp.OK)
	}

	// Malformed JSON → 400
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/command",
			[]byte(`{`), "application/json")
		require.Equal(t, http.StatusBadRequest, w.Code)
	}

	// Empty command → 400
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/command",
			[]byte(`{"command":"  "}`), "application/json")
		require.Equal(t, http.StatusBadRequest, w.Code)
	}

	// Pause / unpause
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/unpause", nil, "")
		require.Equal(t, http.StatusNoContent, w.Code)
		require.False(t, h.Engine().Paused())

		w = doService(t, e, tok, http.MethodPost, "/sweatbox/pause", nil, "")
		require.Equal(t, http.StatusNoContent, w.Code)
		require.True(t, h.Engine().Paused())
	}

	// Airport reload blocked while aircraft present
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport", apt, "text/plain")
		require.Equal(t, http.StatusConflict, w.Code)
	}

	// replace=1 clears aircraft and reloads
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport?replace=1", apt, "text/plain")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		st := h.State()
		require.Empty(t, st.Aircraft)
	}

	// Re-load scenario and delete one aircraft
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/scenario", air, "text/plain")
		require.Equal(t, http.StatusOK, w.Code)
		var res SweatboxScenarioResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		require.Greater(t, res.Loaded, 0)

		st := h.State()
		require.NotEmpty(t, st.Aircraft)
		callsign = st.Aircraft[0].Callsign

		w = doService(t, e, tok, http.MethodDelete, "/sweatbox/aircraft/"+callsign, nil, "")
		require.Equal(t, http.StatusNoContent, w.Code)
		require.Nil(t, h.SessionFor(callsign))

		// Missing → 404
		w = doService(t, e, tok, http.MethodDelete, "/sweatbox/aircraft/"+callsign, nil, "")
		require.Equal(t, http.StatusNotFound, w.Code)
	}

	// POST /del body
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/command",
			[]byte(`{"command":"add I L J -90 5 3000 B738"}`), "application/json")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var cmd SweatboxCommandResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cmd))
		require.True(t, cmd.OK)

		st := h.State()
		require.NotEmpty(t, st.Aircraft)
		cs := st.Aircraft[0].Callsign

		w = doService(t, e, tok, http.MethodPost, "/sweatbox/del",
			[]byte(`{"callsign":"`+cs+`"}`), "application/json")
		require.Equal(t, http.StatusNoContent, w.Code)
	}

	// DELETE all
	{
		_, _ = h.LoadScenario(air)
		require.NotEmpty(t, h.State().Aircraft)
		w := doService(t, e, tok, http.MethodDelete, "/sweatbox/aircraft", nil, "")
		require.Equal(t, http.StatusNoContent, w.Code)
		require.Empty(t, h.State().Aircraft)
	}

	// Invalid airport body → 400
	{
		w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport",
			[]byte("not an airport"), "text/plain")
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestSweatboxHTTP_BodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv, _, _ := newSweatboxEnv(t)
	srv.configKV = &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)

	// Just over 2 MiB
	big := bytes.Repeat([]byte("x"), sweatboxMaxBodyBytes+1)
	w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport", big, "text/plain")
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	// Command body over limit
	w = doService(t, e, tok, http.MethodPost, "/sweatbox/command",
		[]byte(`{"command":"`+strings.Repeat("a", sweatboxMaxBodyBytes)+`"}`),
		"application/json")
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestSweatboxHTTP_CommandWithSelectedCallsign(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv, h, _ := newSweatboxEnv(t)
	srv.configKV = &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)

	apt := readTestdata(t, "KBTV_example.apt")
	w := doService(t, e, tok, http.MethodPost, "/sweatbox/airport", apt, "text/plain")
	require.Equal(t, http.StatusOK, w.Code)

	// Add aircraft via command
	w = doService(t, e, tok, http.MethodPost, "/sweatbox/command",
		[]byte(`{"command":"add I L J -90 5 3000 B738"}`), "application/json")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	st := h.State()
	require.NotEmpty(t, st.Aircraft)
	cs := st.Aircraft[0].Callsign

	// Selected callsign form: verb-first with callsign field
	body := []byte(`{"callsign":"` + cs + `","command":"sq 1234"}`)
	w = doService(t, e, tok, http.MethodPost, "/sweatbox/command", body, "application/json")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SweatboxCommandResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.OK, resp.Message)

	snap, ok := h.Engine().Get(cs)
	require.True(t, ok)
	require.Equal(t, "1234", snap.Squawk)
}

func TestSweatboxHTTP_MultipartAirport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv, _, _ := newSweatboxEnv(t)
	srv.configKV = &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)

	apt := readTestdata(t, "KBTV_example.apt")
	var buf bytes.Buffer
	// Manual multipart (avoid mime/multipart import complexity for tiny body).
	boundary := "----openfsdtest"
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString(`Content-Disposition: form-data; name="airport"` + "\r\n\r\n")
	buf.Write(apt)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sweatbox/airport", &buf)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res AirportLoadResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Equal(t, "KBTV", res.ICAO)
}

func doService(t *testing.T, e *gin.Engine, tok, method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer "+tok)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	e.ServeHTTP(w, req)
	return w
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "sweatbox", "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		b, err = os.ReadFile(filepath.Join("internal", "sweatbox", "testdata", name))
	}
	require.NoError(t, err)
	return b
}
