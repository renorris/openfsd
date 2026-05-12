package main

import (
	"sync"
	"time"
)

const (
	loginMaxAttempts  = 10
	loginWindowPeriod = time.Minute
)

type loginRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
}

type loginBucket struct {
	count   int
	resetAt time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{buckets: make(map[string]*loginBucket)}
}

// allow returns true if the IP is within the rate limit, false if it should be blocked.
func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	bucket, ok := l.buckets[ip]
	if !ok || now.After(bucket.resetAt) {
		l.buckets[ip] = &loginBucket{count: 1, resetAt: now.Add(loginWindowPeriod)}
		return true
	}

	if bucket.count >= loginMaxAttempts {
		return false
	}

	bucket.count++
	return true
}
