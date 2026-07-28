package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIValidateAPTUnauth(t *testing.T) {
	env := setupTestAPI(t)
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": "icao=KBTV\n",
	}, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
}

func TestAPIValidateAIRUnauth(t *testing.T) {
	env := setupTestAPI(t)
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-air", map[string]any{
		"text": "",
	}, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestAPIValidateAPTForbiddenForObserver(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.observer.CID, env.observerPass)
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": "icao=KBTV\n",
	}, access)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	// Must be JSON 403 envelope — never an HTML redirect.
	assert.Empty(t, w.Header().Get("Location"))
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Equal(t, "forbidden", *res.Err)
	assert.Equal(t, "v1", res.Version)
}

func TestAPIValidateAIRForbiddenForObserver(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.observer.CID, env.observerPass)
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-air", map[string]any{
		"text": "",
	}, access)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get("Location"))
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Equal(t, "forbidden", *res.Err)
}

func TestAPIValidateAPTAllowedForInstructor(t *testing.T) {
	env := setupTestAPI(t)
	i1Pass := "i1pass123"
	i1 := &db.User{
		Password:      i1Pass,
		FirstName:     strPtr("Inst"),
		LastName:      strPtr("One"),
		NetworkRating: int(protocol.NetworkRatingInstructor1),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(context.Background(), i1))

	access, _ := env.login(t, i1.CID, i1Pass)
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": "icao=KBTV\n",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
}

func TestAPIValidateAPTAdminOKFixture(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	aptText := readTwrfilesFixture(t, "KBTV_example.apt")
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": aptText,
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	assert.Equal(t, "v1", res.Version)

	data := decodeEditorAPTData(t, res.Data)
	assert.Equal(t, "KBTV", data.ICAO)
	assert.Greater(t, data.SurfaceCount, 0)
	require.NotNil(t, data.Errors)
	// Fixture should parse cleanly (errors array present, typically empty).
	assert.IsType(t, []string{}, data.Errors)

	// Must not leak geometry in the raw body.
	raw := w.Body.String()
	assert.NotContains(t, raw, "Surfaces")
	assert.NotContains(t, raw, "points")
	assert.NotContains(t, raw, "44.46893")
}

func TestAPIValidateAIRAdminOKFixture(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	airText := readTwrfilesFixture(t, "KBTV_example.air")
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-air", map[string]any{
		"text": airText,
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)

	data := decodeEditorAIRData(t, res.Data)
	assert.Equal(t, 3, data.AircraftCount)
	require.NotNil(t, data.Errors)
	assert.Empty(t, data.Errors)

	// Must not return full aircraft rows.
	raw := w.Body.String()
	assert.NotContains(t, raw, "AAL123")
	assert.NotContains(t, raw, "callsign")
}

func TestAPIValidateAPTWithSoftErrors(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	// Invalid ICAO length → soft error, still HTTP 200.
	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": "icao=XX\n",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data := decodeEditorAPTData(t, res.Data)
	assert.Equal(t, "XX", data.ICAO)
	assert.Equal(t, 0, data.SurfaceCount)
	require.NotEmpty(t, data.Errors)
	assert.Contains(t, data.Errors[0], "ICAO")
}

func TestAPIValidateAIRWithSoftErrors(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-air", map[string]any{
		"text": "ONLY:THREE:FIELDS\n",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data := decodeEditorAIRData(t, res.Data)
	assert.Equal(t, 0, data.AircraftCount)
	require.NotEmpty(t, data.Errors)
	assert.Contains(t, data.Errors[0], "fields")
}

func TestAPIValidateAPTEmptyText(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-apt", map[string]any{
		"text": "",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data := decodeEditorAPTData(t, res.Data)
	assert.Equal(t, "", data.ICAO)
	assert.Equal(t, 0, data.SurfaceCount)
	require.NotNil(t, data.Errors)
}

func TestAPIValidateAIREmptyText(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/editor/validate-air", map[string]any{
		"text": "",
	}, access)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data := decodeEditorAIRData(t, res.Data)
	assert.Equal(t, 0, data.AircraftCount)
	require.NotNil(t, data.Errors)
	assert.Empty(t, data.Errors)
}

func TestAPIValidateAPTInvalidJSON(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-apt", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "invalid JSON")
}

func TestAPIValidateAPTOversizedBody(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	// text alone exceeds 2 MiB → raw JSON body also exceeds MaxBytesReader.
	big := strings.Repeat("x", sweatboxWebMaxBody+1)
	payload, err := json.Marshal(map[string]any{"text": big})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-apt", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "2 MiB")
}

func TestAPIValidateAIROversizedBody(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	big := strings.Repeat("y", sweatboxWebMaxBody+1)
	payload, err := json.Marshal(map[string]any{"text": big})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-air", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
}

func TestAPIValidateAPTCookieSessionCSRF(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")

	// Ensure CSRF cookie is issued (dashboard sets it).
	_, cookies = authedGET(t, ts, "/dashboard", cookies)
	csrf := csrfFromCookies(cookies)
	require.NotEmpty(t, csrf)

	payload := `{"text":"icao=KBTV\n"}`

	// Cookie session without CSRF → 403 JSON.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-apt", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get("Location"))
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "CSRF")

	// Cookie session with CSRF header → 200.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-apt", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrf)
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res = decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data := decodeEditorAPTData(t, res.Data)
	assert.Equal(t, "KBTV", data.ICAO)
}

func TestAPIValidateAIRCookieSessionCSRF(t *testing.T) {
	ts := newTestServer(t)
	admin := createTestUser(t, ts, "admin-pass", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, admin.CID, "admin-pass")
	_, cookies = authedGET(t, ts, "/dashboard", cookies)
	csrf := csrfFromCookies(cookies)
	require.NotEmpty(t, csrf)

	payload := `{"text":""}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-air", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	req = httptest.NewRequest(http.MethodPost, "/api/v1/editor/validate-air", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrf)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// --- helpers ---

func readTwrfilesFixture(t *testing.T, name string) string {
	t.Helper()
	// Tests run with package dir as CWD; fixtures live in pkg/twrfiles/testdata.
	path := filepath.Join("..", "..", "pkg", "twrfiles", "testdata", name)
	b, err := os.ReadFile(path)
	require.NoError(t, err, "fixture %s", path)
	return string(b)
}

func decodeEditorAPTData(t *testing.T, data any) editorValidateAPTData {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	var out editorValidateAPTData
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return out
}

func decodeEditorAIRData(t *testing.T, data any) editorValidateAIRData {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	var out editorValidateAIRData
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return out
}
