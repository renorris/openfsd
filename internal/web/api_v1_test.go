package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type testAPIEnv struct {
	server   *Server
	router   *gin.Engine
	admin    *db.User
	observer *db.User
	// plaintext passwords for login tests
	adminPass    string
	observerPass string
}

func setupTestAPI(t *testing.T) *testAPIEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// Sanitize t.Name() so subtests (names with "/") stay valid SQLite memory DSNs.
	dsnName := strings.ReplaceAll(t.Name(), "/", "_")
	sqlDB, err := sql.Open("sqlite", "file:"+dsnName+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.Migrate(sqlDB))

	repos, err := db.NewRepositories(sqlDB)
	require.NoError(t, err)
	require.NoError(t, db.InitDefaultConfig(repos.ConfigRepo))

	adminPass := "adminpass1"
	observerPass := "observerpass1"

	admin := &db.User{
		Password:      adminPass,
		FirstName:     strPtr("Admin"),
		LastName:      strPtr("User"),
		NetworkRating: int(protocol.NetworkRatingAdministator),
	}
	require.NoError(t, repos.UserRepo.CreateUser(admin))

	observer := &db.User{
		Password:      observerPass,
		FirstName:     strPtr("Obs"),
		LastName:      strPtr("Server"),
		NetworkRating: int(protocol.NetworkRatingObserver),
	}
	require.NoError(t, repos.UserRepo.CreateUser(observer))

	// Repos are injected into NewServer; DSN fields on cfg are unused in these tests.
	cfg := &ServerConfig{
		ListenAddr:            ":0",
		FsdHttpServiceAddress: "http://127.0.0.1:1", // unused for most API tests
	}
	srv, err := NewServer(cfg, repos)
	require.NoError(t, err)

	router, err := srv.setupRoutes()
	require.NoError(t, err)

	return &testAPIEnv{
		server:       srv,
		router:       router,
		admin:        admin,
		observer:     observer,
		adminPass:    adminPass,
		observerPass: observerPass,
	}
}

func strPtr(s string) *string { return &s }

func (e *testAPIEnv) doJSON(t *testing.T, method, path string, body any, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
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
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *testAPIEnv) login(t *testing.T, cid int, password string) (access, refresh string) {
	t.Helper()
	w := e.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"cid":      cid,
		"password": password,
	}, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Nil(t, res.Err)
	require.Equal(t, "v1", res.Version)

	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(data, &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	return tokens.AccessToken, tokens.RefreshToken
}

func decodeAPIV1(t *testing.T, w *httptest.ResponseRecorder) APIV1Response {
	t.Helper()
	var res APIV1Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res), w.Body.String())
	return res
}

