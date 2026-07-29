package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIVersionRegistryInvariants(t *testing.T) {
	require.NotEmpty(t, knownMicroversions, "knownMicroversions must not be empty")
	assert.Equal(t, knownMicroversions[0], apiMicroMin, "apiMicroMin must equal first known pin")
	assert.Equal(t, knownMicroversions[len(knownMicroversions)-1], apiMicroMax, "apiMicroMax must equal last known pin")

	for i, pin := range knownMicroversions {
		// Valid calendar YYYY-MM-DD after normalize (identity).
		canonical, err := normalizeAPIVersion(pin, apiMicroMax)
		require.NoError(t, err, "known pin %q must normalize", pin)
		assert.Equal(t, pin, canonical)
		assert.True(t, reAPIVersionDate.MatchString(pin), "pin %q must be YYYY-MM-DD", pin)

		if i > 0 {
			assert.True(t, knownMicroversions[i-1] < pin,
				"knownMicroversions must be ascending ISO: %q then %q", knownMicroversions[i-1], pin)
		}
	}

	// Constants must themselves be known pins.
	assert.True(t, isKnownMicroversion(apiMicroMin))
	assert.True(t, isKnownMicroversion(apiMicroMax))
	assert.Equal(t, "v1", apiMajorVersion)
}

func TestNormalizeAPIVersion(t *testing.T) {
	const max = "2026-07-28"

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "dashed date", raw: "2026-07-28", want: "2026-07-28"},
		{name: "latest lower", raw: "latest", want: max},
		{name: "latest upper", raw: "LATEST", want: max},
		{name: "latest mixed", raw: "Latest", want: max},
		{name: "latest padded", raw: "  latest  ", want: max},
		{name: "alias 1.YYYYMMDD", raw: "1.20260728", want: "2026-07-28"},
		{name: "alias with spaces", raw: " 1.20260728 ", want: "2026-07-28"},
		{name: "date padded", raw: " 2026-07-28 ", want: "2026-07-28"},

		// Invalid forms
		{name: "empty", raw: "", wantErr: errAPIVersionInvalid},
		{name: "whitespace only", raw: "   ", wantErr: errAPIVersionInvalid},
		{name: "mixed alias with dashes", raw: "1.2026-07-28", wantErr: errAPIVersionInvalid},
		{name: "v1 prefix", raw: "v1.2026-07-28", wantErr: errAPIVersionInvalid},
		{name: "slash date", raw: "2026/07/28", wantErr: errAPIVersionInvalid},
		{name: "garbage", raw: "not-a-version", wantErr: errAPIVersionInvalid},
		{name: "partial date", raw: "2026-07", wantErr: errAPIVersionInvalid},
		{name: "impossible date Feb 30", raw: "2026-02-30", wantErr: errAPIVersionInvalid},
		{name: "impossible date Apr 31", raw: "2026-04-31", wantErr: errAPIVersionInvalid},
		{name: "zero month", raw: "2026-00-15", wantErr: errAPIVersionInvalid},
		{name: "alias bad day", raw: "1.20260230", wantErr: errAPIVersionInvalid},
		{name: "alias short", raw: "1.2026072", wantErr: errAPIVersionInvalid},
		{name: "alias long", raw: "1.202607281", wantErr: errAPIVersionInvalid},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAPIVersion(tc.raw, max)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShapeFor(t *testing.T) {
	type domain struct{ N int }
	type dto struct{ Label string }

	adapters := []shapeAdapter[domain, dto]{
		{introducedAt: "2026-07-28", shape: func(d domain) dto { return dto{Label: "baseline"} }},
		{introducedAt: "2026-09-01", shape: func(d domain) dto { return dto{Label: "sept"} }},
		{introducedAt: "2026-11-01", shape: func(d domain) dto { return dto{Label: "nov"} }},
	}

	// Walk every still-supported pin (only baseline today) + future pins for matrix.
	assert.Equal(t, "baseline", shapeFor("2026-07-28", adapters, domain{}).Label)
	assert.Equal(t, "sept", shapeFor("2026-09-01", adapters, domain{}).Label)
	assert.Equal(t, "sept", shapeFor("2026-10-15", adapters, domain{}).Label)
	assert.Equal(t, "nov", shapeFor("2026-11-01", adapters, domain{}).Label)
	assert.Equal(t, "nov", shapeFor("2027-01-01", adapters, domain{}).Label)
	// Before first adapter → first adapter (defensive).
	assert.Equal(t, "baseline", shapeFor("2020-01-01", adapters, domain{}).Label)

	for _, pin := range knownMicroversions {
		got := shapeFor(pin, adapters, domain{})
		assert.NotEmpty(t, got.Label, "pin %s must resolve", pin)
	}

	// Empty adapters → zero value.
	var empty []shapeAdapter[domain, dto]
	assert.Equal(t, dto{}, shapeFor("2026-07-28", empty, domain{}))
}

