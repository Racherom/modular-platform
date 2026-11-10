package tlsacme_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/racherom/modular-platform/sdk/internal/testpki"
	"github.com/racherom/modular-platform/sdk/tlsacme"
)

const host = "tm.sdk-test.internal"

// issuer is a fake Issuer: issue picks each call's result (calls counted from 0), and every call's
// time, name and end are recorded. Challenge answers with challengeCert and counts its calls.
type issuer struct {
	issue func(ctx context.Context, call int) (*tls.Certificate, error)

	mu         sync.Mutex
	calls      []time.Time
	names      []string
	ended      []time.Time
	challenges int
}

var challengeCert = &tls.Certificate{}

func (f *issuer) Issue(ctx context.Context, name string) (*tls.Certificate, error) {
	f.mu.Lock()
	n := len(f.calls)
	f.calls = append(f.calls, time.Now())
	f.names = append(f.names, name)
	f.mu.Unlock()
	c, err := f.issue(ctx, n)
	f.mu.Lock()
	f.ended = append(f.ended, time.Now())
	f.mu.Unlock()
	return c, err
}

func (f *issuer) Challenge(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.challenges++
	return challengeCert, nil
}

// gaps returns the time between each call and the one before it.
func (f *issuer) gaps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []time.Duration
	for i := 1; i < len(f.calls); i++ {
		out = append(out, f.calls[i].Sub(f.calls[i-1]))
	}
	return out
}

func (f *issuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var errDown = errors.New("the issuer is down")

// syncBuffer is a log sink safe for the manager's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newManager returns a manager for host trusting ca, logging into the returned buffer.
func newManager(f tlsacme.Issuer, ca *testpki.CA) (*tlsacme.Manager, *syncBuffer) {
	var logs syncBuffer
	return tlsacme.New(f, host, ca.Pool(), slog.New(slog.NewJSONHandler(&logs, nil))), &logs
}

// run starts m.Run and stops it when the test ends.
func run(t *testing.T, m *tlsacme.Manager) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// served returns the certificate a normal handshake for host gets, or nil.
func served(m *tlsacme.Manager) *tls.Certificate {
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: host, SupportedProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		return nil
	}
	return c
}

// TestServerConfig covers the server's TLS settings (AC-8): TLS 1.3 only, and acme-tls/1 among
// the protocols so a tls-alpn-01 validation reaches the issuer.
func TestServerConfig(t *testing.T) {
	t.Parallel()
	m, _ := newManager(&issuer{}, testpki.New(t))
	cfg := m.ServerConfig()
	if cfg.MinVersion != tls.VersionTLS13 || !slices.Equal(cfg.NextProtos, []string{"h2", "http/1.1", acme.ALPNProto}) || cfg.GetCertificate == nil {
		t.Errorf("ServerConfig = min %x, protos %v", cfg.MinVersion, cfg.NextProtos)
	}
}

// TestChallengeRouting covers which hello gets the validation answer (AC-8): only a hello that
// offers acme-tls/1 and nothing else reaches the issuer's Challenge; one with a second protocol
// gets the held certificate, whatever its server name, and none before one is held.
func TestChallengeRouting(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		cert := ca.Leaf(t, 24*time.Hour, host)
		f := &issuer{issue: func(context.Context, int) (*tls.Certificate, error) { return cert, nil }}
		m, _ := newManager(f, ca)

		if c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: host}); err == nil || c != nil {
			t.Error("a certificate served before one is held")
		}
		if c, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: host, SupportedProtos: []string{acme.ALPNProto}}); c != challengeCert {
			t.Error("an acme-tls/1 only hello did not get the issuer's answer")
		}
		run(t, m)
		synctest.Wait()
		for name, hello := range map[string]tls.ClientHelloInfo{
			"acme-tls/1 and h2": {ServerName: host, SupportedProtos: []string{acme.ALPNProto, "h2"}},
			"no server name":    {SupportedProtos: []string{"h2"}},
			"another name":      {ServerName: "renderer", SupportedProtos: []string{"http/1.1"}},
		} {
			if c, err := m.GetCertificate(&hello); err != nil || c != cert {
				t.Errorf("%s: got %v, %v; want the held certificate", name, c, err)
			}
		}
		if f.challenges != 1 {
			t.Errorf("Challenge called %d times, want 1", f.challenges)
		}
	})
}

