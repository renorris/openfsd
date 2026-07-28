package afv

import (
	"sync"
	"time"
)

// authFailLimiter rate-limits failed password auths per remote IP.
type authFailLimiter struct {
	mu     sync.Mutex
	fails  map[string][]time.Time
	max    int
	window time.Duration
}

func newAuthFailLimiter(max int, window time.Duration) *authFailLimiter {
	return &authFailLimiter{
		fails:  make(map[string][]time.Time),
		max:    max,
		window: window,
	}
}

// Allow reports whether a new attempt is permitted. Call only for failed auths
// via Record; Allow checks current count without recording.
func (a *authFailLimiter) Allow(ip string, now time.Time) bool {
	if a == nil || a.max <= 0 {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked(ip, now)
	return len(a.fails[ip]) < a.max
}

// Record adds a failure timestamp for ip.
func (a *authFailLimiter) Record(ip string, now time.Time) {
	if a == nil || a.max <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked(ip, now)
	a.fails[ip] = append(a.fails[ip], now)
}

func (a *authFailLimiter) pruneLocked(ip string, now time.Time) {
	cut := now.Add(-a.window)
	v := a.fails[ip]
	j := 0
	for _, t := range v {
		if t.After(cut) {
			v[j] = t
			j++
		}
	}
	if j == 0 {
		delete(a.fails, ip)
	} else {
		a.fails[ip] = v[:j]
	}
}
