package platform

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter keeps one token bucket per key, in memory only. Buckets idle longer than the idle
// time are dropped.
type RateLimiter struct {
	perMinute int
	burst     int
	idle      time.Duration
	now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// NewRateLimiter refills perMinute tokens per minute per key, up to burst.
func NewRateLimiter(perMinute, burst int, idle time.Duration) *RateLimiter {
	return &RateLimiter{
		perMinute: perMinute,
		burst:     burst,
		idle:      idle,
		now:       time.Now,
		buckets:   map[string]*bucket{},
	}
}

// Allow takes one token from key's bucket and reports whether there was one.
func (l *RateLimiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(float64(l.perMinute)/60), l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.limiter.AllowN(now, 1)
}

// RetryAfter is how long a client waits for one new token, in whole seconds.
func (l *RateLimiter) RetryAfter() int {
	return int(math.Ceil(60 / float64(l.perMinute)))
}

// Sweep drops buckets idle longer than the idle time.
func (l *RateLimiter) Sweep() {
	cutoff := l.now().Add(-l.idle)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, b := range l.buckets {
		if b.seen.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Len is the number of buckets held.
func (l *RateLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// SweepEvery runs Sweep on every tick until ctx ends.
func (l *RateLimiter) SweepEvery(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Sweep()
		}
	}
}

// Middleware limits each client, keyed by RateKey of its ClientIP, and answers 429 with a
// Retry-After header over the limit. Paths in exempt pass untouched.
func (l *RateLimiter) Middleware(hops int, exempt []string, next http.Handler) http.Handler {
	skip := map[string]bool{}
	for _, p := range exempt {
		skip[p] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip[r.URL.Path] || l.Allow(RateKey(ClientIP(r, hops))) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", strconv.Itoa(l.RetryAfter()))
		WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "Too many requests from your address; try again in a minute.")
	})
}