func TestAPIVersionMiddleware_OmitDefaultsMax(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIVersion))
	assert.Equal(t, apiMicroMin, w.Header().Get(headerAPIMinVersion))
	assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIMaxVersion))
	assert.Equal(t, "true", w.Header().Get(headerAPIVersionDefaulted))
	assert.Contains(t, w.Header().Get("Vary"), headerAPIVersion)
}

func TestAPIVersionMiddleware_PinnedKnown(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set(headerAPIVersion, "2026-07-28")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "2026-07-28", w.Header().Get(headerAPIVersion))
	assert.Empty(t, w.Header().Get(headerAPIVersionDefaulted))
}

func TestAPIVersionMiddleware_LatestAlias(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set(headerAPIVersion, "LATEST")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIVersion))
	assert.Empty(t, w.Header().Get(headerAPIVersionDefaulted))
}

func TestAPIVersionMiddleware_AliasForm(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set(headerAPIVersion, "1.20260728")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "2026-07-28", w.Header().Get(headerAPIVersion))
}

func TestAPIVersionMiddleware_UnknownPin400(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set(headerAPIVersion, "2020-01-01")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "unsupported OpenFSD-API-Version")
	assert.Contains(t, *res.Err, "2020-01-01")
	assert.Equal(t, apiMicroMin, w.Header().Get(headerAPIMinVersion))
	assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIMaxVersion))
}

func TestAPIVersionMiddleware_InvalidPin400(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	for _, pin := range []string{"1.2026-07-28", "not-a-date", "2026-02-30"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set(headerAPIVersion, pin)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, "pin=%s body=%s", pin, w.Body.String())
		res := decodeAPIV1(t, w)
		require.NotNil(t, res.Err)
		assert.Equal(t, "invalid OpenFSD-API-Version", *res.Err)
	}
}

func TestAPIDiscovery_VersionAgnostic(t *testing.T) {
	env := setupTestAPI(t)

	// Bad pin would 400 on resource groups; discovery must ignore it.
	for _, path := range []string{"/api/v1", "/api/v1/versions"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(headerAPIVersion, "2020-01-01")
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "path=%s body=%s", path, w.Body.String())
		res := decodeAPIV1(t, w)
		require.Nil(t, res.Err)
		assert.Equal(t, "v1", res.Version)

		assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIVersion))
		assert.Equal(t, apiMicroMin, w.Header().Get(headerAPIMinVersion))
		assert.Equal(t, apiMicroMax, w.Header().Get(headerAPIMaxVersion))
	}
}

// TestPublicRoutes_NoVersionReject covers KD-3: fsd-jwt (canonical + short
// aliases), auth login/refresh never hard-reject on OpenFSD-API-Version
// (unlike dual-accept resource groups).
func TestPublicRoutes_NoVersionReject(t *testing.T) {
	env := setupTestAPI(t)
	badPins := []string{"2020-01-01", "not-a-version", "1.2026-07-28", "2026-02-30"}

	// All FSD JWT paths (canonical + Client Setup short aliases) stay outside
	// microversion reject — PreferShortJWTPath clients must not get envelope 400.
	for _, path := range []string{"/j", "/api/fsd-jwt", "/api/v1/fsd-jwt"} {
		t.Run("fsd-jwt_"+path, func(t *testing.T) {
			for _, pin := range badPins {
				body := fmt.Sprintf(`{"cid":"%d","password":%q}`, env.admin.CID, env.adminPass)
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(headerAPIVersion, pin)
				w := httptest.NewRecorder()
				env.router.ServeHTTP(w, req)
				// Success path — never version 400.
				require.Equal(t, http.StatusOK, w.Code, "path=%s pin=%s body=%s", path, pin, w.Body.String())
				assert.NotContains(t, w.Body.String(), "OpenFSD-API-Version")
			}
		})
	}

	t.Run("auth_login", func(t *testing.T) {
		for _, pin := range badPins {
			body := fmt.Sprintf(`{"cid":%d,"password":%q}`, env.admin.CID, env.adminPass)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(headerAPIVersion, pin)
			w := httptest.NewRecorder()
			env.router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "pin=%s body=%s", pin, w.Body.String())
			res := decodeAPIV1(t, w)
			require.Nil(t, res.Err, "pin must not produce version err; got %v", res.Err)
		}
	})

	t.Run("auth_refresh", func(t *testing.T) {
		_, refresh := env.login(t, env.observer.CID, env.observerPass)
		for _, pin := range badPins {
			body, err := json.Marshal(map[string]string{"refresh_token": refresh})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(headerAPIVersion, pin)
			w := httptest.NewRecorder()
			env.router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "pin=%s body=%s", pin, w.Body.String())
			res := decodeAPIV1(t, w)
			require.Nil(t, res.Err)
		}
	})
}

