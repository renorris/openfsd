package server

import (
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/session"
	"go.uber.org/atomic"
	"golang.org/x/crypto/bcrypt"
)

// connLimits tracks concurrent TCP connections (pre- and post-login) and
// registered sessions per CID.
type connLimits struct {
	mu     sync.Mutex
	total  int
	perIP  map[string]int
	perCID map[int]int
}

func newConnLimits() *connLimits {
	return &connLimits{
		perIP:  make(map[string]int),
		perCID: make(map[int]int),
	}
}

// tryAcquireConn reserves a connection slot for ip. maxTotal/maxPerIP 0 = unlimited.
func (l *connLimits) tryAcquireConn(ip string, maxTotal, maxPerIP int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxTotal > 0 && l.total >= maxTotal {
		return false
	}
	if maxPerIP > 0 && ip != "" && l.perIP[ip] >= maxPerIP {
		return false
	}
	l.total++
	if ip != "" {
		l.perIP[ip]++
	}
	return true
}

func (l *connLimits) releaseConn(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total > 0 {
		l.total--
	}
	if ip != "" {
		n := l.perIP[ip] - 1
		if n <= 0 {
			delete(l.perIP, ip)
		} else {
			l.perIP[ip] = n
		}
	}
}

// tryAcquireCID reserves a registered session for cid. maxPerCID 0 = unlimited.
func (l *connLimits) tryAcquireCID(cid int, maxPerCID int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxPerCID > 0 && l.perCID[cid] >= maxPerCID {
		return false
	}
	l.perCID[cid]++
	return true
}

func (l *connLimits) releaseCID(cid int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.perCID[cid] - 1
	if n <= 0 {
		delete(l.perCID, cid)
	} else {
		l.perCID[cid] = n
	}
}

// authFailLimiter rate-limits failed authentication attempts per IP.
type authFailLimiter struct {
	mu      sync.Mutex
	fails   map[string][]time.Time
	max     int
	window  time.Duration
	cleanup time.Time
}

func newAuthFailLimiter(max int, window time.Duration) *authFailLimiter {
	if max <= 0 || window <= 0 {
		return &authFailLimiter{max: 0}
	}
	return &authFailLimiter{
		fails:  make(map[string][]time.Time),
		max:    max,
		window: window,
	}
}

// allowed reports whether an auth attempt from ip is currently permitted.
func (a *authFailLimiter) allowed(ip string, now time.Time) bool {
	if a == nil || a.max <= 0 || ip == "" {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked(ip, now)
	return len(a.fails[ip]) < a.max
}

// recordFailure records a failed auth attempt for ip.
func (a *authFailLimiter) recordFailure(ip string, now time.Time) {
	if a == nil || a.max <= 0 || ip == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked(ip, now)
	a.fails[ip] = append(a.fails[ip], now)
}

func (a *authFailLimiter) pruneLocked(ip string, now time.Time) {
	cut := now.Add(-a.window)
	ts := a.fails[ip]
	i := 0
	for i < len(ts) && !ts[i].After(cut) {
		i++
	}
	if i > 0 {
		ts = append([]time.Time(nil), ts[i:]...)
	}
	if len(ts) == 0 {
		delete(a.fails, ip)
	} else {
		a.fails[ip] = ts
	}
	// Occasional global cleanup.
	if now.After(a.cleanup) {
		a.cleanup = now.Add(a.window)
		for k, v := range a.fails {
			j := 0
			for j < len(v) && !v[j].After(cut) {
				j++
			}
			if j >= len(v) {
				delete(a.fails, k)
			} else if j > 0 {
				a.fails[k] = append([]time.Time(nil), v[j:]...)
			}
		}
	}
}

// dummyBcryptHash is a precomputed bcrypt hash used to equalize login timing
// when the CID does not exist (avoids cheap unknown-CID oracle).
var dummyBcryptHash = mustDummyBcrypt()

func mustDummyBcrypt() string {
	h, err := bcrypt.GenerateFromPassword([]byte("openfsd-timing-pad"), bcrypt.DefaultCost)
	if err != nil {
		// Fallback never used for real auth; CompareHashAndPassword will simply fail.
		return "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.rOqP1qP1qP1qP1qP1q"
	}
	return string(h)
}

// Per-session minimum intervals (abuse control).
const (
	minPositionInterval = 50 * time.Millisecond  // ~20 Hz
	minTextInterval     = 100 * time.Millisecond // 10 Hz
	minMetarInterval    = 5 * time.Second
	minFPLInterval      = 2 * time.Second
	minWallopInterval   = 10 * time.Second
	minQueryInterval    = 100 * time.Millisecond
)

// rateOK reports whether the session may emit another packet of the given class.
// Disabled when FsdEnableRateLimits is false (hand-built test configs).
// Uses wall clock so fixed test clocks do not freeze all rate windows.
func (s *Server) rateOK(lastNs *atomic.Int64, minInterval time.Duration) bool {
	if s == nil || s.cfg == nil || !s.cfg.FsdEnableRateLimits {
		return true
	}
	return session.AllowRate(lastNs, minInterval, time.Now())
}
