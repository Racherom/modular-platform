package httpkit

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		xff  []string
		hops int
		want string
	}{
		"no header":            {nil, 1, "10.0.0.9"},
		"one hop, last entry":  {[]string{"6.6.6.6, 1.2.3.4"}, 1, "1.2.3.4"},
		"two hops":             {[]string{"6.6.6.6, 1.2.3.4, 10.1.1.1"}, 2, "1.2.3.4"},
		"lines joined":         {[]string{"6.6.6.6", "1.2.3.4"}, 1, "1.2.3.4"},
		"too few entries":      {[]string{"1.2.3.4"}, 2, "10.0.0.9"},
		"not an ip":            {[]string{"1.2.3.4, unknown"}, 1, "10.0.0.9"},
		"ipv4 mapped unmapped": {[]string{"::ffff:1.2.3.4"}, 1, "1.2.3.4"},
		"zero hops uses peer":  {[]string{"1.2.3.4"}, 0, "10.0.0.9"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "10.0.0.9:5555"
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := ClientIP(r, tc.hops).String(); got != tc.want {
				t.Errorf("ClientIP = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRateKey(t *testing.T) {
	t.Parallel()
	a := RateKey(netip.MustParseAddr("2001:db8:1:2:aaaa::1"))
	b := RateKey(netip.MustParseAddr("2001:db8:1:2:bbbb::2"))
	c := RateKey(netip.MustParseAddr("2001:db8:1:3::1"))
	if a != b || a == c {
		t.Errorf("IPv6 keys: %s %s %s, want the same /64 for the first two only", a, b, c)
	}
	if got := RateKey(netip.MustParseAddr("1.2.3.4")); got != "1.2.3.4" {
		t.Errorf("IPv4 key = %s", got)
	}
}

func TestRateLimiterBurstAndSweep(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewRateLimiter(60, 10, 10*time.Minute, 10)
	l.now = func() time.Time { return now }
	for i := range 10 {
		if !l.Allow("k") {
			t.Fatalf("request %d refused inside the burst", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatal("request 11 allowed past the burst")
	}
	now = now.Add(time.Second)
	if !l.Allow("k") {
		t.Fatal("no token after one second at 60 per minute")
	}
	now = now.Add(11 * time.Minute)
	l.Sweep()
	if l.Len() != 0 {
		t.Errorf("idle bucket kept: %d buckets", l.Len())
	}
}

// testLimiter is a limiter for at most maxKeys keys, burst 10 at 60 a minute, 10 minute idle, on a
// fake clock that the test moves forward through the returned pointer.
func testLimiter(maxKeys int) (*RateLimiter, *time.Time) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewRateLimiter(60, 10, 10*time.Minute, maxKeys)
	l.now = func() time.Time { return now }
	return l, &now
}

func (l *RateLimiter) holds(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets[key]
	return ok
}

// TestRateLimiterCap covers spec 0003 AC-1.
func TestRateLimiterCap(t *testing.T) {
	t.Parallel()
	l, _ := testLimiter(3)
	for i := range 10 {
		l.Allow(fmt.Sprintf("k%d", i))
	}
	if l.Len() != 3 {
		t.Errorf("%d buckets after 10 keys, want the cap of 3", l.Len())
	}
}

// TestRateLimiterFullAllowsNewKey covers spec 0003 AC-2.
func TestRateLimiterFullAllowsNewKey(t *testing.T) {
	t.Parallel()
	l, _ := testLimiter(3)
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	for i := range 10 {
		if !l.Allow("new") {
			t.Fatalf("request %d from a new key refused at a full map", i+1)
		}
	}
	if l.Allow("new") {
		t.Error("request 11 from the new key allowed past its burst")
	}
}

// TestRateLimiterEvictsLeastRecentlySeen covers spec 0003 AC-3.
func TestRateLimiterEvictsLeastRecentlySeen(t *testing.T) {
	t.Parallel()
	l, now := testLimiter(3)
	for _, k := range []string{"k1", "k2", "k3"} {
		l.Allow(k)
		*now = now.Add(time.Second)
	}
	l.Allow("k1")
	l.Allow("k4")
	if l.holds("k2") {
		t.Error("k2, the least recently seen, was kept")
	}
	for _, k := range []string{"k1", "k3", "k4"} {
		if !l.holds(k) {
			t.Errorf("%s was evicted", k)
		}
	}
}

// TestRateLimiterEvictedClientStartsFresh covers spec 0003 AC-4.
func TestRateLimiterEvictedClientStartsFresh(t *testing.T) {
	t.Parallel()
	l, _ := testLimiter(3)
	for range 10 {
		l.Allow("a")
	}
	if l.Allow("a") {
		t.Fatal("request 11 allowed past the burst")
	}
	for _, k := range []string{"b", "c", "d"} {
		l.Allow(k)
	}
	if l.holds("a") {
		t.Fatal("a was not evicted")
	}
	for i := range 10 {
		if !l.Allow("a") {
			t.Fatalf("request %d refused after eviction, want a fresh burst", i+1)
		}
	}
}

// TestRateLimiterSweepIdle covers spec 0003 AC-5.
func TestRateLimiterSweepIdle(t *testing.T) {
	t.Parallel()
	l, now := testLimiter(10)
	l.Allow("old")
	*now = now.Add(time.Nanosecond)
	l.Allow("edge")
	*now = now.Add(5 * time.Minute)
	l.Allow("recent")
	l.Allow("old2") // seen after recent, but kept below: the sweep stops at the first recent one
	*now = now.Add(5 * time.Minute)
	// old is idle 10 minutes and 1 ns, edge exactly 10 minutes
	l.Sweep()
	if l.holds("old") {
		t.Error("a bucket idle longer than 10 minutes was kept")
	}
	for _, k := range []string{"edge", "recent", "old2"} {
		if !l.holds(k) {
			t.Errorf("%s was dropped", k)
		}
	}
	*now = now.Add(time.Nanosecond)
	l.Sweep()
	if l.holds("edge") {
		t.Error("edge kept once idle longer than 10 minutes")
	}
	if l.Len() != 2 {
		t.Errorf("%d buckets, want recent and old2", l.Len())
	}
}

// TestRateLimiterSweepCounts covers the counts behind spec 0003 AC-6.
func TestRateLimiterSweepCounts(t *testing.T) {
	t.Parallel()
	l, now := testLimiter(3)
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	*now = now.Add(11 * time.Minute)
	l.Allow("d") // evicts a
	l.Allow("e") // evicts b; c is now idle
	evicted, buckets := l.Sweep()
	if evicted != 2 {
		t.Errorf("evicted %d, want 2 (idle drops are not counted)", evicted)
	}
	if buckets != 2 || buckets != l.Len() {
		t.Errorf("buckets %d, Len %d, want 2", buckets, l.Len())
	}
	if evicted, _ := l.Sweep(); evicted != 0 {
		t.Errorf("second sweep evicted %d, want 0", evicted)
	}
}

// TestRateLimiterWarnsOncePerInterval covers spec 0003 AC-6.
func TestRateLimiterWarnsOncePerInterval(t *testing.T) {
	t.Parallel()
	l, _ := testLimiter(2)
	var logs bytes.Buffer
	ticks := make(chan time.Time)
	swept := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.sweepTicks(ctx, ticks, slog.New(slog.NewJSONHandler(&logs, nil)), func() { swept <- struct{}{} })
	}()
	// tick hands one tick over and waits until its sweep has finished, so no Allow below races
	// with a sweep
	tick := func() {
		ticks <- time.Time{}
		<-swept
	}

	keys := []string{"client-a", "client-b", "client-c", "client-d", "client-e", "client-f"}
	for _, k := range keys[:4] {
		l.Allow(k) // two evictions
	}
	tick()
	tick()           // quiet
	l.Allow(keys[4]) // one eviction
	tick()
	l.Allow(keys[5]) // after the last tick: not logged
	cancel()
	<-done

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d log lines, want one per interval with evictions:\n%s", len(lines), logs.String())
	}
	for i, want := range []int{2, 1} {
		var got struct {
			Level   string
			Msg     string
			Evicted int
			Buckets int
			Max     int
		}
		if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
			t.Fatal(err)
		}
		if got.Level != "WARN" || got.Msg != "rate limiter full" || got.Evicted != want || got.Buckets != 2 || got.Max != 2 {
			t.Errorf("line %d = %+v, want WARN rate limiter full, evicted %d, buckets 2, max 2", i+1, got, want)
		}
	}
	for _, k := range keys {
		if strings.Contains(logs.String(), k) {
			t.Errorf("key %s reached the log", k)
		}
	}
}

// TestNewRateLimiterNeedsMaxKeys covers the guard behind spec 0003 AC-1.
func TestNewRateLimiterNeedsMaxKeys(t *testing.T) {
	t.Parallel()
	defer func() {
		if got := recover(); got != "httpkit: NewRateLimiter maxKeys must be at least 1" {
			t.Errorf("panic %v", got)
		}
	}()
	NewRateLimiter(60, 10, time.Minute, 0)
}

// TestRateLimiterConcurrent covers spec 0003 AC-7 under -race.
func TestRateLimiterConcurrent(t *testing.T) {
	t.Parallel()
	l := NewRateLimiter(60, 10, time.Millisecond, 50)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 500 {
				l.Allow(fmt.Sprintf("%d-%d", g, i%80))
			}
		})
	}
	wg.Go(func() {
		for range 200 {
			l.Sweep()
		}
	})
	wg.Wait()
	if l.Len() > 50 {
		t.Errorf("%d buckets, over the cap of 50", l.Len())
	}
}

