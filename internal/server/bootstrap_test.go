package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, cfg.FsdListenAddrs)
	require.Equal(t, "sqlite", cfg.DatabaseDriver)
	require.NotZero(t, cfg.NumMetarWorkers)
}

func TestNewDefaultCreatesAdmin(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "boot.db")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", dsn)
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("DATABASE_MAX_CONNS", "2")
	t.Setenv("FSD_LISTEN_ADDRS", ":0")
	t.Setenv("SERVICE_HTTP_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("NUM_METAR_WORKERS", "1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := NewDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// CID 1 should exist after generateDefaultAdminUser
	u, err := srv.users.GetUserByCID(1)
	require.NoError(t, err)
	require.Equal(t, 1, u.CID)

	// Second call should not recreate admin
	srv2, err := NewDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, srv2)
}

func TestGenerateDefaultAdminUser(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "admin.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(sqlDB))
	repos, err := db.NewRepositories(sqlDB)
	require.NoError(t, err)

	user, err := generateDefaultAdminUser(repos)
	require.NoError(t, err)
	require.Equal(t, 1, user.CID)
	require.NotEmpty(t, user.Password)
	require.Equal(t, int(protocol.NetworkRatingAdministator), user.NetworkRating)
}

func TestServiceHTTPAuthAndKick(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := postoffice.New()
	// map config
	kv := &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	srv, err := New(Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}, ServiceHTTPListenAddr: "127.0.0.1:0"},
		Users:    stubUserStore{},
		ConfigKV: kv,
		Registry: reg,
		Metar:    &recordingMetar{},
		Clock:    realClock{},
	})
	require.NoError(t, err)

	// Seed a session to kick
	s := newSess("KICKME", false, NetworkRatingObserver)
	require.NoError(t, reg.Register(s))

	e := srv.setupRoutes()

	// Missing bearer
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/online_users", nil)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Bad token
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/online_users", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Valid fsd_service admin token
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/online_users", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Forbidden rating
	tokObs := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingObserver)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/online_users", nil)
	req.Header.Set("Authorization", "Bearer "+tokObs)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	// Kick bad body
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/kick_user", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Kick missing callsign
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/kick_user", strings.NewReader(`{"callsign":"NONE"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)

	// Kick OK
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/kick_user", strings.NewReader(`{"callsign":"KICKME"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	select {
	case <-s.Ctx.Done():
	default:
		t.Fatal("kick should cancel session")
	}
}

// TestOnlineUsersSyntheticBadge ensures sweatbox pilots expose synthetic:true
// on GET /online_users while human pilots omit the field (omitempty).
func TestOnlineUsersSyntheticBadge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := postoffice.New()
	kv := &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	srv, err := New(Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}, ServiceHTTPListenAddr: "127.0.0.1:0"},
		Users:    stubUserStore{},
		ConfigKV: kv,
		Registry: reg,
		Metar:    &recordingMetar{},
		Clock:    realClock{},
	})
	require.NoError(t, err)

	human := newSess("HUMAN1", false, NetworkRatingObserver)
	require.NoError(t, reg.Register(human))

	synth := newSess("SBX1", false, NetworkRatingObserver)
	synth.Synthetic = true
	require.NoError(t, reg.Register(synth))

	e := srv.setupRoutes()
	tok := mintServiceToken(t, TestJWTSecret, protocol.NetworkRatingAdministator)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/online_users", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	var data OnlineUsersResponseData
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &data))
	var sawHuman, sawSynth bool
	for _, p := range data.Pilots {
		switch p.Callsign {
		case "HUMAN1":
			sawHuman = true
			require.False(t, p.Synthetic, "human pilot must not be synthetic")
		case "SBX1":
			sawSynth = true
			require.True(t, p.Synthetic, "sweatbox pilot must be synthetic")
		}
	}
	require.True(t, sawHuman && sawSynth, "expected both pilots in snapshot")
	// Wire JSON must include synthetic:true for sweatbox (operators / dashboard badge).
	require.Contains(t, body, `"synthetic":true`)
	// Human pilots must omit synthetic when false (omitempty).
	require.NotContains(t, body, `"synthetic":false`)
}

func TestRunServiceHTTPAndListen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()

	// Pre-bind HTTP listener for HTTPListen inject
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	httpAddr := httpLn.Addr().String()

	fsdLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	fsdAddr := fsdLn.Addr().String()

	kv := &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	srv, err := New(Deps{
		Config: &Config{
			FsdListenAddrs:        []string{fsdAddr},
			ServiceHTTPListenAddr: httpAddr,
		},
		Users:    stubUserStore{},
		ConfigKV: kv,
		Registry: postoffice.New(),
		Metar:    &recordingMetar{},
		Clock:    realClock{},
		Listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			return fsdLn, nil
		},
		HTTPListen: func(network, address string) (net.Listener, error) {
			return httpLn, nil
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Give listeners a moment
	time.Sleep(50 * time.Millisecond)
	// Connect TCP briefly
	conn, err := net.DialTimeout("tcp", fsdAddr, time.Second)
	if err == nil {
		_ = conn.Close()
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	_ = addr
}

type mapConfig struct{ m map[string]string }

func (m *mapConfig) Get(key string) (string, error) {
	v, ok := m.m[key]
	if !ok {
		return "", db.ErrConfigKeyNotFound
	}
	return v, nil
}

func mintServiceToken(t *testing.T, secret string, rating protocol.NetworkRating) string {
	t.Helper()
	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd_service",
		CID:           -1,
		NetworkRating: rating,
	}, time.Hour)
	require.NoError(t, err)
	s, err := tok.SignedString([]byte(secret))
	require.NoError(t, err)
	return s
}
