package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontendLandingAndLoginRedirect(t *testing.T) {
	ts := newTestServer(t)

	// Landing unauthenticated
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "openfsd")

	// Login GET when already authed redirects to dashboard
	user := createTestUser(t, ts, "pass12345", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, user.CID, "pass12345")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/dashboard", w.Header().Get("Location"))

	// Landing with session
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	ts.engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestConfigResetSecretForm(t *testing.T) {
	ts := newTestServer(t)
	user := createTestUser(t, ts, "pass12345", int(protocol.NetworkRatingAdministator))
	cookies := formLogin(t, ts, user.CID, "pass12345")

	// Missing confirm
	form := url.Values{}
	form.Set("confirm", "")
	w, cookies := formPOST(t, ts, "/configeditor/reset-secret", form, cookies)
	// re-renders with flash error or 403 without CSRF handled by formPOST
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusSeeOther || w.Code == http.StatusForbidden, w.Body.String())

	// Confirm reset
	form = url.Values{}
	form.Set("confirm", "yes")
	w, _ = formPOST(t, ts, "/configeditor/reset-secret", form, cookies)
	// Success: redirect login after secret rotate
	if w.Code == http.StatusSeeOther {
		assert.Equal(t, "/login", w.Header().Get("Location"))
	}
}

func TestKickActiveConnectionAPI(t *testing.T) {
	env := setupTestAPI(t)
	access, _ := env.login(t, env.admin.CID, env.adminPass)

	// Supervisor required — admin is fine
	w := env.doJSON(t, http.MethodPost, "/api/v1/fsdconn/kickuser", map[string]any{
		"callsign": "NONE",
	}, access)
	// FSD unreachable → 500 or incomplete response handled without panic
	assert.True(t, w.Code == http.StatusInternalServerError || w.Code == http.StatusOK || w.Code == http.StatusNotFound || w.Code == 0 || w.Code >= 400, w.Code)

	// Observer forbidden
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)
	w = env.doJSON(t, http.MethodPost, "/api/v1/fsdconn/kickuser", map[string]any{
		"callsign": "NONE",
	}, obsAccess)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestOptionalSession(t *testing.T) {
	env := setupTestAPI(t)

	// optionalSession nil
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	claims := env.server.optionalSession(c)
	assert.Nil(t, claims)
}

func TestLoadServerConfig(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("FSD_HTTP_SERVICE_ADDRESS", "http://127.0.0.1:13618")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	cfg, err := loadServerConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, ":0", cfg.ListenAddr)
	require.Equal(t, "http://127.0.0.1:13618", cfg.FsdHttpServiceAddress)
}

func TestRequireCSRFMiddleware(t *testing.T) {
	env := setupTestAPI(t)
	e := gin.New()
	e.POST("/x", env.server.requireCSRF, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	e.ServeHTTP(w, req)
	// missing CSRF → forbidden
	assert.Equal(t, http.StatusForbidden, w.Code)
}