// freeAddr returns a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// TestHealthcheck covers the exit code of the healthcheck subcommand: 0 for a 2xx answer, 1 for
// any other status, a refused connection, an untrusted certificate, or a url no request can be
// built from.
func TestHealthcheck(t *testing.T) {
	t.Parallel()
	status := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusNoContent)
		case "/down":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	plain := httptest.NewServer(status)
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(status)
	t.Cleanup(secure.Close)
	trusted := secure.Client().Transport.(*http.Transport).TLSClientConfig

	tests := map[string]struct {
		url  string
		cfg  *tls.Config
		want int
	}{
		"2xx answer":          {plain.URL + "/ok", nil, 0},
		"5xx answer":          {plain.URL + "/down", nil, 1},
		"4xx answer":          {plain.URL + "/missing", nil, 1},
		"connection refused":  {"http://" + freeAddr(t) + "/healthz", nil, 1},
		"malformed url":       {"http://[::1/healthz", nil, 1},
		"https, trusted":      {secure.URL + "/ok", trusted, 0},
		"https, no tls given": {secure.URL + "/ok", nil, 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := Healthcheck(tc.url, tc.cfg); got != tc.want {
				t.Errorf("Healthcheck(%s) = %d, want %d", tc.url, got, tc.want)
			}
		})
	}
}

// TestServe covers Serve on a free address: it answers with handler until ctx ends, then shuts
// down and returns nil.
func TestServe(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, slog.New(slog.DiscardHandler), addr, handler) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusTeapot {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Serve never answered on %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("Serve after ctx ended = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after ctx ended")
	}
}

// TestServeAddrInUse covers Serve on an address another listener holds: it returns a serve error
// at once instead of serving.
func TestServeAddrInUse(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	err = Serve(t.Context(), slog.New(slog.DiscardHandler), held.Addr().String(), http.NotFoundHandler())

	if err == nil || !strings.HasPrefix(err.Error(), "serve: ") {
		t.Errorf("Serve on a held address = %v, want a serve: error", err)
	}
}

func TestBasePath(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"", "/platform", "/a/b"} {
		if _, err := BasePath(ok); err != nil {
			t.Errorf("BasePath(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"platform", "/platform/", "/", "//x"} {
		if _, err := BasePath(bad); err == nil {
			t.Errorf("BasePath(%q) accepted", bad)
		}
	}
}