func TestAuthLoginSuccessAndFailure(t *testing.T) {
	env := setupTestAPI(t)

	// success
	access, refresh := env.login(t, env.admin.CID, env.adminPass)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)

	// bad password
	w := env.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"cid":      env.admin.CID,
		"password": "wrong-password",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "CID")

	// empty object / missing required fields
	w = env.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]any{}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthRefresh(t *testing.T) {
	env := setupTestAPI(t)
	_, refresh := env.login(t, env.observer.CID, env.observerPass)

	w := env.doJSON(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": refresh,
	}, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)

	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var body struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(data, &body))
	assert.NotEmpty(t, body.AccessToken)

	// access token is not a refresh token
	w = env.doJSON(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": body.AccessToken,
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBearerMiddleware(t *testing.T) {
	env := setupTestAPI(t)
	access, refresh := env.login(t, env.admin.CID, env.adminPass)

	// Dual-accept (KD-18): missing/invalid Bearer without session is unauthorized.

	// missing Authorization
	w := env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "unauthorized")

	// garbage token
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, "not-a-jwt")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	res = decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "unauthorized")

	// refresh token is not an access token
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, refresh)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	res = decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "unauthorized")

	// valid access token
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, access)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestFsdJwt(t *testing.T) {
	env := setupTestAPI(t)

	// good credentials
	w := env.doJSON(t, http.MethodPost, "/api/v1/fsd-jwt", map[string]any{
		"cid":      fmt.Sprintf("%d", env.admin.CID),
		"password": env.adminPass,
	}, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var okBody struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &okBody))
	assert.True(t, okBody.Success)
	assert.NotEmpty(t, okBody.Token)

	// bad password
	w = env.doJSON(t, http.MethodPost, "/api/v1/fsd-jwt", map[string]any{
		"cid":      fmt.Sprintf("%d", env.admin.CID),
		"password": "wrong-password",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	var errBody struct {
		Success  bool   `json:"success"`
		ErrorMsg string `json:"error_msg"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errBody))
	assert.False(t, errBody.Success)
	assert.Contains(t, errBody.ErrorMsg, "CID")

	// suspended rating
	suspendedPass := "suspended1"
	suspended := &db.User{
		Password:      suspendedPass,
		FirstName:     strPtr("Suspended"),
		NetworkRating: int(protocol.NetworkRatingSuspended),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(suspended))

	w = env.doJSON(t, http.MethodPost, "/api/v1/fsd-jwt", map[string]any{
		"cid":      fmt.Sprintf("%d", suspended.CID),
		"password": suspendedPass,
	}, "")
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errBody))
	assert.Contains(t, errBody.ErrorMsg, "suspended")

	// bind failure → 400
	w = env.doJSON(t, http.MethodPost, "/api/v1/fsd-jwt", map[string]any{}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestUserLoadPermissions(t *testing.T) {
	env := setupTestAPI(t)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)

	// observer can load self
	w := env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.observer.CID}, obsAccess)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data, _ := json.Marshal(res.Data)
	assert.Contains(t, string(data), fmt.Sprintf(`"cid":%d`, env.observer.CID))

	// observer cannot load another user
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin (rating >= SUP) can load another user
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.observer.CID}, adminAccess)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUserCreateAndUpdate(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	// observer cannot create
	w := env.doJSON(t, http.MethodPost, "/api/v1/user/create", map[string]any{
		"password":       "newuserpass",
		"first_name":     "New",
		"last_name":      "Pilot",
		"network_rating": int(protocol.NetworkRatingObserver),
	}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin creates user
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/create", map[string]any{
		"password":       "newuserpass",
		"first_name":     "New",
		"last_name":      "Pilot",
		"network_rating": int(protocol.NetworkRatingObserver),
	}, adminAccess)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var created struct {
		CID int `json:"cid"`
	}
	require.NoError(t, json.Unmarshal(data, &created))
	require.Greater(t, created.CID, 0)

	// admin updates first name (include network_rating so binding validators accept the body)
	newName := "Renamed"
	w = env.doJSON(t, http.MethodPatch, "/api/v1/user/update", map[string]any{
		"cid":            created.CID,
		"first_name":     newName,
		"network_rating": int(protocol.NetworkRatingObserver),
	}, adminAccess)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res = decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data, _ = json.Marshal(res.Data)
	assert.Contains(t, string(data), newName)

	// observer cannot update
	w = env.doJSON(t, http.MethodPatch, "/api/v1/user/update", map[string]any{
		"cid":            created.CID,
		"first_name":     "Nope",
		"network_rating": int(protocol.NetworkRatingObserver),
	}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestConfigLoadAndUpdate(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	// observer forbidden
	w := env.doJSON(t, http.MethodGet, "/api/v1/config/load", nil, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin load
	w = env.doJSON(t, http.MethodGet, "/api/v1/config/load", nil, adminAccess)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	assert.Contains(t, string(data), db.ConfigWelcomeMessage)

	// admin update welcome message
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/update", map[string]any{
		"key_value_pairs": []map[string]string{
			{"key": db.ConfigWelcomeMessage, "value": "Hello from test"},
		},
	}, adminAccess)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// verify persisted
	val, err := env.server.dbRepo.ConfigRepo.Get(db.ConfigWelcomeMessage)
	require.NoError(t, err)
	assert.Equal(t, "Hello from test", val)

	// observer cannot update
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/update", map[string]any{
		"key_value_pairs": []map[string]string{
			{"key": db.ConfigWelcomeMessage, "value": "nope"},
		},
	}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestConfigResetSecretKey(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	// observer forbidden
	w := env.doJSON(t, http.MethodPost, "/api/v1/config/resetsecretkey", nil, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin rotates secret
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/resetsecretkey", nil, adminAccess)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// old access token no longer validates
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, adminAccess)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// re-login works with new secret
	newAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, newAccess)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestConfigCreateToken(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	// observer forbidden
	w := env.doJSON(t, http.MethodPost, "/api/v1/config/createtoken", map[string]any{
		"expiry_date_time": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// past expiry → 400
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/createtoken", map[string]any{
		"expiry_date_time": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}, adminAccess)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "past")

	// future expiry → token usable as Bearer access
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/createtoken", map[string]any{
		"expiry_date_time": time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	}, adminAccess)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	res = decodeAPIV1(t, w)
	require.Nil(t, res.Err)
	data, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var tokenBody struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(data, &tokenBody))
	require.NotEmpty(t, tokenBody.Token)

	w = env.doJSON(t, http.MethodGet, "/api/v1/config/load", nil, tokenBody.Token)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Expiry beyond 90 days must be rejected.
	w = env.doJSON(t, http.MethodPost, "/api/v1/config/createtoken", map[string]any{
		"expiry_date_time": time.Now().UTC().Add(100 * 24 * time.Hour).Format(time.RFC3339),
	}, adminAccess)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	res = decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "90")
}

func TestDataStatusJSONUnauthenticated(t *testing.T) {
	env := setupTestAPI(t)

	// public data routes do not require Bearer
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/status.json", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "openfsd-data.json")
	assert.Contains(t, w.Body.String(), "openfsd-servers.json")
}

func TestDataServersJSONUnauthenticated(t *testing.T) {
	env := setupTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/openfsd-servers.json", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "OPENFSD")
	assert.Contains(t, w.Body.String(), "localhost")
}

// TestBearerActorRevalidation covers KD-18: Bearer tokens on dual-accept resource
// groups revalidate against the DB so demotion / soft-delete / hard-delete take effect.
func TestBearerActorRevalidation(t *testing.T) {
	setRating := func(t *testing.T, env *testAPIEnv, cid, rating int) {
		t.Helper()
		u, err := env.server.dbRepo.UserRepo.GetUserByCID(cid)
		require.NoError(t, err)
		u.NetworkRating = rating
		u.Password = ""
		require.NoError(t, env.server.dbRepo.UserRepo.UpdateUser(u))
	}

	tests := []struct {
		name          string
		mutate        func(t *testing.T, env *testAPIEnv)
		method        string
		path          string
		body          any
		wantStatus    int
		wantErrSubstr string
	}{
		{
			name:       "valid_bearer_still_works",
			method:     http.MethodPost,
			path:       "/api/v1/user/load",
			body:       nil, // filled with self CID after login
			wantStatus: http.StatusOK,
		},
		{
			name: "soft_deleted_inactive_401",
			mutate: func(t *testing.T, env *testAPIEnv) {
				setRating(t, env, env.admin.CID, int(protocol.NetworkRatingInactive))
			},
			method:        http.MethodPost,
			path:          "/api/v1/user/load",
			wantStatus:    http.StatusUnauthorized,
			wantErrSubstr: "unauthorized",
		},
		{
			name: "suspended_401",
			mutate: func(t *testing.T, env *testAPIEnv) {
				setRating(t, env, env.admin.CID, int(protocol.NetworkRatingSuspended))
			},
			method:        http.MethodPost,
			path:          "/api/v1/user/load",
			wantStatus:    http.StatusUnauthorized,
			wantErrSubstr: "unauthorized",
		},
		{
			name: "hard_deleted_401",
			mutate: func(t *testing.T, env *testAPIEnv) {
				require.NoError(t, env.server.dbRepo.UserRepo.DeleteUser(env.admin.CID))
			},
			method:        http.MethodPost,
			path:          "/api/v1/user/load",
			wantStatus:    http.StatusUnauthorized,
			wantErrSubstr: "unauthorized",
		},
		{
			name: "demoted_admin_config_forbidden",
			mutate: func(t *testing.T, env *testAPIEnv) {
				// Still active, but no longer Administrator — authz ceiling from DB overlay.
				setRating(t, env, env.admin.CID, int(protocol.NetworkRatingObserver))
			},
			method:        http.MethodGet,
			path:          "/api/v1/config/load",
			body:          nil,
			wantStatus:    http.StatusForbidden,
			wantErrSubstr: "forbidden",
		},
		{
			name: "demoted_admin_can_load_self",
			mutate: func(t *testing.T, env *testAPIEnv) {
				setRating(t, env, env.admin.CID, int(protocol.NetworkRatingObserver))
			},
			method:     http.MethodPost,
			path:       "/api/v1/user/load",
			wantStatus: http.StatusOK,
		},
		{
			name: "demoted_admin_cannot_load_other",
			mutate: func(t *testing.T, env *testAPIEnv) {
				setRating(t, env, env.admin.CID, int(protocol.NetworkRatingObserver))
			},
			method: http.MethodPost,
			path:   "/api/v1/user/load",
			// body set to observer CID in loop
			wantStatus:    http.StatusForbidden,
			wantErrSubstr: "forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupTestAPI(t)
			access, _ := env.login(t, env.admin.CID, env.adminPass)

			if tt.mutate != nil {
				tt.mutate(t, env)
			}

			body := tt.body
			if tt.path == "/api/v1/user/load" && body == nil {
				// Default: load self; demoted-cannot-load-other overrides to other CID.
				cid := env.admin.CID
				if tt.name == "demoted_admin_cannot_load_other" {
					cid = env.observer.CID
				}
				body = map[string]any{"cid": cid}
			}

			w := env.doJSON(t, tt.method, tt.path, body, access)
			assert.Equal(t, tt.wantStatus, w.Code, w.Body.String())
			if tt.wantErrSubstr != "" {
				res := decodeAPIV1(t, w)
				require.NotNil(t, res.Err, w.Body.String())
				assert.Contains(t, *res.Err, tt.wantErrSubstr)
			}
		})
	}
}

// TestBearerActorRevalidationSessionUnaffected ensures cookie dual-accept still works
// when revalidateBearerActor is on the chain (session path already revalidated; middleware skips).
func TestBearerActorRevalidationSessionUnaffected(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "sess-ok1", int(protocol.NetworkRatingSupervisor))
	cookies := formLogin(t, ts, user.CID, "sess-ok1")

	// Refresh CSRF cookie from an authed HTML page (same pattern as TestAPICookieAuthRequiresCSRF).
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Cookie", cookieHeader(cookies))
	w := httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cookies = mergeCookies(cookies, w.Result())
	csrf := csrfFromCookies(cookies)
	require.NotEmpty(t, csrf, "csrf cookie after dashboard")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/user/load",
		strings.NewReader(fmt.Sprintf(`{"cid":%d}`, user.CID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHeader(cookies))
	req.Header.Set(csrfHeaderName, csrf)
	w = httptest.NewRecorder()
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decodeAPIV1(t, w)
	require.Nil(t, res.Err)
}
