package server

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
	"golang.org/x/crypto/bcrypt"
)

// TestPilotCannotEmitATCPosition ensures role×packet allowlist drops pilot %.
func TestPilotCannotEmitATCPosition(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	pilot := newSess("N100", false, NetworkRatingController1) // high rating but pilot
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{pilot, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}
	// Pilot tries to emit ATC position with huge range — must be ignored.
	srv.handleATCPosition(pilot, []byte("%N100:28550:0:9999:12:34.0:-118.0:0\r\n"))
	out := drain(peer)
	for _, p := range out {
		if strings.HasPrefix(p, "%N100") {
			t.Fatalf("peer must not receive pilot-originated %%: %q", p)
		}
	}
}

// TestATCCannotEmitPilotPosition drops ATC @ and fast velocity streams.
func TestATCCannotEmitPilotPosition(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{atc, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}
	srv.handlePilotPosition(atc, []byte("@S:LAX_TWR:1200:1:34.0:-118.0:5000:250:0:0\r\n"))
	srv.handleFastPilotPosition(atc, []byte("^LAX_TWR:34.0:-118.0:5000:250:0:0:0:0:0:0:0:0\r\n"))
	out := drain(peer)
	for _, p := range out {
		if strings.Contains(p, "@S:LAX_TWR") || strings.HasPrefix(p, "^LAX_TWR") {
			t.Fatalf("peer must not receive ATC pilot stream: %q", p)
		}
	}
}

// TestPilotPositionRewritesRating ensures wire rating is replaced by session rating.
func TestPilotPositionRewritesRating(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	// Session rating OBS (1); packet claims rating 12 (admin).
	p1 := newSess("N100", false, NetworkRatingObserver)
	p2 := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{p1, p2} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}
	// @MODE:CS:XPDR:RATING:LAT:LON:...
	srv.handlePilotPosition(p1, []byte("@S:N100:1200:12:34.0:-118.0:5000:250:0:0\r\n"))
	out := drain(p2)
	found := false
	for _, p := range out {
		if !strings.Contains(p, "@S:N100:") {
			continue
		}
		found = true
		if strings.Contains(p, ":1200:12:") {
			t.Fatalf("wire rating not rewritten: %q", p)
		}
		if !strings.Contains(p, ":1200:1:") {
			t.Fatalf("expected session rating 1 in fan-out: %q", p)
		}
	}
	if !found {
		t.Fatalf("peer did not receive position: %v", out)
	}
}

