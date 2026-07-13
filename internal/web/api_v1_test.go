package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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

	sqlDB, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
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

	cfg := &ServerConfig{
		ListenAddr:            ":0",
		DatabaseDriver:        "sqlite",
		DatabaseSourceName:    ":memory:",
		DatabaseMaxConns:      1,
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

	// missing body
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
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	// missing Authorization
	w := env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	res := decodeAPIV1(t, w)
	require.NotNil(t, res.Err)
	assert.Contains(t, *res.Err, "bearer")

	// garbage token
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, "not-a-jwt")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// valid access token
	w = env.doJSON(t, http.MethodPost, "/api/v1/user/load", map[string]any{"cid": env.admin.CID}, access)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
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
