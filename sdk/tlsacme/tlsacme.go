// Package tlsacme holds a module's TLS certificate in TLS mode (spec 0005, "Bindings, clients and
// protection"). The Manager orders an ECDSA P-256 certificate for the module's host name through
// an Issuer, checks it against MP_CA_FILE, holds it, and renews it at two thirds of its lifetime;
// the module serves only TLS 1.3 with it and reports whether it can still serve. Client is the
// Issuer outside tests: the SDK's own ACME client on x/crypto/acme, validated over tls-alpn-01.
// Only Manager.Run orders certificates; a TLS handshake never does.
package tlsacme

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"
)

// Addr is where a module listens in TLS mode. It serves nothing in plain HTTP then.
const Addr = ":8443"

const (
	// minLeft is the least time left on the held certificate for the module to count as ready.
	minLeft = 2 * time.Hour
	// minRetryBackoff and maxRetryBackoff bound the backoff between failed attempts.
	minRetryBackoff = time.Second
	maxRetryBackoff = 30 * time.Second
	// attemptTimeout cuts off one attempt to issue a certificate.
	attemptTimeout = 5 * time.Minute
	// renewFloor is the least wait between an issue and the next one.
	renewFloor = 30 * time.Second
	// caSkew is how far ahead of the module's clock the issuer's clock may run.
	caSkew = time.Minute
)

// Issuer issues the module's certificates. Client is the one outside tests.
type Issuer interface {
	// Issue returns a new certificate for name, with Leaf set.
	Issue(ctx context.Context, name string) (*tls.Certificate, error)
	// Challenge answers a tls-alpn-01 validation of a pending Issue.
	Challenge(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// clock is where the manager reads time; tests replace it.
type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Manager holds the module's certificate. Run issues and renews it; ServerConfig serves it.
type Manager struct {
	issuer Issuer
	host   string
	roots  *x509.CertPool
	log    *slog.Logger
	clock  clock
	held   atomic.Pointer[tls.Certificate]
}

// New returns a manager that orders the certificate for host from issuer and holds it only once
// it verifies against roots (MP_CA_FILE). It holds none until Run has issued one.
func New(issuer Issuer, host string, roots *x509.CertPool, log *slog.Logger) *Manager {
	return &Manager{issuer: issuer, host: host, roots: roots, log: log, clock: realClock{}}
}

// GetCertificate answers a hello offering only acme-tls/1 with the issuer's validation answer,
// and any other hello with the held certificate, whatever its server name. With none held yet it
// fails the handshake.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if slices.Equal(hello.SupportedProtos, []string{acme.ALPNProto}) {
		return m.issuer.Challenge(hello)
	}
	if c := m.held.Load(); c != nil {
		return c, nil
	}
	return nil, errors.New("tlsacme: no certificate held yet")
}

// ServerConfig is the TLS config of the module's server: TLS 1.3 only, the held certificate for
// every handshake, and the issuer's answer to a tls-alpn-01 validation.
func (m *Manager) ServerConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
		GetCertificate: m.GetCertificate,
	}
}

// Ready reports whether a certificate is held with at least two hours left.
func (m *Manager) Ready() bool {
	c := m.held.Load()
	return c != nil && c.Leaf.NotAfter.Sub(m.clock.Now()) >= minLeft
}

// Run issues the first certificate and then renews it, each time once two thirds of the held
// one's lifetime have passed, but never sooner than 30 seconds after the last issue. A failed
// attempt is retried with backoff from 1 to 30 seconds while the held certificate keeps serving.
// Run returns when ctx ends, cutting any attempt in flight.
func (m *Manager) Run(ctx context.Context) {
	for {
		c, ok := m.obtain(ctx)
		if !ok {
			return
		}
		issued := m.clock.Now()
		lifetime := c.Leaf.NotAfter.Sub(c.Leaf.NotBefore)
		renew := c.Leaf.NotBefore.Add(lifetime * 2 / 3)
		renew = later(renew, issued.Add(renewFloor))
		select {
		case <-ctx.Done():
			return
		case <-m.clock.After(renew.Sub(issued)):
		}
	}
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// obtain makes attempts until one gives a certificate that passes the check, holds it, and
// returns it; false when ctx ended first.
func (m *Manager) obtain(ctx context.Context) (*tls.Certificate, bool) {
	backoff := minRetryBackoff
	for {
		c, err := m.attempt(ctx)
		if err == nil {
			m.held.Store(c)
			m.log.InfoContext(ctx, "certificate held", slog.String("host", m.host), slog.Time("not_after", c.Leaf.NotAfter))
			return c, true
		}
		if ctx.Err() != nil {
			return nil, false
		}
		m.log.WarnContext(ctx, "certificate issue failed", slog.Any("error", err))
		select {
		case <-ctx.Done():
			return nil, false
		case <-m.clock.After(backoff):
		}
		backoff = min(backoff*2, maxRetryBackoff)
	}
}

// attempt issues one certificate within attemptTimeout and checks it.
func (m *Manager) attempt(ctx context.Context) (*tls.Certificate, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	c, err := m.issuer.Issue(ctx, m.host)
	if err != nil {
		return nil, err
	}
	if err := m.check(c); err != nil {
		return nil, fmt.Errorf("tlsacme: the issued certificate is refused: %w", err)
	}
	return c, nil
}

// check holds an issued certificate to the rules: a leaf for the host name with server auth that
// verifies against roots, a CA clock up to a minute ahead tolerated; its key pair matches; it has
// not expired.
func (m *Manager) check(c *tls.Certificate) error {
	if c == nil || c.Leaf == nil {
		return errors.New("no leaf")
	}
	now := m.clock.Now()
	if !now.Before(c.Leaf.NotAfter) {
		return fmt.Errorf("it expired at %s", c.Leaf.NotAfter.Format(time.RFC3339))
	}
	inter := x509.NewCertPool()
	for _, der := range c.Certificate[1:] {
		ic, err := x509.ParseCertificate(der)
		if err != nil {
			return fmt.Errorf("chain: %w", err)
		}
		inter.AddCert(ic)
	}
	if _, err := c.Leaf.Verify(x509.VerifyOptions{
		Roots:         m.roots,
		Intermediates: inter,
		DNSName:       m.host,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime:   now.Add(caSkew),
	}); err != nil {
		return err
	}
	priv, ok := c.PrivateKey.(crypto.Signer)
	if !ok {
		return errors.New("the private key cannot sign")
	}
	if pub, ok := c.Leaf.PublicKey.(interface{ Equal(crypto.PublicKey) bool }); !ok || !pub.Equal(priv.Public()) {
		return errors.New("the leaf's public key is not the certificate's private key")
	}
	return nil
}

// ClientConfig is the TLS config for calling another SDK module: it trusts roots (MP_CA_FILE) and
// checks that the server's certificate names host.
func ClientConfig(roots *x509.CertPool, host string) *tls.Config {
	return &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS13}
}