// TestATCPositionRewritesRatingAndCapsVisRange caps range and rewrites rating.
func TestATCPositionRewritesRatingAndCapsVisRange(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	srv.cfg.FsdMaxAtcVisRangeNM = 1500
	atc := newSess("LAX_CTR", true, NetworkRatingController1)
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{atc, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	reg.UpdatePosition(atc, [2]float64{34, -118}, 40*1852)
	reg.UpdatePosition(peer, [2]float64{34.1, -118}, 50*1852)

	// %FREQ:CS:FAC:VIS:RATING:LAT:LON:...
	srv.handleATCPosition(atc, []byte("%LAX_CTR:28550:6:99999:12:34.0:-118.0:0\r\n"))

	wantM := 1500.0 * 1852.0
	if got := atc.VisRange.Load(); got != wantM {
		t.Fatalf("VisRange=%v want %v (1500 NM)", got, wantM)
	}
	out := drain(peer)
	for _, p := range out {
		if !strings.HasPrefix(p, "%LAX_CTR:") {
			continue
		}
		fields := strings.Split(strings.TrimSuffix(strings.TrimSuffix(p, "\n"), "\r"), ":")
		// After rewrite: rating field index 4 should be session C1.
		if len(fields) > 4 && fields[4] != strconv.Itoa(int(NetworkRatingController1)) {
			t.Fatalf("rating field=%q want %d in %q", fields[4], NetworkRatingController1, p)
		}
	}
}

// TestATCPositionRejectsInvalidGeo rejects Inf/NaN/out-of-range lat lon.
func TestATCPositionRejectsInvalidGeo(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	for _, pkt := range []string{
		"%LAX_TWR:28550:2:50:1:NaN:-118.0:0\r\n",
		"%LAX_TWR:28550:2:50:1:34.0:Inf:0\r\n",
		"%LAX_TWR:28550:2:50:1:91.0:-118.0:0\r\n",
		"%LAX_TWR:28550:2:50:1:34.0:-181.0:0\r\n",
	} {
		drain(atc)
		srv.handleATCPosition(atc, []byte(pkt))
		out := drain(atc)
		if !hasOutboundContaining(out, "$ER") {
			t.Fatalf("expected $ER for %q, got %v", pkt, out)
		}
	}
}

// TestDeleteRebuildsServerLeavePacket does not rebroadcast client-forged CID/type.
func TestDeleteRebuildsServerLeavePacket(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	p := newSess("N100", false, NetworkRatingObserver)
	p.CID = 42
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{p, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}
	srv.handleDelete(p, []byte("#DAN100:SERVER:99999\r\n"))
	select {
	case <-p.Ctx.Done():
	default:
		t.Fatal("delete should disconnect")
	}
	out := drain(peer)
	if !hasOutboundContaining(out, "#DPN100:SERVER:42") {
		t.Fatalf("expected server-built #DP with CID 42, got %v", out)
	}
	if hasOutboundContaining(out, "#DAN100") {
		t.Fatalf("must not rebroadcast forged #DA: %v", out)
	}
	// Idempotent second disconnect notify
	srv.broadcastDisconnectPacket(p)
	out2 := drain(peer)
	if hasOutboundContaining(out2, "#DPN100") {
		t.Fatalf("double leave notify: %v", out2)
	}
}

// TestBroadcastDisconnectIdempotent only notifies once.
func TestBroadcastDisconnectIdempotent(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	a := newSess("A1", true, NetworkRatingController1)
	a.CID = 7
	b := newSess("B1", false, NetworkRatingObserver)
	for _, s := range []*session.Session{a, b} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	srv.broadcastDisconnectPacket(a)
	srv.broadcastDisconnectPacket(a)
	out := drain(b)
	count := 0
	for _, p := range out {
		if strings.Contains(p, "#DAA1:SERVER:7") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("leave packets=%d want 1: %v", count, out)
	}
}

// TestRateLimitsWhenEnabled drops excess text under FsdEnableRateLimits.
func TestRateLimitsWhenEnabled(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	srv.cfg.FsdEnableRateLimits = true
	p := newSess("N100", false, NetworkRatingObserver)
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{p, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}
	srv.handleTextMessage(p, []byte("#TMN100:N200:one\r\n"))
	srv.handleTextMessage(p, []byte("#TMN100:N200:two\r\n"))
	out := drain(peer)
	n := 0
	for _, pkt := range out {
		if strings.Contains(pkt, "#TMN100:N200:") {
			n++
		}
	}
	if n > 1 {
		t.Fatalf("rate limit failed: got %d DMs: %v", n, out)
	}
	if n < 1 {
		t.Fatalf("first message should pass: %v", out)
	}
}

// TestLoginParseSanitizesRealName strips CR/LF (and any colon) from RealName.
// Wire RealName is a single colon-field; CR/LF can still appear inside the field.
func TestLoginParseSanitizesRealName(t *testing.T) {
	id := []byte("$IDN1:SERVER:OPENFSD:0:1:1:1:1")
	// 8 fields: #APCS:SERVER:CID:TOKEN:RATING:PROTO:SIM:REALNAME
	add := []byte("#APN1:SERVER:100:pass:1:100:1:Evil\rName\n")
	data, token, code, msg, err := parseLoginPackets(id, add, time.Now())
	if err != nil {
		t.Fatalf("parse: %v code=%d msg=%s", err, code, msg)
	}
	if token != "pass" {
		t.Fatalf("token=%q", token)
	}
	if strings.ContainsAny(data.RealName, ":\r\n") {
		t.Fatalf("RealName still has delimiters: %q", data.RealName)
	}
	if data.RealName != "EvilName" {
		t.Fatalf("RealName=%q want EvilName", data.RealName)
	}
}

// TestLoginParseATCSanitizesRealName for #AA path.
func TestLoginParseATCSanitizesRealName(t *testing.T) {
	id := []byte("$IDTWR:SERVER:OPENFSD:0:1:1:1:1")
	// 7 fields: #AACS:SERVER:REALNAME:CID:TOKEN:RATING:PROTO
	add := []byte("#AATWR:SERVER:Bob\rAlice:200:tok:5:100\r\n")
	data, _, _, _, err := parseLoginPackets(id, add, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if data.RealName != "BobAlice" {
		t.Fatalf("RealName=%q", data.RealName)
	}
	if !data.IsAtc {
		t.Fatal("expected ATC")
	}
}

// TestBroadcastAddPacketUsesSanitizedName ensures join broadcast has no extra fields.
func TestBroadcastAddPacketUsesSanitizedName(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	// RealName already sanitized at login; force colon to verify broadcast uses as-is
	// after sanitize path (simulate post-sanitize value).
	p := newSess("N100", false, NetworkRatingObserver)
	p.RealName = "Safe Name"
	p.CID = 10
	peer := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{p, peer} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	srv.broadcastAddPacket(p)
	out := drain(peer)
	if !hasOutboundContaining(out, "#APN100:SERVER:10::1:101:1:Safe Name") {
		// ProtoRevision 101 from newSess
		found := false
		for _, o := range out {
			if strings.HasPrefix(o, "#APN100:") && strings.Contains(o, "Safe Name") && !strings.Contains(o, "Safe:Name") {
				found = true
			}
		}
		if !found {
			t.Fatalf("add packet missing sanitized name: %v", out)
		}
	}
}

// fakeUserStore supports auth attempt tests.
type fakeUserStore struct {
	byCID       map[int]*db.User
	verifyCalls int
	lastHash    string
}

func (f *fakeUserStore) GetUserByCID(cid int) (*db.User, error) {
	u, ok := f.byCID[cid]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return u, nil
}

func (f *fakeUserStore) VerifyPasswordHash(plaintext, hash string) bool {
	f.verifyCalls++
	f.lastHash = hash
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

func newAuthTestServer(t *testing.T, users UserStore, cfg *Config) *Server {
	t.Helper()
	if cfg == nil {
		cfg = &Config{FsdListenAddrs: []string{":0"}}
	}
	kv := &mapConfig{m: map[string]string{db.ConfigJwtSecretKey: TestJWTSecret}}
	srv, err := New(Deps{
		Config:   cfg,
		Users:    users,
		ConfigKV: kv,
		Registry: postoffice.New(),
		Metar:    &recordingMetar{},
		Clock:    fixedClock{t: time.Unix(1_700_000_000, 0)},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// discardConn implements net.Conn for login-phase WriteError sinks.
type discardConn struct {
	remote string
}

func (d *discardConn) Read([]byte) (int, error)    { return 0, errors.New("closed") }
func (d *discardConn) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardConn) Close() error                { return nil }
func (d *discardConn) LocalAddr() net.Addr         { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }
func (d *discardConn) RemoteAddr() net.Addr {
	if d.remote != "" {
		host, port, err := net.SplitHostPort(d.remote)
		if err == nil {
			return &net.TCPAddr{IP: net.ParseIP(host), Port: atoiPort(port)}
		}
	}
	return &net.TCPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 50000}
}
func (d *discardConn) SetDeadline(time.Time) error      { return nil }
func (d *discardConn) SetReadDeadline(time.Time) error  { return nil }
func (d *discardConn) SetWriteDeadline(time.Time) error { return nil }

func atoiPort(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func TestAttemptAuth_UnknownCIDStillVerifies(t *testing.T) {
	f := &fakeUserStore{byCID: map[int]*db.User{}}
	srv := newAuthTestServer(t, f, &Config{
		FsdListenAddrs: []string{":0"},
		AuthFailMax:    0,
	})
	client := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
		Callsign:      "N1",
		CID:           99999,
		NetworkRating: protocol.NetworkRatingObserver,
	})
	client.Auth = &auth.AuthState{}
	err := srv.attemptAuthentication(client, "guess")
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if f.verifyCalls != 1 {
		t.Fatalf("verifyCalls=%d want 1 (dummy bcrypt path)", f.verifyCalls)
	}
	if f.lastHash != dummyBcryptHash {
		t.Fatal("unknown CID should verify against dummy hash")
	}
}

func TestAttemptAuth_RateLimited(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeUserStore{byCID: map[int]*db.User{
		1: {CID: 1, Password: string(hash), NetworkRating: int(protocol.NetworkRatingObserver)},
	}}
	srv := newAuthTestServer(t, f, &Config{
		FsdListenAddrs: []string{":0"},
		AuthFailMax:    2,
		AuthFailWindow: time.Minute,
	})
	makeClient := func() *session.Session {
		c := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
			Callsign:      "N1",
			CID:           1,
			NetworkRating: protocol.NetworkRatingObserver,
		})
		c.Auth = &auth.AuthState{}
		c.SetRemoteIP("203.0.113.10")
		return c
	}
	for i := 0; i < 2; i++ {
		_ = srv.attemptAuthentication(makeClient(), "wrong")
	}
	callsBefore := f.verifyCalls
	err = srv.attemptAuthentication(makeClient(), "wrong")
	if err == nil {
		t.Fatal("expected rate limit failure")
	}
	if f.verifyCalls != callsBefore {
		t.Fatalf("rate limited attempt should not call VerifyPasswordHash (calls %d→%d)",
			callsBefore, f.verifyCalls)
	}
}

func TestAttemptAuth_SuccessPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeUserStore{byCID: map[int]*db.User{
		5: {CID: 5, Password: string(hash), NetworkRating: int(protocol.NetworkRatingController1)},
	}}
	srv := newAuthTestServer(t, f, nil)
	client := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
		Callsign:      "N5",
		CID:           5,
		NetworkRating: protocol.NetworkRatingObserver,
	})
	client.Auth = &auth.AuthState{}
	if err := srv.attemptAuthentication(client, "secret"); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if client.MaxNetworkRating != protocol.NetworkRatingController1 {
		t.Fatalf("MaxNetworkRating=%v", client.MaxNetworkRating)
	}
}

