package platform

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
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
	l := NewRateLimiter(60, 10, 10*time.Minute)
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
