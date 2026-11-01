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
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/racherom/modular-platform/sdk/internal/testpki"
	"github.com/racherom/modular-platform/sdk/tlsacme"
)

const host = "tm.sdk-test.internal"

// getter is a fake autocert: it hands out cert, fails the calls fail picks (counted from 0), and
// records every hello it got.
type getter struct {
	fail func(call int) bool

	mu     sync.Mutex
	cert   *tls.Certificate
	hellos []tls.ClientHelloInfo
	calls  []time.Time
}

func (g *getter) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := len(g.calls)
	g.calls = append(g.calls, time.Now())
	g.hellos = append(g.hellos, *hello)
	if g.fail != nil && g.fail(n) {
		return nil, errors.New("the issuer is down")
	}
	return g.cert, nil
}

func (g *getter) set(c *tls.Certificate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cert = c
}

func (g *getter) callTimes(start time.Time) []time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []time.Duration
	for _, c := range g.calls {
		out = append(out, c.Sub(start))
	}
	return out
}

func (g *getter) lastHello() tls.ClientHelloInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hellos[len(g.hellos)-1]
}

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

// TestHelloRewrite covers the client hello rewrite (AC-8): the getter sees a hello offering ECDSA
// P-256 only, whatever the client offered, so autocert never enrols a second, RSA certificate for
// a TLS 1.3 only client. ServerName and SupportedProtos pass as they came, so a tls-alpn-01
// validation still gets its challenge certificate, and the incoming hello is never changed.
func TestHelloRewrite(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]tls.ClientHelloInfo{
		"a TLS 1.3 only client": {
			ServerName:        host,
			CipherSuites:      []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256},
			SignatureSchemes:  []tls.SignatureScheme{tls.PSSWithSHA256, tls.Ed25519},
			SupportedCurves:   []tls.CurveID{tls.X25519},
			SupportedProtos:   []string{"h2", "http/1.1"},
			SupportedVersions: []uint16{tls.VersionTLS13},
		},
		"a tls-alpn-01 validation": {
			ServerName:      host,
			SupportedProtos: []string{acme.ALPNProto},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := &getter{cert: &tls.Certificate{}}
			m := tlsacme.New(g, host, slog.New(slog.DiscardHandler))
			hello := in
			if _, err := m.GetCertificate(&hello); err != nil {
				t.Fatal(err)
			}
			got := g.lastHello()
			if !slices.Equal(got.CipherSuites, []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}) ||
				!slices.Equal(got.SignatureSchemes, []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256}) ||
				!slices.Equal(got.SupportedCurves, []tls.CurveID{tls.CurveP256}) {
				t.Errorf("the getter saw %v %v %v", got.CipherSuites, got.SignatureSchemes, got.SupportedCurves)
			}
			if got.ServerName != in.ServerName || !slices.Equal(got.SupportedProtos, in.SupportedProtos) ||
				!slices.Equal(got.SupportedVersions, in.SupportedVersions) {
				t.Errorf("the getter saw %q %v %v, want them unchanged", got.ServerName, got.SupportedProtos, got.SupportedVersions)
			}
			if !slices.Equal(hello.CipherSuites, in.CipherSuites) || !slices.Equal(hello.SupportedCurves, in.SupportedCurves) {
				t.Error("the incoming hello was changed")
			}
		})
	}
}

// TestServerConfig covers the server's TLS settings (AC-8): TLS 1.3 only, and acme-tls/1 among
// the protocols so a tls-alpn-01 validation reaches autocert.
func TestServerConfig(t *testing.T) {
	t.Parallel()
	cfg := tlsacme.New(&getter{}, host, slog.New(slog.DiscardHandler)).ServerConfig()
	if cfg.MinVersion != tls.VersionTLS13 || !slices.Contains(cfg.NextProtos, acme.ALPNProto) || cfg.GetCertificate == nil {
		t.Errorf("ServerConfig = min %x, protos %v", cfg.MinVersion, cfg.NextProtos)
	}
}

