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
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/auth"
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

// fakeMetarHTTP is an injectable metar.HTTPDoer that never hits NOAA.
type fakeMetarHTTP struct {
	body string
}

func (f *fakeMetarHTTP) Do(req *http.Request) (*http.Response, error) {
	_ = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
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

	sqlDB  *sql.DB
	cancel context.CancelFunc
	done   <-chan error
}

// StartTestServer mirrors NewDefault essentials for tests:
//   - SQLite temp file, migrate, InitDefaultConfig
//   - Seed users with known passwords
//   - Fixed JWT secret
//   - Fake METAR HTTP transport (no real NOAA)
//   - FSD listen on 127.0.0.1:0
//   - Service HTTP on a free 127.0.0.1 port
//
// Returns FSD/HTTP addresses and registers t.Cleanup for shutdown.
func StartTestServer(t testing.TB) *TestServer {
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

	// NOAA station file format: timestamp line, METAR line, trailing newline.
	const metarBody = "2024/01/15 12:00\nKJFK 151200Z 18010KT 10SM FEW050 22/12 A3001\n"
	metarSvc := metar.New(2, &fakeMetarHTTP{body: metarBody})

	httpAddr := freeLocalAddr(t)

	var fsdOnce sync.Once
	fsdAddrCh := make(chan string, 1)

	cfg := &Config{
		FsdListenAddrs:        []string{"127.0.0.1:0"},
		NumMetarWorkers:       2,
		ServiceHTTPListenAddr: httpAddr,
		DatabaseDriver:        "sqlite",
		DatabaseSourceName:    dsn,
		DatabaseMaxConns:      4,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	srv, err := New(Deps{
		Config:   cfg,
		Users:    repos.UserRepo,
		ConfigKV: repos.ConfigRepo,
		Registry: postoffice.New(),
		Metar:    metarSvc,
		Logger:   logger,
		Listen: func(ctx context.Context, network, addr string) (net.Listener, error) {
			_ = ctx
			_ = addr
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				return nil, err
			}
			fsdOnce.Do(func() {
				fsdAddrCh <- ln.Addr().String()
			})
			return ln, nil
		},
	})
	if err != nil {
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
	case <-time.After(5 * time.Second):
		cancel()
		_ = sqlDB.Close()
		t.Fatal("timeout waiting for FSD listener")
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
func (ts *TestServer) Shutdown() {
	if ts.cancel != nil {
		ts.cancel()
		ts.cancel = nil
	}
	if ts.done != nil {
		select {
		case <-ts.done:
		case <-time.After(5 * time.Second):
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

func freeLocalAddr(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeLocalAddr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitHTTPReady(t testing.TB, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	url := fmt.Sprintf("http://%s/online_users", addr)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // test-only loopback
		if err == nil {
			_ = resp.Body.Close()
			// Auth middleware returns 400 without Bearer — any response means up.
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("service HTTP not ready at %s", addr)
}
