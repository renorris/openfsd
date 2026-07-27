//go:build verifyperf

// Gnet FSD plane baseline smoke (historical A/B vs classic removed with dual-path).
//
//	go test -tags=verifyperf -count=1 -timeout=180s ./internal/server/ -run TestIO_GnetBaseline -v
//
// Reports goroutine counts, send throughput, and peer receive counts under a
// hub-like position storm. Not compiled into default test runs.
package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/metar"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/fsdclient"
	"github.com/renorris/openfsd/pkg/protocol"
	_ "modernc.org/sqlite"
)

func abPilots() int {
	if v := os.Getenv("OPENFSD_AB_M"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

func abDuration() time.Duration {
	if v := os.Getenv("OPENFSD_AB_T"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 5 * time.Second
}

func abHz() float64 {
	if v := os.Getenv("OPENFSD_AB_HZ"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 5.0 // aggressive vs production ~0.2Hz to stress write path
}

type abResult struct {
	name           string
	pilots         int
	sends          int64
	recvPos        int64
	goroutinesEnd  int
	goroutinesPeak int
	elapsed        time.Duration
	sendPerSec     float64
	recvPerSec     float64
}

func TestIO_GnetBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("verifyperf under -short")
	}
	m := abPilots()
	dur := abDuration()
	hz := abHz()

	gnetRes := runIOAB(t, "gnet", m, dur, hz)

	t.Logf("=== gnet baseline (M=%d T=%s hz=%.1f) ===", m, dur, hz)
	logAB(t, gnetRes)

	if gnetRes.sends == 0 || gnetRes.recvPos == 0 {
		t.Fatal("gnet path produced zero traffic")
	}
	if gnetRes.goroutinesEnd < 1 {
		t.Fatal("unexpected zero goroutines")
	}
}

func logAB(t *testing.T, r abResult) {
	t.Helper()
	t.Logf("%s: sends=%d (%.0f/s) recvPos=%d (%.0f/s) goroutines_end=%d peak=%d wall=%s",
		r.name, r.sends, r.sendPerSec, r.recvPos, r.recvPerSec,
		r.goroutinesEnd, r.goroutinesPeak, r.elapsed.Round(time.Millisecond))
}

func max1(a int) int {
	if a < 1 {
		return 1
	}
	return a
}

func maxF1(a float64) float64 {
	if a < 1 {
		return 1
	}
	return a
}

func runIOAB(t *testing.T, name string, m int, dur time.Duration, hz float64) abResult {
	t.Helper()
	ts := startABServer(t, m)

	clients := make([]*fsdclient.Client, m)
	callsigns := make([]string, m)
	for i := 0; i < m; i++ {
		cs := fmt.Sprintf("AB%03d", i)
		callsigns[i] = cs
		c := dialAB(t, ts.fsdAddr)
		tok, err := ts.makeJWT(ts.cids[i])
		if err != nil {
			t.Fatalf("jwt: %v", err)
		}
		loginPilotAB(t, c, cs, ts.cids[i], tok)
		waitMOTDAB(t, c, cs)
		clients[i] = c
	}

	var recv atomic.Int64
	var sends atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var rg sync.WaitGroup
	for i := 0; i < m; i++ {
		rg.Add(1)
		go func(c *fsdclient.Client) {
			defer rg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				rctx, rcancel := context.WithTimeout(ctx, 200*time.Millisecond)
				r, err := c.Next(rctx)
				rcancel()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				if r.Type == protocol.PacketTypePilotPosition {
					recv.Add(1)
				}
			}
		}(clients[i])
	}

	interval := time.Duration(float64(time.Second) / hz)
	var peakG atomic.Int64
	peakG.Store(int64(runtime.NumGoroutine()))

	rg.Add(1)
	go func() {
		defer rg.Done()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n := int64(runtime.NumGoroutine())
				for {
					old := peakG.Load()
					if n <= old || peakG.CompareAndSwap(old, n) {
						break
					}
				}
			}
		}
	}()

	// Warm: one position each so peers discover each other.
	for i := 0; i < m; i++ {
		if err := clients[i].SendPilotPosition(abPos(callsigns[i], i)); err != nil {
			t.Fatalf("warm pos: %v", err)
		}
		sends.Add(1)
	}
	time.Sleep(400 * time.Millisecond)

	start := time.Now()
	deadline := start.Add(dur)
	var wg sync.WaitGroup
	for i := 0; i < m; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := clients[idx]
			cs := callsigns[idx]
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			seq := 0
			for time.Now().Before(deadline) {
				select {
				case <-ticker.C:
				case <-time.After(time.Until(deadline)):
					return
				}
				seq++
				pos := abPos(cs, idx)
				pos.TrueAltitude = 1000 + seq
				if err := c.SendPilotPosition(pos); err != nil {
					return
				}
				sends.Add(1)
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(500 * time.Millisecond)
	gEnd := runtime.NumGoroutine()
	elapsed := time.Since(start)
	cancel()
	rg.Wait()

	for _, c := range clients {
		_ = c.Close(context.Background())
	}
	ts.shutdown()
	// Let process settle so next mode starts cleaner.
	time.Sleep(100 * time.Millisecond)

	s := sends.Load()
	r := recv.Load()
	sec := elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	return abResult{
		name:           name,
		pilots:         m,
		sends:          s,
		recvPos:        r,
		goroutinesEnd:  gEnd,
		goroutinesPeak: int(peakG.Load()),
		elapsed:        elapsed,
		sendPerSec:     float64(s) / sec,
		recvPerSec:     float64(r) / sec,
	}
}