func TestAppendVary(t *testing.T) {
	h := http.Header{}
	appendVary(h, headerAPIVersion)
	assert.Equal(t, headerAPIVersion, h.Get("Vary"))
	// Idempotent.
	appendVary(h, headerAPIVersion)
	assert.Equal(t, headerAPIVersion, h.Get("Vary"))
	// Merge with existing.
	h.Set("Vary", "Accept-Encoding")
	appendVary(h, headerAPIVersion)
	assert.Equal(t, "Accept-Encoding, "+headerAPIVersion, h.Get("Vary"))
}

func TestAPIDiscovery_PayloadGolden(t *testing.T) {
	env := setupTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/versions", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertGoldenJSON(t, "2026-07-28/discovery.json", w.Body.Bytes())
}

func TestOpenAPIEndpoints(t *testing.T) {
	env := setupTestAPI(t)

	// YAML
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	req.Header.Set(headerAPIVersion, "bogus") // must not 400
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "yaml")
	assert.Contains(t, w.Body.String(), "openapi:")
	assert.Contains(t, w.Body.String(), "openfsd")
	// Guard against merge regressions that duplicate top-level components: keys
	// (YAML last-key-wins would drop earlier schemas such as Account*).
	componentsYAMLLines := 0
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if line == "components:" {
			componentsYAMLLines++
		}
	}
	assert.Equal(t, 1, componentsYAMLLines, "OpenAPI YAML must contain exactly one top-level components: map")

	// JSON
	req = httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	req.Header.Set(headerAPIVersion, "2020-01-01")
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "json")
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Contains(t, doc, "openapi")
	assert.Contains(t, doc, "paths")

	paths, ok := doc["paths"].(map[string]any)
	require.True(t, ok, "paths must be an object")
	for _, p := range []string{
		"/users",
		"/users/{cid}",
		"/account/password",
		"/account/delete",
		"/sweatbox/session",
		"/sweatbox/airport",
		"/sweatbox/pause",
		"/config/createtoken",
		"/versions",
	} {
		assert.Contains(t, paths, p, "first-train path missing from OpenAPI")
	}

	components, ok := doc["components"].(map[string]any)
	require.True(t, ok, "components must be an object")
	schemas, ok := components["schemas"].(map[string]any)
	require.True(t, ok, "components.schemas must be an object")
	for _, name := range []string{
		"UserRecord",
		"UserListData",
		"AccountPasswordRequest",
		"AccountDeleteRequest",
		"AccountDeleteData",
		"APIV1EnvelopeUserList",
		"SweatboxSessionData",
		"SweatboxCommandData",
	} {
		assert.Contains(t, schemas, name, "expected schema missing after YAML→JSON")
	}

	// Account ops stay Provisional until maintainer sign-off.
	assertOpenAPIStability(t, paths, "/account/password", "post", "provisional")
	assertOpenAPIStability(t, paths, "/account/delete", "post", "provisional")
	assertOpenAPIStability(t, paths, "/users", "get", "stable")
	assertOpenAPIStability(t, paths, "/sweatbox/session", "get", "stable")
}

// assertOpenAPIStability checks x-openfsd-stability on a path operation.
func assertOpenAPIStability(t *testing.T, paths map[string]any, path, method, want string) {
	t.Helper()
	item, ok := paths[path].(map[string]any)
	require.True(t, ok, "path %s", path)
	op, ok := item[method].(map[string]any)
	require.True(t, ok, "path %s method %s", path, method)
	got, _ := op["x-openfsd-stability"].(string)
	assert.Equal(t, want, got, "x-openfsd-stability on %s %s", method, path)
}

