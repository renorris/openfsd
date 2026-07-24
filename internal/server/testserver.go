package server

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/metar"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/pkg/protocol"
	_ "modernc.org/sqlite"
)

// TestJWTSecret is a fixed HS256 secret used by StartTestServer so tests can
// mint JWTs without reading the generated config value.
const TestJWTSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// Seeded test account passwords (plaintext; hashed by CreateUser).
const (
	TestPilotPassword = "pilot-pass"
	TestATCPassword   = "atc-pass"
	TestSupPassword   = "sup-pass"
)

// TestMETARKJFKBody is the NOAA station-file body served for KJFK by the fake transport.
const TestMETARKJFKBody = "2024/01/15 12:00\nKJFK 151200Z 18010KT 10SM FEW050 22/12 A3001\n"

// fakeMetarHTTP is an injectable metar.HTTPDoer that never hits NOAA.
// It maps ICAO codes extracted from the stations path to bodies.
type fakeMetarHTTP struct {
	stations map[string]string // uppercase ICAO -> NOAA station file body
}

func (f *fakeMetarHTTP) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	}
	icao := icaoFromMetarPath(req.URL.Path)
	body, ok := f.stations[icao]
	if !ok || body == "" {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// icaoFromMetarPath extracts ICAO from .../stations/KJFK.TXT style paths.
func icaoFromMetarPath(path string) string {
	const marker = "/stations/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	rest := path[i+len(marker):]
	rest = strings.TrimSuffix(rest, ".TXT")
	rest = strings.TrimSuffix(rest, ".txt")
	if len(rest) != 4 {
		return ""
	}
	return strings.ToUpper(rest)
}

// TestServer is a fully wired FSD server for e2e tests.
//
// Call StartTestServer to construct one. t.Cleanup shuts it down.
type TestServer struct {
	// FSDAddr is the TCP address for FSD clients (host:port on 127.0.0.1).
	FSDAddr string
	// HTTPAddr is the service HTTP listen address (host:port on 127.0.0.1).
	HTTPAddr string
	// JWTSecret is the fixed secret written into ConfigKV (TestJWTSecret).
	JWTSecret string

	// Seeded users (CID assigned by SQLite autoincrement).
	PilotCID      int
	PilotPassword string
	ATCCID        int
	ATCPassword   string
	SupCID        int
	SupPassword   string

	// Second pilot for multi-client scenarios (same password as Pilot).
	Pilot2CID int

	// ConfigRepo / UserRepo expose seeded stores for e2e policy tests.
	ConfigRepo db.ConfigRepository
	UserRepo   db.UserRepository

	sqlDB  *sql.DB
	cancel context.CancelFunc
	done   <-chan error
}

// TestServerOptions customizes StartTestServer for security/limit e2e cases.
// Zero values keep historical unlimited-connection e2e behaviour.
type TestServerOptions struct {
	// MaxSessionsPerCID is FsdMaxSessionsPerCID (0 = unlimited).
	MaxSessionsPerCID int
	// MaxConnections is FsdMaxConnections (0 = unlimited).
	MaxConnections int
	// MaxConnectionsPerIP is FsdMaxConnectionsPerIP (0 = unlimited).
	MaxConnectionsPerIP int
	// EnableRateLimits sets FsdEnableRateLimits.
	EnableRateLimits bool
	// LoginTimeout / IdleTimeout override FSD timeouts (0 = disabled).
	LoginTimeout time.Duration
	IdleTimeout  time.Duration
}

// StartTestServer mirrors NewDefault essentials for tests:
//   - SQLite temp file, migrate, InitDefaultConfig
//   - Seed users with known passwords
//   - Fixed JWT secret
//   - Fake METAR HTTP transport (no real NOAA)
//   - FSD listen on 127.0.0.1:0
//   - Service HTTP on a pre-bound 127.0.0.1 listener (no bind/close/rebind TOCTOU)
//   - SweatboxEnabled true by default (empty engine until LoadAirport/scenario)
//
// Returns FSD/HTTP addresses and registers t.Cleanup for shutdown.
func StartTestServer(t testing.TB) *TestServer {
	return StartTestServerOpts(t, TestServerOptions{})
}

// StartTestServerOpts is StartTestServer with security limit overrides.
func StartTestServerOpts(t testing.TB, opts TestServerOptions) *TestServer {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())

	dir := t.TempDir()
	dsn := filepath.Join(dir, "openfsd-e2e.db")
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		cancel()
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)

	if err = sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("ping sqlite: %v", err)
	}
	if err = db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("migrate: %v", err)
	}

	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("repositories: %v", err)
	}

	// Seed known users before config so CIDs are stable for assertions.
	pilot := &db.User{
		Password:      TestPilotPassword,
		FirstName:     strPtr("E2E Pilot"),
		NetworkRating: int(protocol.NetworkRatingObserver),
	}
	if err = repos.UserRepo.CreateUser(pilot); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("seed pilot: %v", err)
	}

	pilot2 := &db.User{
		Password:      TestPilotPassword,
		FirstName:     strPtr("E2E Pilot Two"),
		NetworkRating: int(protocol.NetworkRatingObserver),
	}
	if err = repos.UserRepo.CreateUser(pilot2); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("seed pilot2: %v", err)
	}

	atc := &db.User{
		Password:      TestATCPassword,
		FirstName:     strPtr("E2E ATC"),
		NetworkRating: int(protocol.NetworkRatingController1),
	}
	if err = repos.UserRepo.CreateUser(atc); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("seed atc: %v", err)
	}

	sup := &db.User{
		Password:      TestSupPassword,
		FirstName:     strPtr("E2E Supervisor"),
		NetworkRating: int(protocol.NetworkRatingSupervisor),
	}
	if err = repos.UserRepo.CreateUser(sup); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("seed sup: %v", err)
	}

	if err = db.InitDefaultConfig(repos.ConfigRepo); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("InitDefaultConfig: %v", err)
	}
	// Overwrite random secret with fixed test secret.
	if err = repos.ConfigRepo.Set(db.ConfigJwtSecretKey, TestJWTSecret); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("set JWT secret: %v", err)
	}

	metarSvc := metar.New(2, &fakeMetarHTTP{
		stations: map[string]string{
			"KJFK": TestMETARKJFKBody,
		},
	})

	// Pre-bind HTTP listener and hand it to the server (no freeLocalAddr TOCTOU).
	httpLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("http listen: %v", err)
	}
	httpAddr := httpLn.Addr().String()

	// Bound FSD address reported by gnet OnBoot (or classic listenLoop).
	fsdAddrCh := make(chan string, 2)

	cfg := &Config{
		FsdListenAddrs:         []string{"127.0.0.1:0"},
		FsdNumEventLoop:        2,
		NumMetarWorkers:        2,
		ServiceHTTPListenAddr:  httpAddr,
		DatabaseDriver:         "sqlite",
		DatabaseSourceName:     dsn,
		DatabaseMaxConns:       4,
		FsdMaxSessionsPerCID:   opts.MaxSessionsPerCID,
		FsdMaxConnections:      opts.MaxConnections,
		FsdMaxConnectionsPerIP: opts.MaxConnectionsPerIP,
		FsdEnableRateLimits:    opts.EnableRateLimits,
		FsdLoginTimeout:        opts.LoginTimeout,
		FsdIdleTimeout:         opts.IdleTimeout,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	srv, err := New(Deps{
		Config:          cfg,
		Users:           repos.UserRepo,
		ConfigKV:        repos.ConfigRepo,
		Registry:        postoffice.New(),
		Metar:           metarSvc,
		Logger:          logger,
		SweatboxEnabled: true, // e2e convenience; empty until airport/scenario load
		// Production gnet path (Listen nil). Bound address via FSDBound.
		FSDBound: fsdAddrCh,
		// Return the already-bound listener; never rebind.
		HTTPListen: func(network, addr string) (net.Listener, error) {
			_ = network
			_ = addr
			return httpLn, nil
		},
	})
	if err != nil {
		_ = httpLn.Close()
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	var fsdAddr string
	select {
	case fsdAddr = <-fsdAddrCh:
	case err := <-done:
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("server exited before listen: %v", err)
	case <-time.After(8 * time.Second):
		cancel()
		_ = sqlDB.Close()
		t.Fatal("timeout waiting for FSD listener (gnet OnBoot / classic bind)")
	}
	// gnet may report 0.0.0.0 — clients should dial loopback.
	if host, port, err := net.SplitHostPort(fsdAddr); err == nil {
		if host == "0.0.0.0" || host == "::" {
			fsdAddr = net.JoinHostPort("127.0.0.1", port)
		}
	}

	waitHTTPReady(t, httpAddr)

	ts := &TestServer{
		FSDAddr:       fsdAddr,
		HTTPAddr:      httpAddr,
		JWTSecret:     TestJWTSecret,
		PilotCID:      pilot.CID,
		PilotPassword: TestPilotPassword,
		ATCCID:        atc.CID,
		ATCPassword:   TestATCPassword,
		SupCID:        sup.CID,
		SupPassword:   TestSupPassword,
		Pilot2CID:     pilot2.CID,
		ConfigRepo:    repos.ConfigRepo,
		UserRepo:      repos.UserRepo,
		sqlDB:         sqlDB,
		cancel:        cancel,
		done:          done,
	}

	t.Cleanup(func() {
		ts.Shutdown()
	})

	return ts
}

// Shutdown stops the server and closes the database. Idempotent; also registered via t.Cleanup.
// Waits for Server.Run to return (which joins service HTTP) before closing sqlDB.
func (ts *TestServer) Shutdown() {
	if ts.cancel != nil {
		ts.cancel()
		ts.cancel = nil
	}
	if ts.done != nil {
		select {
		case <-ts.done:
		case <-time.After(8 * time.Second):
		}
		ts.done = nil
	}
	if ts.sqlDB != nil {
		_ = ts.sqlDB.Close()
		ts.sqlDB = nil
	}
}

// MakeFSDJWT mints a password-field JWT accepted by FSD login (token_type=fsd).
func (ts *TestServer) MakeFSDJWT(cid int, rating protocol.NetworkRating) (string, error) {
	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd",
		CID:           cid,
		NetworkRating: rating,
	}, time.Hour)
	if err != nil {
		return "", err
	}
	return tok.SignedString([]byte(ts.JWTSecret))
}

// MakeServiceJWT mints a service HTTP bearer token (token_type=fsd_service, admin rating).
func (ts *TestServer) MakeServiceJWT() (string, error) {
	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd_service",
		CID:           -1,
		NetworkRating: protocol.NetworkRatingAdministator,
	}, time.Hour)
	if err != nil {
		return "", err
	}
	return tok.SignedString([]byte(ts.JWTSecret))
}

// HTTPBaseURL returns http:// + HTTPAddr for service HTTP clients.
func (ts *TestServer) HTTPBaseURL() string {
	return "http://" + ts.HTTPAddr
}

// httpClient is a short-timeout client for readiness and e2e service HTTP polls.
func httpClient() *http.Client {
	return &http.Client{Timeout: 200 * time.Millisecond}
}

func waitHTTPReady(t testing.TB, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	url := fmt.Sprintf("http://%s/online_users", addr)
	client := httpClient()
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) //nolint:gosec // test-only loopback
		if err == nil {
			_ = resp.Body.Close()
			// Auth middleware returns 400 without Bearer — any response means up.
			return
		}
		// Connection errors only: brief backoff then retry (bounded by deadline).
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("service HTTP not ready at %s", addr)
}