// TestRenewal covers the renewal time (AC-8): the manager issues for its host name, holds the
// certificate, and orders the next one once two thirds of its lifetime have passed; when that
// point is already near, it waits at least 30 seconds after the issue.
func TestRenewal(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		f := &issuer{issue: func(_ context.Context, n int) (*tls.Certificate, error) {
			now := time.Now()
			if n == 1 {
				// issued 10 hours ago for 12 hours: two thirds have passed already
				return ca.LeafBetween(t, now.Add(-10*time.Hour), now.Add(2*time.Hour), host), nil
			}
			return ca.Leaf(t, 24*time.Hour, host), nil
		}}
		m, logs := newManager(f, ca)
		run(t, m)
		synctest.Wait()
		if !m.Ready() || served(m) == nil {
			t.Fatal("no certificate held after the first issue")
		}
		time.Sleep(16*time.Hour + 30*time.Second + time.Minute)
		synctest.Wait()
		if got := f.gaps(); !slices.Equal(got, []time.Duration{16 * time.Hour, 30 * time.Second}) {
			t.Errorf("issues %v apart, want 16h then 30s", got)
		}
		if f.names[0] != host {
			t.Errorf("issued for %q, want %q", f.names[0], host)
		}
		if n := strings.Count(logs.String(), `"msg":"certificate held","host":"`+host+`"`); n != 3 {
			t.Errorf("%d certificate held lines, want 3\n%s", n, logs)
		}
	})
}

// TestBackoff covers failed attempts (AC-8, AC-9): retries 1, 2, 4, 8, 16, 30 and 30 seconds
// apart with one warning each, the held certificate serving meanwhile, and the backoff starting
// at 1 second again after a success.
func TestBackoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		// call 0 succeeds; 1 to 7 fail; 8 succeeds; 9 fails; 10 succeeds
		f := &issuer{issue: func(_ context.Context, n int) (*tls.Certificate, error) {
			if (n >= 1 && n <= 7) || n == 9 {
				return nil, errDown
			}
			return ca.Leaf(t, 3*time.Hour, host), nil
		}}
		m, logs := newManager(f, ca)
		run(t, m)
		synctest.Wait()
		first := served(m)
		time.Sleep(2 * time.Hour) // renewal at two thirds of 3 hours
		synctest.Wait()
		if served(m) != first {
			t.Error("the held certificate stopped serving while renewal fails")
		}
		time.Sleep(4 * time.Hour)
		synctest.Wait()
		s := time.Second
		want := []time.Duration{2 * time.Hour, s, 2 * s, 4 * s, 8 * s, 16 * s, 30 * s, 30 * s, 2 * time.Hour, s}
		if got := f.gaps(); !slices.Equal(got[:len(want)], want) {
			t.Errorf("issues %v apart, want %v", got, want)
		}
		if n := strings.Count(logs.String(), "certificate issue failed"); n != 8 {
			t.Errorf("%d warnings, want 8\n%s", n, logs)
		}
		if strings.Contains(logs.String(), "PRIVATE KEY") {
			t.Error("a key reached the log")
		}
	})
}

// TestAttemptTimeout covers the attempt deadline (AC-8): an attempt the issuer never answers is
// cut off after 5 minutes and retried.
func TestAttemptTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		f := &issuer{issue: func(ctx context.Context, n int) (*tls.Certificate, error) {
			if n == 0 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return ca.Leaf(t, 24*time.Hour, host), nil
		}}
		m, _ := newManager(f, ca)
		run(t, m)
		time.Sleep(5*time.Minute + 2*time.Second)
		synctest.Wait()
		if got := f.ended[0].Sub(f.calls[0]); got != 5*time.Minute {
			t.Errorf("the first attempt was cut after %s, want 5m", got)
		}
		if f.count() != 2 || !m.Ready() {
			t.Errorf("%d attempts, ready %v; want a second attempt that holds", f.count(), m.Ready())
		}
	})
}

