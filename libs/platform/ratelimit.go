package platform

import (
	"container/list"
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter keeps one token bucket per key, in memory only. It holds at most maxKeys buckets: a
// new key at a full map takes the place of the least recently seen one (spec 0003). Buckets idle
// longer than the idle time are dropped by Sweep.
type RateLimiter struct {
	perMinute int
	burst     int
	idle      time.Duration
	maxKeys   int
	now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*list.Element
	order   *list.List // of *bucket; front is the most recently seen
	evicted int        // evictions forced by the cap since the last Sweep
}

type bucket struct {
	key     string
	limiter *rate.Limiter
	seen    time.Time
}

// NewRateLimiter refills perMinute tokens per minute per key, up to burst, for at most maxKeys keys.
func NewRateLimiter(perMinute, burst int, idle time.Duration, maxKeys int) *RateLimiter {
	if maxKeys < 1 {
		panic("platform: NewRateLimiter maxKeys must be at least 1")
	}
	return &RateLimiter{
		perMinute: perMinute,
		burst:     burst,
		idle:      idle,
		maxKeys:   maxKeys,
		now:       time.Now,
		buckets:   map[string]*list.Element{},
		order:     list.New(),
	}
}

// Allow takes one token from key's bucket and reports whether there was one. A full map never
// refuses: the least recently seen bucket makes room and the new key starts with a full burst.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// read the clock under the lock, so the list stays in last seen order
	now := l.now()
	e, ok := l.buckets[key]
	if ok {
		l.order.MoveToFront(e)
	} else {
		if l.order.Len() >= l.maxKeys {
			oldest := l.order.Back()
			l.order.Remove(oldest)
			delete(l.buckets, oldest.Value.(*bucket).key)
			l.evicted++
		}
		e = l.order.PushFront(&bucket{key: key, limiter: rate.NewLimiter(rate.Limit(float64(l.perMinute)/60), l.burst)})
		l.buckets[key] = e
	}
	b := e.Value.(*bucket)
	b.seen = now
	return b.limiter.AllowN(now, 1)
}

// RetryAfter is how long a client waits for one new token, in whole seconds.
func (l *RateLimiter) RetryAfter() int {
	return int(math.Ceil(60 / float64(l.perMinute)))
}

// Sweep drops buckets idle longer than the idle time, walking from the least recently seen and
// stopping at the first that is not idle. It returns the evictions the cap forced since the
// previous Sweep (idle drops are not counted) and the buckets left, and resets the count.
func (l *RateLimiter) Sweep() (evicted, buckets int) {
	cutoff := l.now().Add(-l.idle)
	l.mu.Lock()
	defer l.mu.Unlock()
	for e := l.order.Back(); e != nil; e = l.order.Back() {
		b := e.Value.(*bucket)
		if !b.seen.Before(cutoff) {
			break
		}
		l.order.Remove(e)
		delete(l.buckets, b.key)
	}
	evicted, l.evicted = l.evicted, 0
	return evicted, l.order.Len()
}

// Len is the number of buckets held.
func (l *RateLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// SweepEvery runs Sweep every interval until ctx ends, and logs one warning per interval in which
// the cap forced evictions.
func (l *RateLimiter) SweepEvery(ctx context.Context, every time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	l.sweepTicks(ctx, ticker.C, log)
}

// sweepTicks is SweepEvery with the ticks supplied, so tests can drive it. Evictions after the
// last tick are not logged.
func (l *RateLimiter) sweepTicks(ctx context.Context, ticks <-chan time.Time, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			evicted, buckets := l.Sweep()
			if evicted > 0 {
				// counts only: no client address or key is ever logged
				log.LogAttrs(ctx, slog.LevelWarn, "rate limiter full",
					slog.Int("evicted", evicted),
					slog.Int("buckets", buckets),
					slog.Int("max", l.maxKeys),
				)
			}
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