// TestEnrol covers enrolment (AC-8, AC-9): the manager asks for the host name's certificate with
// a hello holding only the server name, retries failures with backoff from 1 second, and is not
// ready until it holds one. Before that, Ready never asks the getter (autocert would block).
func TestEnrol(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		g := &getter{cert: ca.Leaf(t, 24*time.Hour, host), fail: func(n int) bool { return n < 2 }}
		var logs syncBuffer
		m := tlsacme.New(g, host, slog.New(slog.NewJSONHandler(&logs, nil)))
		start := time.Now()
		if m.Ready() || len(g.callTimes(start)) != 0 {
			t.Fatal("ready, or the getter asked, before Run")
		}
		run(t, m)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if m.Ready() {
			t.Error("ready before a certificate is held")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if !m.Ready() {
			t.Error("not ready once a certificate is held")
		}
		calls := g.callTimes(start)
		if !slices.Equal(calls[:3], []time.Duration{0, time.Second, 3 * time.Second}) {
			t.Errorf("enrolment calls at %v, want 0s 1s 3s", calls)
		}
		if h := g.hellos[0]; h.ServerName != host || h.SupportedProtos != nil {
			t.Errorf("enrolment hello = %q %v", h.ServerName, h.SupportedProtos)
		}
		if n := strings.Count(logs.String(), "certificate not ready"); n != 2 {
			t.Errorf("%d not ready warnings, want 2\n%s", n, logs.String())
		}
		if !strings.Contains(logs.String(), `"msg":"certificate held","host":"`+host+`"`) {
			t.Errorf("no certificate held line\n%s", logs.String())
		}
	})
}

// TestReadyTimeLeft covers the readiness rule (AC-9): ready while the held certificate has at
// least two hours left, unready below that while it still serves, and the hourly check warns.
// Renewal is autocert's (its timer runs on the real clock), so here the certificate never renews.
func TestReadyTimeLeft(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		cert := ca.Leaf(t, 24*time.Hour, host)
		g := &getter{cert: cert}
		var logs syncBuffer
		m := tlsacme.New(g, host, slog.New(slog.NewJSONHandler(&logs, nil)))
		run(t, m)
		synctest.Wait()
		time.Sleep(22*time.Hour - time.Minute)
		synctest.Wait()
		if !m.Ready() {
			t.Error("not ready with 2 hours and a minute left")
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if m.Ready() {
			t.Error("ready with under 2 hours left")
		}
		if c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: host}); err != nil || c != cert {
			t.Errorf("the held certificate no longer serves: %v", err)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if !strings.Contains(logs.String(), "certificate expires soon") {
			t.Errorf("no expiry warning\n%s", logs.String())
		}
	})
}

// TestRenewedLogged covers the hourly check (AC-8): once autocert holds a new certificate, the
// check logs it and readiness follows the new one.
func TestRenewedLogged(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ca := testpki.New(t)
		g := &getter{cert: ca.Leaf(t, 3*time.Hour, host)}
		var logs syncBuffer
		m := tlsacme.New(g, host, slog.New(slog.NewJSONHandler(&logs, nil)))
		run(t, m)
		synctest.Wait()
		time.Sleep(90 * time.Minute)
		synctest.Wait()
		if m.Ready() {
			t.Error("ready with 90 minutes left")
		}
		g.set(ca.Leaf(t, 24*time.Hour, host))
		if !m.Ready() {
			t.Error("not ready on the renewed certificate")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if strings.Count(logs.String(), "certificate renewed") != 1 {
			t.Errorf("want one certificate renewed line\n%s", logs.String())
		}
	})
}

// TestACME covers the autocert settings (AC-8): the issuer's directory, the host name alone,
// memory only, and autocert's own renewal at a third of the lifetime (RenewBefore unset).
func TestACME(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	m, err := tlsacme.ACME("https://issuer.test/directory", ca.Pool(), host)
	if err != nil {
		t.Fatal(err)
	}
	if m.Client.DirectoryURL != "https://issuer.test/directory" || m.Client.Key == nil || m.Cache != nil || m.RenewBefore != 0 {
		t.Errorf("manager = directory %q, key %v, cache %v, renew before %s", m.Client.DirectoryURL, m.Client.Key != nil, m.Cache, m.RenewBefore)
	}
	if err := m.HostPolicy(t.Context(), host); err != nil {
		t.Errorf("host policy refuses %s: %v", host, err)
	}
	if err := m.HostPolicy(t.Context(), "other.sdk-test.internal"); err == nil {
		t.Error("host policy allows another name")
	}
}
