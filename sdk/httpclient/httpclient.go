// Package httpclient is the client of an http binding (spec 0005, "Bindings, clients and
// protection"): it reaches the provider at the binding's url and sends the binding's token as
// Authorization: Bearer. It never retries a request; the caller decides.
package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/caller"
)

// Client calls one provider. It implements the Do method generated API clients take.
type Client struct {
	base *url.URL
	hc   *http.Client
}

// New returns a client for the provider at base. An empty token sends no Authorization header.
func New(base *url.URL, token string) *Client {
	return newClient(base, token, nil, nil)
}

// Open is the sdk.Opener of an http binding: a client for the binding's url and token that
// trusts the certificates in its ca_file, if set, and sends the caller in a request's context
// as an MP-Caller token when the module holds a signing key.
func Open(b sdk.Binding) (sdk.Opened, error) {
	c := newClient(b.URL, b.Token.Reveal(), b.Roots, b.Caller)
	return sdk.Opened{Client: c, Ready: c.Ready, Close: c.hc.CloseIdleConnections}, nil
}

func newClient(base *url.URL, token string, roots *x509.CertPool, signer *caller.Signer) *Client {
	b := *base
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if roots != nil {
		tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &Client{
		base: &b,
		hc:   &http.Client{Transport: &bearer{base: &b, token: token, signer: signer, next: tr}},
	}
}

// URL returns a copy of the provider's base URL.
func (c *Client) URL() *url.URL {
	u := *c.base
	return &u
}

// NewRequest builds a request to ref (a path with an optional query, like /items?x=1) below the
// base URL.
func (c *Client) NewRequest(ctx context.Context, method, ref string, body io.Reader) (*http.Request, error) {
	r, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("httpclient: %w", err)
	}
	u := c.base.JoinPath(r.Path)
	u.RawQuery = r.RawQuery
	return http.NewRequestWithContext(ctx, method, u.String(), body)
}

// Do sends req. The token goes only to the binding's own scheme and host, so a request built
// for another address never carries it.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.hc.Do(req)
}

// Ready asks the provider's /readyz once. Any answer below 500 counts as ready, so a provider
// without a readiness endpoint (404) is ready as soon as it answers.
func (c *Client) Ready(ctx context.Context) error {
	req, err := c.NewRequest(ctx, http.MethodGet, "/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("GET %s answered %d", req.URL.Redacted(), resp.StatusCode)
	}
	return nil
}

// Format shows the base URL only, never the token.
func (c *Client) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "httpclient.Client{%s}", c.base.Redacted())
}

// LogValue shows the base URL only, never the token.
func (c *Client) LogValue() slog.Value { return slog.StringValue(c.base.Redacted()) }

// bearer adds the token, and an MP-Caller token minted for the caller in the request's context,
// to every request for the binding's scheme and host.
type bearer struct {
	base   *url.URL
	token  string
	signer *caller.Signer // nil: no MP-Caller
	next   http.RoundTripper
}

func (b *bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != b.base.Scheme || req.URL.Host != b.base.Host {
		return b.next.RoundTrip(req)
	}
	c, hasCaller := caller.From(req.Context())
	mint := b.signer != nil && hasCaller
	if b.token == "" && !mint {
		return b.next.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	if mint {
		// aud is the provider's host name: the URL's host without the port, lowercased
		token, err := b.signer.Mint(c, strings.ToLower(req.URL.Hostname()), time.Now())
		if err != nil {
			return nil, fmt.Errorf("httpclient: %w", err)
		}
		r.Header.Set(caller.Header, token)
	}
	return b.next.RoundTrip(r)
}