func TestDataFeed_NoVersionReject(t *testing.T) {
	env := setupTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/status.json", nil)
	req.Header.Set(headerAPIVersion, "2020-01-01")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestCreateToken_RecommendedAPIVersionFields(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/config/createtoken", map[string]any{
		"expiry_date_time": time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	}, adminAccess)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)

	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var body struct {
		Token                 string `json:"token"`
		RecommendedAPIVersion string `json:"recommended_api_version"`
		APIVersionMin         string `json:"api_version_min"`
		APIVersionMax         string `json:"api_version_max"`
	}
	require.NoError(t, json.Unmarshal(data, &body))
	require.NotEmpty(t, body.Token)
	assert.Equal(t, apiMicroMax, body.RecommendedAPIVersion)
	assert.Equal(t, apiMicroMin, body.APIVersionMin)
	assert.Equal(t, apiMicroMax, body.APIVersionMax)

	// Golden with redacted token.
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	dataMap, ok := envelope["data"].(map[string]any)
	require.True(t, ok)
	dataMap["token"] = "<redacted>"
	rewritten, err := json.Marshal(envelope)
	require.NoError(t, err)
	assertGoldenJSON(t, "2026-07-28/createtoken_fields.json", rewritten)
}

func TestAPIV1Goldens_EnvelopedRoutes(t *testing.T) {
	env := setupTestAPI(t)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)

	t.Run("error_unauthorized", func(t *testing.T) {
		w := env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.observer.CID}, "")
		require.Equal(t, http.StatusUnauthorized, w.Code)
		assertGoldenJSON(t, "2026-07-28/error_unauthorized.json", w.Body.Bytes())
	})

	t.Run("error_forbidden", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/config/load", nil, obsAccess)
		require.Equal(t, http.StatusForbidden, w.Code)
		assertGoldenJSON(t, "2026-07-28/error_forbidden.json", w.Body.Bytes())
	})

	t.Run("error_invalid_json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/load", bytes.NewReader([]byte(`{not-json`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+obsAccess)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assertGoldenJSON(t, "2026-07-28/error_invalid_json.json", w.Body.Bytes())
	})

	t.Run("error_invalid_version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
		req.Header.Set("Authorization", "Bearer "+adminAccess)
		req.Header.Set(headerAPIVersion, "1.2026-07-28")
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assertGoldenJSON(t, "2026-07-28/error_invalid_version.json", w.Body.Bytes())
	})

	t.Run("error_unsupported_version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config/load", nil)
		req.Header.Set("Authorization", "Bearer "+adminAccess)
		req.Header.Set(headerAPIVersion, "2020-01-01")
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assertGoldenJSON(t, "2026-07-28/error_unsupported_version.json", w.Body.Bytes())
	})

	t.Run("config_update_ok", func(t *testing.T) {
		w := env.doJSON(t, http.MethodPost, "/api/v1/config/update", map[string]any{
			"key_value_pairs": []map[string]string{
				{"key": "WELCOME_MESSAGE", "value": "hello golden"},
			},
		}, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assertGoldenJSON(t, "2026-07-28/config_update_ok.json", w.Body.Bytes())
	})

	t.Run("user_load_self", func(t *testing.T) {
		w := env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{
			"cid": env.observer.CID,
		}, obsAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		// Rewrite dynamic CID to 0 for golden compare.
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		dataMap, ok := envelope["data"].(map[string]any)
		require.True(t, ok)
		dataMap["cid"] = float64(0) // JSON numbers → float64
		rewritten, err := json.Marshal(envelope)
		require.NoError(t, err)
		assertGoldenJSON(t, "2026-07-28/user_load_self.json", rewritten)
	})
}

// assertGoldenJSON compares got JSON to a fixture under testdata/api_v1/.
// Comparison is semantic (via json.Unmarshal + remarshal) so key order is ignored.
func assertGoldenJSON(t *testing.T, rel string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "api_v1", filepath.FromSlash(rel))
	wantBytes, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s", path)

	var wantAny, gotAny any
	require.NoError(t, json.Unmarshal(wantBytes, &wantAny), "parse golden %s", path)
	require.NoError(t, json.Unmarshal(got, &gotAny), "parse response: %s", string(got))

	wantNorm, err := json.Marshal(wantAny)
	require.NoError(t, err)
	gotNorm, err := json.Marshal(gotAny)
	require.NoError(t, err)

	if !bytes.Equal(wantNorm, gotNorm) {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s\n",
			rel, prettyJSON(wantNorm), prettyJSON(gotNorm))
	}
}

func prettyJSON(b []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return string(b)
	}
	return strings.TrimSpace(buf.String())
}