// TestIssuedCheck covers the check of an issued certificate (AC-8): each bad result is a failed
// attempt (one warning, nothing held, retried), and a leaf from a CA clock up to a minute ahead
// passes.
func TestIssuedCheck(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		bad  func(t *testing.T, ca *testpki.CA) *tls.Certificate
		want string
	}{
		"another name": {
			bad: func(t *testing.T, ca *testpki.CA) *tls.Certificate {
				return ca.Leaf(t, 24*time.Hour, "renderer.sdk-test.internal")
			},
			want: "certificate is valid for renderer.sdk-test.internal",
		},
		"a foreign CA": {
			bad:  func(t *testing.T, _ *testpki.CA) *tls.Certificate { return testpki.New(t).Leaf(t, 24*time.Hour, host) },
			want: "unknown authority",
		},
		"a key mismatch": {
			bad: func(t *testing.T, ca *testpki.CA) *tls.Certificate {
				c := ca.Leaf(t, 24*time.Hour, host)
				c.PrivateKey = ca.Leaf(t, time.Hour, host).PrivateKey
				return c
			},
			want: "public key is not the certificate's private key",
		},
		"no time left": {
			bad:  func(t *testing.T, ca *testpki.CA) *tls.Certificate { return ca.Leaf(t, 30*time.Second, host) },
			want: "expired or is not yet valid",
		},
		"expired": {
			bad: func(t *testing.T, ca *testpki.CA) *tls.Certificate {
				now := time.Now()
				return ca.LeafBetween(t, now.Add(-time.Hour), now.Add(-time.Minute), host)
			},
			want: "it expired at",
		},
		"a CA clock two minutes ahead": {
			bad: func(t *testing.T, ca *testpki.CA) *tls.Certificate {
				now := time.Now()
				return ca.LeafBetween(t, now.Add(2*time.Minute), now.Add(24*time.Hour), host)
			},
			want: "expired or is not yet valid",
		},
		"no leaf": {
			bad: func(t *testing.T, ca *testpki.CA) *tls.Certificate {
				c := ca.Leaf(t, 24*time.Hour, host)
				c.Leaf = nil
				return c
			},
			want: "no leaf",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ca := testpki.New(t)
				f := &issuer{issue: func(_ context.Context, n int) (*tls.Certificate, error) {
					if n == 0 {
						return tc.bad(t, ca), nil
					}
					return ca.Leaf(t, 24*time.Hour, host), nil
				}}
				m, logs := newManager(f, ca)
				run(t, m)
				synctest.Wait()
				if m.Ready() || served(m) != nil {
					t.Error("a refused certificate is held")
				}
				if !strings.Contains(logs.String(), "certificate issue failed") || !strings.Contains(logs.String(), tc.want) {
					t.Errorf("want a warning with %q\n%s", tc.want, logs)
				}
				time.Sleep(time.Second)
				synctest.Wait()
				if !m.Ready() {
					t.Error("the retry did not hold its certificate")
				}
			})
		})
	}

	t.Run("a CA clock 30 seconds ahead", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			ca := testpki.New(t)
			f := &issuer{issue: func(context.Context, int) (*tls.Certificate, error) {
				now := time.Now()
				return ca.LeafBetween(t, now.Add(30*time.Second), now.Add(24*time.Hour), host), nil
			}}
			m, logs := newManager(f, ca)
			run(t, m)
			synctest.Wait()
			if !m.Ready() {
				t.Errorf("refused\n%s", logs)
			}
		})
	})
}

// clockAhead is the real clock read ahead by a duration a test moves.
type clockAhead struct{ by atomic.Int64 }

func (c *clockAhead) Now() time.Time                         { return time.Now().Add(time.Duration(c.by.Load())) }
func (c *clockAhead) After(d time.Duration) <-chan time.Time { return time.After(d) }

// TestReadyTimeLeft covers the readiness rule (AC-9): unready before a certificate is held, ready
// while it has at least two hours left, and unready below that while it still serves.
func TestReadyTimeLeft(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		cert := ca.Leaf(t, 24*time.Hour, host)
		f := &issuer{issue: func(context.Context, int) (*tls.Certificate, error) { return cert, nil }}
		m, _ := newManager(f, ca)
		clock := &clockAhead{}
		m.SetClock(clock)
		if m.Ready() {
			t.Error("ready before Run")
		}
		run(t, m)
		synctest.Wait()
		clock.by.Store(int64(22*time.Hour - time.Minute))
		if !m.Ready() {
			t.Error("not ready with 2 hours and a minute left")
		}
		clock.by.Store(int64(22*time.Hour + time.Minute))
		if m.Ready() {
			t.Error("ready with under 2 hours left")
		}
		if served(m) != cert {
			t.Error("the held certificate no longer serves")
		}
	})
}