func TestAttemptAuth_JWT(t *testing.T) {
	srv := newAuthTestServer(t, &fakeUserStore{byCID: map[int]*db.User{}}, nil)
	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd",
		CID:           42,
		NetworkRating: protocol.NetworkRatingStudent1,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := tok.SignedString([]byte(TestJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	client := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
		Callsign:      "N42",
		CID:           42,
		NetworkRating: protocol.NetworkRatingObserver,
	})
	client.Auth = &auth.AuthState{}
	if err := srv.attemptAuthentication(client, signed); err != nil {
		t.Fatalf("jwt auth: %v", err)
	}
	// Wrong token type
	tok2, _ := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "access",
		CID:           42,
		NetworkRating: protocol.NetworkRatingStudent1,
	}, time.Minute)
	signed2, _ := tok2.SignedString([]byte(TestJWTSecret))
	client2 := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
		Callsign: "N42", CID: 42, NetworkRating: protocol.NetworkRatingObserver,
	})
	client2.Auth = &auth.AuthState{}
	if err := srv.attemptAuthentication(client2, signed2); err == nil {
		t.Fatal("access token must not work for FSD login")
	}
}

func TestAttemptAuth_RatingTooHigh(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeUserStore{byCID: map[int]*db.User{
		5: {CID: 5, Password: string(hash), NetworkRating: int(protocol.NetworkRatingObserver)},
	}}
	srv := newAuthTestServer(t, f, nil)
	client := session.New(context.Background(), &discardConn{}, nil, session.LoginData{
		Callsign:      "N5",
		CID:           5,
		NetworkRating: protocol.NetworkRatingSupervisor,
	})
	client.Auth = &auth.AuthState{}
	if err := srv.attemptAuthentication(client, "secret"); err == nil {
		t.Fatal("requested level too high should fail")
	}
}

// TestSessionCIDLimitIntegration registers via acquireCID path used at login.
func TestSessionCIDLimitIntegration(t *testing.T) {
	l := newConnLimits()
	const max = 5
	const cid = 77
	for i := 0; i < max; i++ {
		if !l.tryAcquireCID(cid, max) {
			t.Fatalf("session %d should succeed", i)
		}
	}
	if l.tryAcquireCID(cid, max) {
		t.Fatal("6th session for CID must fail")
	}
	l.releaseCID(cid)
	if !l.tryAcquireCID(cid, max) {
		t.Fatal("after release should succeed")
	}
}
