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
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"
)

const (
	// headerTimeout bounds the wait for the issuer's response headers.
	headerTimeout = 30 * time.Second
	// problemAccountDoesNotExist is the issuer's answer once it lost the account (RFC 8555).
	problemAccountDoesNotExist = "urn:ietf:params:acme:error:accountDoesNotExist"
	// challengeTLSALPN01 is the only challenge type the client answers.
	challengeTLSALPN01 = "tls-alpn-01"
)

// Client is the SDK's ACME engine and the Issuer outside tests. It orders from one ACME
// directory with an account key it makes per process and holds only in memory, and answers the
// issuer's tls-alpn-01 validation through Challenge. Issue calls never overlap.
type Client struct {
	acme *acme.Client

	mu         sync.Mutex // held for a whole Issue
	registered bool

	pending atomic.Pointer[challenge] // the answer to the validation in flight, if any
}

// challenge is the tls-alpn-01 answer for name while its authorization is pending.
type challenge struct {
	name string
	cert *tls.Certificate
}

var _ Issuer = (*Client)(nil)

// NewClient returns a client of the ACME directory that trusts roots (MP_CA_FILE) toward the
// issuer, ignores proxy env vars (the issuer is internal), and waits at most 30 seconds for
// response headers. It makes a fresh ECDSA P-256 account key and registers it on the first Issue.
func NewClient(directory string, roots *x509.CertPool) (*Client, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: account key: %w", err)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	tr.ResponseHeaderTimeout = headerTimeout
	return &Client{acme: &acme.Client{DirectoryURL: directory, Key: key, HTTPClient: &http.Client{Transport: tr}}}, nil
}

// Issue orders a certificate for name: it registers the account if needed, authorizes the one
// DNS identifier over tls-alpn-01, and finalizes the order with a CSR for a fresh ECDSA P-256 key.
// ctx bounds the whole order. When the issuer reports the account lost, the next Issue registers
// again.
func (c *Client) Issue(ctx context.Context, name string) (cert *tls.Certificate, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		var ae *acme.Error
		if errors.As(err, &ae) && ae.ProblemType == problemAccountDoesNotExist {
			c.registered = false
		}
	}()

	if !c.registered {
		_, err := c.acme.Register(ctx, &acme.Account{}, acme.AcceptTOS)
		if err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
			return nil, fmt.Errorf("tlsacme: register the account: %w", err)
		}
		c.registered = true
	}
	order, err := c.acme.AuthorizeOrder(ctx, acme.DomainIDs(name))
	if err != nil {
		return nil, fmt.Errorf("tlsacme: order: %w", err)
	}
	if len(order.Identifiers) != 1 || order.Identifiers[0].Type != "dns" || order.Identifiers[0].Value != name {
		return nil, fmt.Errorf("tlsacme: the order names %v, not %s alone", order.Identifiers, name)
	}
	for _, u := range order.AuthzURLs {
		if err := c.authorize(ctx, u, name); err != nil {
			return nil, err
		}
	}
	order, err = c.acme.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: wait for the order: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: certificate key: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		DNSNames:           []string{name},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}, key)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: CSR: %w", err)
	}
	der, _, err := c.acme.CreateCertFromOrder(ctx, order, csr, true)
	if err != nil {
		return nil, fmt.Errorf("tlsacme: finalize the order: %w", err)
	}
	leaf, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, fmt.Errorf("tlsacme: the issued leaf: %w", err)
	}
	return &tls.Certificate{Certificate: der, PrivateKey: key, Leaf: leaf}, nil
}

// authorize settles one authorization of the order: a valid one needs nothing, a pending one is
// validated over tls-alpn-01, and any other state fails the attempt.
func (c *Client) authorize(ctx context.Context, url, name string) error {
	z, err := c.acme.GetAuthorization(ctx, url)
	if err != nil {
		return fmt.Errorf("tlsacme: authorization: %w", err)
	}
	switch z.Status {
	case acme.StatusValid:
		return nil
	case acme.StatusPending:
	default:
		return fmt.Errorf("tlsacme: the authorization for %s is %s", z.Identifier.Value, z.Status)
	}
	var chal *acme.Challenge
	for _, ch := range z.Challenges {
		if ch.Type == challengeTLSALPN01 {
			chal = ch
			break
		}
	}
	if chal == nil {
		return fmt.Errorf("tlsacme: the issuer offers no %s challenge for %s", challengeTLSALPN01, z.Identifier.Value)
	}
	answer, err := c.acme.TLSALPN01ChallengeCert(chal.Token, name)
	if err != nil {
		return fmt.Errorf("tlsacme: challenge certificate: %w", err)
	}
	c.pending.Store(&challenge{name: name, cert: &answer})
	defer c.pending.Store(nil)
	if _, err := c.acme.Accept(ctx, chal); err != nil {
		return fmt.Errorf("tlsacme: accept the challenge: %w", err)
	}
	if _, err := c.acme.WaitAuthorization(ctx, z.URI); err != nil {
		return fmt.Errorf("tlsacme: wait for the authorization: %w", err)
	}
	return nil
}

// Challenge returns the pending tls-alpn-01 answer when the hello's server name (lowercased,
// a trailing dot trimmed) is the name being validated.
func (c *Client) Challenge(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.TrimSuffix(strings.ToLower(hello.ServerName), ".")
	if p := c.pending.Load(); p != nil && p.name == name {
		return p.cert, nil
	}
	return nil, fmt.Errorf("tlsacme: no validation pending for %q", hello.ServerName)
}