func abPos(cs string, i int) protocol.PilotPosition {
	return protocol.PilotPosition{
		TransponderMode:  "S",
		Callsign:         cs,
		TransponderCode:  "1200",
		NetworkRating:    protocol.NetworkRatingObserver,
		Latitude:         33.94 + float64(i%10)*0.001,
		Longitude:        -118.40 + float64(i/10)*0.001,
		TrueAltitude:     5000,
		Groundspeed:      250,
		PitchBankHeading: 0,
	}
}

type abServer struct {
	fsdAddr string
	cids    []int
	secret  string
	cancel  context.CancelFunc
	done    <-chan error
	sqlDB   *sql.DB
}

func (s *abServer) shutdown() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(8 * time.Second):
		}
		s.done = nil
	}
	if s.sqlDB != nil {
		_ = s.sqlDB.Close()
		s.sqlDB = nil
	}
}

func (s *abServer) makeJWT(cid int) (string, error) {
	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd",
		CID:           cid,
		NetworkRating: protocol.NetworkRatingObserver,
	}, time.Hour)
	if err != nil {
		return "", err
	}
	return tok.SignedString([]byte(s.secret))
}

func startABServer(t *testing.T, m int) *abServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())

	dir := t.TempDir()
	dsn := filepath.Join(dir, "ab.db")
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	if err = sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}
	if err = db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}
	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}
	if err = db.InitDefaultConfig(repos.ConfigRepo); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}
	const secret = server.TestJWTSecret
	if err = repos.ConfigRepo.Set(db.ConfigJwtSecretKey, secret); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}

	cids := make([]int, m)
	for i := 0; i < m; i++ {
		name := fmt.Sprintf("AB%d", i)
		u := &db.User{
			Password:      "ab-pass",
			FirstName:     &name,
			NetworkRating: int(protocol.NetworkRatingObserver),
		}
		if err = repos.UserRepo.CreateUser(u); err != nil {
			_ = sqlDB.Close()
			cancel()
			t.Fatal(err)
		}
		cids[i] = u.CID
	}

	httpLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}
	httpAddr := httpLn.Addr().String()
	fsdAddrCh := make(chan string, 2)

	cfg := &server.Config{
		FsdListenAddrs:        []string{"127.0.0.1:0"},
		FsdNumEventLoop:       2,
		NumMetarWorkers:       1,
		ServiceHTTPListenAddr: httpAddr,
		DatabaseDriver:        "sqlite",
		DatabaseSourceName:    dsn,
		DatabaseMaxConns:      8,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	srv, err := server.New(server.Deps{
		Config:          cfg,
		Users:           repos.UserRepo,
		ConfigKV:        repos.ConfigRepo,
		Registry:        postoffice.New(),
		Metar:           metar.New(1, abNoopHTTP{}),
		Logger:          logger,
		SweatboxEnabled: false,
		FSDBound:        fsdAddrCh,
		HTTPListen: func(network, addr string) (net.Listener, error) {
			return httpLn, nil
		},
	})
	if err != nil {
		_ = httpLn.Close()
		_ = sqlDB.Close()
		cancel()
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	var fsdAddr string
	select {
	case fsdAddr = <-fsdAddrCh:
	case err := <-done:
		cancel()
		t.Fatalf("server exit: %v", err)
	case <-time.After(8 * time.Second):
		cancel()
		t.Fatal("timeout FSD bind")
	}
	if host, port, err := net.SplitHostPort(fsdAddr); err == nil {
		if host == "0.0.0.0" || host == "::" {
			fsdAddr = net.JoinHostPort("127.0.0.1", port)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + httpAddr + "/online_users") //nolint:gosec
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	ab := &abServer{
		fsdAddr: fsdAddr,
		cids:    cids,
		secret:  secret,
		cancel:  cancel,
		done:    done,
		sqlDB:   sqlDB,
	}
	t.Cleanup(ab.shutdown)
	return ab
}

type abNoopHTTP struct{}

func (abNoopHTTP) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     make(http.Header),
	}, nil
}

func dialAB(t *testing.T, addr string) *fsdclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := fsdclient.Dial(ctx, fsdclient.Config{
		Addr:        addr,
		DialTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c
}

func loginPilotAB(t *testing.T, c *fsdclient.Client, callsign string, cid int, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.LoginPilot(ctx, fsdclient.PilotLogin{
		Callsign:      callsign,
		CID:           strconv.Itoa(cid),
		Token:         token,
		NetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision: 100,
		SimulatorType: 1,
		RealName:      "AB Pilot",
		ClientIdent: &protocol.ClientIdent{
			SoftwareID:   "88e4",
			SoftwareName: "ab",
			VersionMajor: 1,
			VersionMinor: 0,
			SystemUID:    1,
		},
	})
	if err != nil {
		t.Fatalf("LoginPilot %s: %v", callsign, err)
	}
}

func waitMOTDAB(t *testing.T, c *fsdclient.Client, callsign string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage &&
			bytes.Contains(r.Raw, []byte(callsign)) &&
			bytes.Contains(r.Raw, []byte("Connected to openfsd"))
	})
	if err != nil {
		t.Fatalf("wait MOTD for %s: %v", callsign, err)
	}
}
