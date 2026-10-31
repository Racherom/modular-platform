// Package httpclient is the client of an http binding (spec 0005, "Bindings, clients and
// protection"): it reaches the provider at the binding's url and sends the binding's token as
// Authorization: Bearer. It never retries a request; the caller decides.
package httpclient

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
)

// Client calls one provider. It implements the Do method generated API clients take.
type Client struct {
	base *url.URL
	hc   *http.Client
}

// New returns a client for the provider at base. An empty token sends no Authorization header.
func New(base *url.URL, token string) *Client {
	b := *base
	return &Client{
		base: &b,
		hc:   &http.Client{Transport: &bearer{base: &b, token: token, next: http.DefaultTransport.(*http.Transport).Clone()}},
	}
}

// URL returns a copy of the provider's base URL.
func (c *Client) URL() *url.URL {
	u := *c.base
	return &u
}

// NewRequest builds a request to ref (a path with an optional query, like /plan?x=1) below the
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

// bearer adds the token to every request for the binding's scheme and host.
type bearer struct {
	base  *url.URL
	token string
	next  http.RoundTripper
}

func (b *bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.token == "" || req.URL.Scheme != b.base.Scheme || req.URL.Host != b.base.Host {
		return b.next.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}
