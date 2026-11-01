// Package tlsacme holds a module's TLS certificate in TLS mode (spec 0005, "Bindings, clients and
// protection"): autocert gets an ECDSA P-256 certificate for the module's host name from the
// issuer's ACME directory over tls-alpn-01 and renews it; the module serves only TLS 1.3 with it
// and reports whether the held certificate can still serve.
package tlsacme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Addr is where a module listens in TLS mode. It serves nothing in plain HTTP then.
const Addr = ":8443"

const (
	// minLeft is the least time left on the held certificate for the module to count as ready.
	minLeft = 2 * time.Hour
	// retryMin and retryMax bound the backoff between failed enrolments.
	retryMin = time.Second
	retryMax = 30 * time.Second
	// checkEvery is how often the held certificate's time left is read in the background.
	checkEvery = time.Hour
)

// Getter returns the certificate for a client hello: the module's own, or the answer to a
// tls-alpn-01 validation. *autocert.Manager is the one outside tests.
type Getter interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// ACME returns the autocert manager of a module: it orders from the ACME directory, trusting
// roots (MP_CA_FILE) for it, only for host, with a fresh ECDSA P-256 account key held in memory,
// and no cache, so a restart enrols again. It renews at a third of the certificate's lifetime.
func ACME(directory string, roots *x509.CertPool, host string) (*autocert.Manager, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: account key: %w", err)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Client:     &acme.Client{DirectoryURL: directory, Key: key, HTTPClient: &http.Client{Transport: tr}},
		HostPolicy: autocert.HostWhitelist(host),
	}, nil
}

// Manager holds the module's certificate. Run enrols it; ServerConfig serves it.
type Manager struct {
	get      Getter
	host     string
	log      *slog.Logger
	enrolled atomic.Bool
}

// New returns a manager that gets the certificate for host from get. It holds none until Run has
// enrolled.
func New(get Getter, host string, log *slog.Logger) *Manager {
	return &Manager{get: get, host: host, log: log}
}

// GetCertificate asks the getter with an ECDSA only copy of hello, so every certificate is ECDSA
// P-256. autocert picks the key type from the hello and finds ECDSA only in a TLS 1.2 cipher
// suite; a client offering TLS 1.3 alone would make it enrol a second, RSA certificate. Every
// TLS 1.3 client supports P-256 ECDSA, so the copy is always true. ServerName and
// SupportedProtos stay as they came: autocert answers a tls-alpn-01 hello from those two.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.get.GetCertificate(ecdsaHello(hello))
}

func ecdsaHello(hello *tls.ClientHelloInfo) *tls.ClientHelloInfo {
	h := *hello
	h.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
	h.SignatureSchemes = []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256}
	h.SupportedCurves = []tls.CurveID{tls.CurveP256}
	return &h
}

// ServerConfig is the TLS config of the module's server: TLS 1.3 only, the held certificate for
// every handshake, and autocert's answer to a tls-alpn-01 validation.
func (m *Manager) ServerConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
		GetCertificate: m.GetCertificate,
	}
}

// Ready reports whether a certificate is held with at least two hours left. Before the first
// enrolment it does not ask the getter: autocert would block until that enrolment ends.
func (m *Manager) Ready() bool {
	if !m.enrolled.Load() {
		return false
	}
	c, err := m.held()
	return err == nil && time.Until(c.Leaf.NotAfter) >= minLeft
}

// held returns the certificate autocert holds for the host name. After enrolment it never orders:
// autocert returns the one in memory, even an expired one, and renews on its own timer.
func (m *Manager) held() (*tls.Certificate, error) {
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: m.host})
	if err != nil {
		return nil, err
	}
	if c == nil || c.Leaf == nil {
		return nil, errors.New("tlsacme: the issuer returned no certificate")
	}
	return c, nil
}

// Run enrols the first certificate, retrying with backoff from 1 to 30 seconds until it holds
// one, then reads the held certificate's time left every hour and logs a renewal or a
// certificate about to expire. Run returns when ctx ends.
func (m *Manager) Run(ctx context.Context) {
	serial, ok := m.enrol(ctx)
	if !ok {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkEvery):
		}
		c, err := m.held()
		switch {
		case err != nil:
			m.log.WarnContext(ctx, "certificate not readable", slog.Any("error", err))
		case c.Leaf.SerialNumber.Cmp(serial) != 0:
			serial = c.Leaf.SerialNumber
			m.log.InfoContext(ctx, "certificate renewed", slog.Time("not_after", c.Leaf.NotAfter))
		case time.Until(c.Leaf.NotAfter) < minLeft:
			m.log.WarnContext(ctx, "certificate expires soon", slog.Time("not_after", c.Leaf.NotAfter))
		}
	}
}

// enrol gets the first certificate by asking for it with a hello holding only the host name
// (autocert otherwise waits for the first handshake). It returns the certificate's serial, and
// false when ctx ended first.
func (m *Manager) enrol(ctx context.Context) (serial *big.Int, ok bool) {
	backoff := retryMin
	for {
		c, err := m.held()
		if err == nil {
			m.enrolled.Store(true)
			m.log.InfoContext(ctx, "certificate held", slog.String("host", m.host), slog.Time("not_after", c.Leaf.NotAfter))
			return c.Leaf.SerialNumber, true
		}
		if ctx.Err() != nil {
			return nil, false
		}
		m.log.WarnContext(ctx, "certificate not ready", slog.Any("error", err))
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, retryMax)
	}
}

// ClientConfig is the TLS config for calling another SDK module: it trusts roots (MP_CA_FILE) and
// checks that the server's certificate names host.
func ClientConfig(roots *x509.CertPool, host string) *tls.Config {
	return &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS13}
}
