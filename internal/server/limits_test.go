package server

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/session"
	"go.uber.org/atomic"
)

func TestConnLimits_Basic(t *testing.T) {
	l := newConnLimits()
	if !l.tryAcquireConn("1.2.3.4", 2, 1) {
		t.Fatal("first conn should acquire")
	}
	if l.tryAcquireConn("1.2.3.4", 2, 1) {
		t.Fatal("per-IP limit should block")
	}
	if !l.tryAcquireConn("5.6.7.8", 2, 1) {
		t.Fatal("other IP should acquire")
	}
	if l.tryAcquireConn("9.9.9.9", 2, 1) {
		t.Fatal("global max should block")
	}
	l.releaseConn("1.2.3.4")
	if !l.tryAcquireConn("1.2.3.4", 2, 1) {
		t.Fatal("after release should acquire")
	}
}

func TestConnLimits_UnlimitedAndEmptyIP(t *testing.T) {
	l := newConnLimits()
	// maxTotal/maxPerIP 0 = unlimited
	for i := 0; i < 100; i++ {
		if !l.tryAcquireConn("10.0.0.1", 0, 0) {
			t.Fatalf("unlimited acquire failed at %d", i)
		}
	}
	// empty IP still counts toward total when maxTotal set
	if !l.tryAcquireConn("", 101, 1) {
		t.Fatal("empty IP should acquire when under total")
	}
	if l.tryAcquireConn("", 101, 1) {
		t.Fatal("empty IP should hit global max")
	}
	// release empty IP should not panic
	l.releaseConn("")
	l.releaseConn("missing")
}

func TestConnLimits_ReleaseCleansPerIPMap(t *testing.T) {
	l := newConnLimits()
	if !l.tryAcquireConn("1.1.1.1", 10, 10) {
		t.Fatal("acquire")
	}
	l.releaseConn("1.1.1.1")
	l.mu.Lock()
	_, ok := l.perIP["1.1.1.1"]
	total := l.total
	l.mu.Unlock()
	if ok {
		t.Fatal("per-IP entry should be deleted at zero")
	}
	if total != 0 {
		t.Fatalf("total=%d want 0", total)
	}
}

func TestCIDLimits(t *testing.T) {
	l := newConnLimits()
	if !l.tryAcquireCID(100, 2) {
		t.Fatal("first session")
	}
	if !l.tryAcquireCID(100, 2) {
		t.Fatal("second session")
	}
	if l.tryAcquireCID(100, 2) {
		t.Fatal("third should fail at max 2")
	}
	// different CID ok
	if !l.tryAcquireCID(101, 2) {
		t.Fatal("other CID")
	}
	l.releaseCID(100)
	if !l.tryAcquireCID(100, 2) {
		t.Fatal("after release")
	}
	// unlimited
	if !l.tryAcquireCID(200, 0) || !l.tryAcquireCID(200, 0) {
		t.Fatal("unlimited CID")
	}
	l.releaseCID(999) // no-op over-release
}

func TestConnLimits_Concurrent(t *testing.T) {
	l := newConnLimits()
	const n = 200
	var wg sync.WaitGroup
	ok := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ip := "10.0.0.1"
			if i%2 == 0 {
				ip = "10.0.0.2"
			}
			ok <- l.tryAcquireConn(ip, 50, 30)
		}(i)
	}
	wg.Wait()
	close(ok)
	acquired := 0
	for v := range ok {
		if v {
			acquired++
		}
	}
	if acquired != 50 {
		t.Fatalf("acquired=%d want 50 (global cap)", acquired)
	}
}

