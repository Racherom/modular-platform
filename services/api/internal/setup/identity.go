package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
)

// Errors of the identity handoff (spec 0037); the HTTP layer maps each to a problem code.
var (
	ErrNoFirstAdmin        = errors.New("setup: the identity module has no first admin page")
	ErrProviderUnavailable = errors.New("setup: the identity provider is not ready")
	ErrHandoffRefused      = errors.New("setup: handoff refused")
)

// FirstAdmin is where the identity provider's first admin page is.
type FirstAdmin struct {
	Origin string // https://<auth host>, no path
	Path   string // the page on that host, from the module's first_admin.path
}

// Identity tells where the first admin page is. ok is false when the identity module has no
// first_admin block; the setup then shows only the sign in.
type Identity interface {
	FirstAdmin(ctx context.Context) (fa FirstAdmin, ok bool, err error)
}

// WithIdentity sets the identity port the handoff needs and returns the service.
func (s *Service) WithIdentity(id Identity) *Service {
	s.identity = id
	return s
}

// used remembers handoff nonces until their token would have expired anyway.
type used struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	limit int
}

// take records nonce until expiry; it reports false when the nonce was taken before, or when the
// table is full (fail closed: a flood of links cannot make a reused token pass).
func (u *used) take(nonce string, expiry, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.seen == nil {
		u.seen = map[string]time.Time{}
	}
	for n, e := range u.seen {
		if !now.Before(e) {
			delete(u.seen, n)
		}
	}
	if _, dup := u.seen[nonce]; dup || len(u.seen) >= u.limit {
		return false
	}
	u.seen[nonce] = expiry
	return true
}

// IdentityLink returns a link on the auth host that trades a handoff token for the gate cookie.
// It needs a valid setup session, a pending setup and a ready provider.
func (s *Service) IdentityLink(r *http.Request) (string, error) {
	ctx := r.Context()
	pending, err := s.Pending(ctx)
	if err != nil {
		return "", err
	}
	if !pending {
		return "", ErrDone
	}
	value, ok := s.SessionValue(r)
	if !ok {
		return "", ErrNoSession
	}
	fa, host, ok, err := s.firstAdmin(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoFirstAdmin
	}
	if !s.provider.Ready() {
		return "", ErrProviderUnavailable
	}
	key, ok, err := s.files.Key(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the session key: %w", err)
	}
	if !ok {
		return "", ErrNoSession
	}
	expiry, err := setupsession.Verify(key, value, s.now())
	if err != nil {
		return "", ErrNoSession
	}
	nonce, err := newNonce()
	if err != nil {
		return "", err
	}
	token := setupsession.MintHandoff(key, host, expiry, s.now(), nonce)
	return fa.Origin + s.cfg.BasePath + "/api/v1/setup/identity-enter?t=" + url.QueryEscape(token), nil
}

// Enter trades a handoff token for the auth host cookie and returns where to send the browser.
// The request must be for the auth host. Every refusal is ErrHandoffRefused (the cause is logged
// without the token).
func (s *Service) Enter(r *http.Request, token string) (*http.Cookie, string, error) {
	ctx := r.Context()
	refuse := func(reason string) (*http.Cookie, string, error) {
		s.log.InfoContext(ctx, "setup handoff refused", slog.String("reason", reason))
		return nil, "", ErrHandoffRefused
	}
	pending, err := s.Pending(ctx)
	if err != nil {
		return nil, "", err
	}
	if !pending {
		return refuse("done")
	}
	fa, host, ok, err := s.firstAdmin(ctx)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return refuse("no first admin")
	}
	if r.Host != host {
		return refuse("host")
	}
	key, ok, err := s.files.Key(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("reading the session key: %w", err)
	}
	if !ok {
		return refuse("retired")
	}
	h, err := setupsession.VerifyHandoff(key, host, token, s.now())
	if err != nil {
		return refuse("token")
	}
	if !s.handoffs.take(h.Nonce, h.Expiry, s.now()) {
		return refuse("reused")
	}
	maxAge := int(h.SessionExpiry.Sub(s.now()).Seconds())
	c := &http.Cookie{
		Name: setupsession.GateCookieName(), Value: setupsession.MintGate(key, h.SessionExpiry), Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
	s.log.InfoContext(ctx, "setup handoff accepted")
	return c, fa.Origin + fa.Path, nil
}

// GateOpen answers the ingress's question for a guarded provider path. It is true only while no
// admin is bound and the request carries a valid auth host cookie. Any error answers false.
func (s *Service) GateOpen(r *http.Request) bool {
	ctx := r.Context()
	pending, err := s.Pending(ctx)
	if err != nil || !pending {
		return false
	}
	c, err := r.Cookie(setupsession.GateCookieName())
	if err != nil || c.Value == "" {
		return false
	}
	key, ok, err := s.files.Key(ctx)
	if err != nil || !ok {
		return false
	}
	_, err = setupsession.VerifyGate(key, c.Value, s.now())
	return err == nil
}

// firstAdmin reads the port and the auth host (host and port) of its origin.
func (s *Service) firstAdmin(ctx context.Context) (FirstAdmin, string, bool, error) {
	if s.identity == nil {
		return FirstAdmin{}, "", false, nil
	}
	fa, ok, err := s.identity.FirstAdmin(ctx)
	if err != nil {
		return FirstAdmin{}, "", false, fmt.Errorf("asking where the first admin page is: %w", err)
	}
	if !ok {
		return FirstAdmin{}, "", false, nil
	}
	u, err := url.Parse(fa.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return FirstAdmin{}, "", false, fmt.Errorf("the first admin origin %q is not an https origin", fa.Origin)
	}
	return fa, u.Host, true, nil
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("making a handoff nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