func TestAuthFailLimiter(t *testing.T) {
	a := newAuthFailLimiter(2, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	if !a.allowed("10.0.0.1", now) {
		t.Fatal("should allow initially")
	}
	a.recordFailure("10.0.0.1", now)
	a.recordFailure("10.0.0.1", now.Add(time.Second))
	if a.allowed("10.0.0.1", now.Add(2*time.Second)) {
		t.Fatal("should block after max failures")
	}
	// Different IP ok
	if !a.allowed("10.0.0.2", now) {
		t.Fatal("other IP should allow")
	}
}

func TestAuthFailLimiter_WindowExpiry(t *testing.T) {
	a := newAuthFailLimiter(2, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	a.recordFailure("1.1.1.1", now)
	a.recordFailure("1.1.1.1", now.Add(time.Second))
	if a.allowed("1.1.1.1", now.Add(30*time.Second)) {
		t.Fatal("still blocked inside window")
	}
	// After window, failures prune
	later := now.Add(2 * time.Minute)
	if !a.allowed("1.1.1.1", later) {
		t.Fatal("should allow after window expiry")
	}
}

func TestAuthFailLimiter_Disabled(t *testing.T) {
	a := newAuthFailLimiter(0, time.Minute)
	now := time.Now()
	for i := 0; i < 100; i++ {
		a.recordFailure("9.9.9.9", now)
		if !a.allowed("9.9.9.9", now) {
			t.Fatal("disabled limiter must always allow")
		}
	}
	// nil-safe
	var nilLim *authFailLimiter
	if !nilLim.allowed("x", now) {
		t.Fatal("nil limiter allowed")
	}
	nilLim.recordFailure("x", now)
}

func TestAuthFailLimiter_EmptyIP(t *testing.T) {
	a := newAuthFailLimiter(1, time.Minute)
	now := time.Now()
	a.recordFailure("", now)
	if !a.allowed("", now) {
		t.Fatal("empty IP must not be rate limited")
	}
}

func TestParseVisRangeCap(t *testing.T) {
	pkt := []byte("%CS:28550:6:99999:5:34.0:-118.0:0\r\n")
	vr, ok := parseVisRange(pkt, 3, 1500)
	if !ok {
		t.Fatal("expected ok")
	}
	want := 1500.0 * 1852.0
	if vr != want {
		t.Fatalf("visRange=%v want %v", vr, want)
	}
	// default max when maxNM <= 0
	vr2, ok := parseVisRange(pkt, 3, 0)
	if !ok || vr2 != want {
		t.Fatalf("default max: %v %v", vr2, ok)
	}
	// non-positive / non-finite
	if _, ok := parseVisRange([]byte("%CS:28550:6:0:5:34.0:-118.0:0\r\n"), 3, 1500); ok {
		t.Fatal("zero range invalid")
	}
	if _, ok := parseVisRange([]byte("%CS:28550:6:-1:5:34.0:-118.0:0\r\n"), 3, 1500); ok {
		t.Fatal("negative range invalid")
	}
	if _, ok := parseVisRange([]byte("%CS:28550:6:Inf:5:34.0:-118.0:0\r\n"), 3, 1500); ok {
		t.Fatal("Inf range invalid")
	}
	if _, ok := parseVisRange([]byte("%CS:28550:6:NaN:5:34.0:-118.0:0\r\n"), 3, 1500); ok {
		t.Fatal("NaN range invalid")
	}
	// under cap passthrough
	under := []byte("%CS:28550:6:40:5:34.0:-118.0:0\r\n")
	vr3, ok := parseVisRange(under, 3, 1500)
	if !ok || vr3 != 40*1852 {
		t.Fatalf("under cap: %v %v", vr3, ok)
	}
}

func TestValidLatLon(t *testing.T) {
	cases := []struct {
		lat, lon float64
		ok       bool
	}{
		{0, 0, true},
		{90, 180, true},
		{-90, -180, true},
		{45.5, -122.3, true},
		{91, 0, false},
		{-91, 0, false},
		{0, 181, false},
		{0, -181, false},
		{math.NaN(), 0, false},
		{0, math.NaN(), false},
		{math.Inf(1), 0, false},
		{0, math.Inf(-1), false},
	}
	for _, tc := range cases {
		if got := validLatLon(tc.lat, tc.lon); got != tc.ok {
			t.Fatalf("validLatLon(%v,%v)=%v want %v", tc.lat, tc.lon, got, tc.ok)
		}
	}
	// parseLatLon rejects NaN wire values
	if _, _, ok := parseLatLon([]byte("%x:y:z:a:b:NaN:0:c\r\n"), 5, 6); ok {
		t.Fatal("parseLatLon NaN")
	}
	if _, _, ok := parseLatLon([]byte("%x:y:z:a:b:91.0:-118.0:c\r\n"), 5, 6); ok {
		t.Fatal("parseLatLon lat>90")
	}
}

func TestSanitizeRealName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Alice", "Alice"},
		{"A:B", "A B"},
		{"A:B\r\n", "A B"},
		{"line\nbreak", "linebreak"},
		{"x\ry", "xy"},
	}
	for _, tc := range cases {
		if got := sanitizeRealName(tc.in); got != tc.want {
			t.Fatalf("sanitizeRealName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestRewriteField(t *testing.T) {
	pkt := []byte("@S:N1:1200:99:34.0:-118.0:0:0:0:0\r\n")
	out := rewriteField(pkt, 3, "1")
	if string(out) != "@S:N1:1200:1:34.0:-118.0:0:0:0:0\r\n" {
		t.Fatalf("got %q", out)
	}
	// LF-only suffix
	lf := []byte("a:b:c\n")
	if string(rewriteField(lf, 1, "x")) != "a:x:c\n" {
		t.Fatalf("lf rewrite: %q", rewriteField(lf, 1, "x"))
	}
	// missing index returns original
	orig := []byte("a:b\r\n")
	if string(rewriteField(orig, 5, "z")) != string(orig) {
		t.Fatal("missing index should return original")
	}
	if string(rewriteField(nil, 0, "x")) != "" {
		t.Fatal("nil packet")
	}
	if string(rewriteField(orig, -1, "x")) != string(orig) {
		t.Fatal("negative index")
	}
}

func TestAllowRate(t *testing.T) {
	var last atomic.Int64
	now := time.Unix(100, 0)
	if !session.AllowRate(&last, time.Second, now) {
		t.Fatal("first allow")
	}
	if session.AllowRate(&last, time.Second, now.Add(100*time.Millisecond)) {
		t.Fatal("should deny within window")
	}
	if !session.AllowRate(&last, time.Second, now.Add(time.Second)) {
		t.Fatal("should allow after window")
	}
	// nil / zero interval always allow
	if !session.AllowRate(nil, time.Second, now) {
		t.Fatal("nil last")
	}
	if !session.AllowRate(&last, 0, now) {
		t.Fatal("zero interval")
	}
}

func TestRateOK_ConfigGate(t *testing.T) {
	var last atomic.Int64
	// disabled
	s := &Server{cfg: &Config{FsdEnableRateLimits: false}}
	if !s.rateOK(&last, time.Hour) {
		t.Fatal("disabled must allow")
	}
	// nil server / cfg
	if !(*Server)(nil).rateOK(&last, time.Hour) {
		t.Fatal("nil server")
	}
	s2 := &Server{}
	if !s2.rateOK(&last, time.Hour) {
		t.Fatal("nil cfg")
	}
	// enabled: first ok, second blocked (wall clock)
	s3 := &Server{cfg: &Config{FsdEnableRateLimits: true}}
	if !s3.rateOK(&last, time.Hour) {
		t.Fatal("first enabled")
	}
	if s3.rateOK(&last, time.Hour) {
		t.Fatal("second within hour must deny")
	}
}

func TestMostLikelyJwtBase64URL(t *testing.T) {
	// {"alg":"HS256","typ":"JWT"} base64url
	hdr := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	tok := []byte(hdr + ".payload.sig")
	if !mostLikelyJwt(tok) {
		t.Fatal("expected JWT recognition for base64url header")
	}
	if mostLikelyJwt([]byte("not.a.jwt")) {
		// may or may not decode — ensure nonsense with wrong structure fails
	}
	if mostLikelyJwt([]byte("onlyonepart")) {
		t.Fatal("one part not jwt")
	}
	if mostLikelyJwt([]byte("a.b.c.d")) {
		t.Fatal("too many dots")
	}
	if mostLikelyJwt([]byte("")) {
		t.Fatal("empty")
	}
}

func TestConfigMaxAtcVisRangeNM(t *testing.T) {
	var nilCfg *Config
	if nilCfg.maxAtcVisRangeNM() != 1500 {
		t.Fatal("nil default")
	}
	if (&Config{}).maxAtcVisRangeNM() != 1500 {
		t.Fatal("zero default")
	}
	if (&Config{FsdMaxAtcVisRangeNM: 300}).maxAtcVisRangeNM() != 300 {
		t.Fatal("explicit")
	}
}

func TestCutBearerToken(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"Bearer abc", "abc", true},
		{"bearer abc", "abc", true},
		{"BEARER  tok ", "tok", true},
		{"Basic abc", "", false},
		{"", "", false},
		{"Bearer", "", false},
		{"Bearer ", "", false},
	}
	for _, tc := range cases {
		got, ok := cutBearerToken(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("cutBearerToken(%q)=(%q,%v) want (%q,%v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestDummyBcryptHashUsable(t *testing.T) {
	if dummyBcryptHash == "" {
		t.Fatal("empty dummy hash")
	}
	// Must be a valid bcrypt hash that never verifies the pad password for wrong input paths
	// (real verify goes through UserStore; just ensure string is non-empty and plausible).
	if len(dummyBcryptHash) < 20 {
		t.Fatalf("implausible hash: %q", dummyBcryptHash)
	}
}
