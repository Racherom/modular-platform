package signin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/racherom/modular-platform/sdk/httpkit"
)

const (
	loginTTL   = 10 * time.Minute
	sweepEvery = 10 * time.Minute
)

// Claims is what the provider says about who signed in.
type Claims struct {
	Identity      Identity
	Email         string
	EmailVerified bool
}

// Provider is the identity provider, behind its OIDC protocol.
type Provider interface {
	// Ready reports whether discovery has succeeded.
	Ready() bool
	// AuthCodeURL is where the browser signs in; relogin asks for credentials again.
	AuthCodeURL(state, nonce, verifier string, relogin bool) string
	// Exchange trades the code for verified claims (userinfo first, the ID token as a fallback)
	// and the raw ID token, checking the nonce.
	Exchange(ctx context.Context, code, verifier, nonce string) (Claims, string, error)
	// EndSessionURL is the provider's end_session_endpoint; empty when discovery lists none.
	EndSessionURL() string
}

// Config is what the service needs beside its provider and store.
type Config struct {
	PublicOrigin string // scheme, host and port the browser sees
	BasePath     string // empty at the site root, else like /platform
	AdminEmail   string
	Idle, Max    time.Duration
}

// Service signs the admin in and keeps their sessions.
type Service struct {
	cfg   Config
	store Store
	idp   Provider
	log   *slog.Logger
	now   func() time.Time
}

// New returns the service.
func New(cfg Config, store Store, idp Provider, log *slog.Logger) *Service {
	return &Service{cfg: cfg, store: store, idp: idp, log: log, now: time.Now}
}

// cookie names follow spec 0001: __Host- with Path=/ at the site root, __Secure- with
// Path=<base>/ under a sub path, never a Domain attribute.
func (s *Service) cookie(name, value string, maxAge int) *http.Cookie {
	c := &http.Cookie{
		Name: "__Host-mp_" + name, Value: value, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
	if s.cfg.BasePath != "" {
		c.Name, c.Path = "__Secure-mp_"+name, s.cfg.BasePath+"/"
	}
	return c
}

func (s *Service) cookieName(name string) string { return s.cookie(name, "", 0).Name }

// SessionCookieName is the name of the session cookie.
func (s *Service) SessionCookieName() string { return s.cookieName("session") }

func (s *Service) home() string { return s.cfg.BasePath + "/" }

// login is the state the login cookie carries to the callback.
type login struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n"`
	ReturnTo string `json:"r"`
}

// Login sends the browser to the provider (GET /auth/login). It answers 503
// provider_unavailable while discovery has not succeeded.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !s.idp.Ready() {
		httpkit.WriteProblem(w, http.StatusServiceUnavailable, "provider_unavailable", "The identity provider has not answered yet; try again shortly.")
		return
	}
	l := login{State: random(), Verifier: random() + random(), Nonce: random(), ReturnTo: s.returnTo(r.URL.Query().Get("return_to"))}
	data, _ := json.Marshal(l) //nolint:errchkjson // plain strings
	http.SetCookie(w, s.cookie("login", base64.RawURLEncoding.EncodeToString(data), int(loginTTL.Seconds())))
	_, relogin := cookieValue(r, s.cookieName("relogin"))
	if relogin {
		http.SetCookie(w, s.cookie("relogin", "", -1))
	}
	//nolint:gosec // the provider's authorization endpoint from discovery, not request input
	http.Redirect(w, r, s.idp.AuthCodeURL(l.State, l.Nonce, l.Verifier, relogin), http.StatusFound)
}

// returnTo keeps a path under the base, never another origin; anything else is the home page.
func (s *Service) returnTo(v string) string {
	u, err := url.Parse(v)
	if v == "" || err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, s.home()) ||
		strings.HasPrefix(v, "//") || strings.Contains(v, `\`) {
		return s.home()
	}
	return u.RequestURI()
}

// Callback finishes a sign in (GET /auth/callback): a session for the admin, else the signed
// out screen with the reason.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	http.SetCookie(w, s.cookie("login", "", -1))
	refuse := func(reason string, attrs ...any) {
		s.log.InfoContext(ctx, "sign in refused", append([]any{slog.String("reason", reason)}, attrs...)...)
		//nolint:gosec // the home path from config and one of the fixed reason codes
		http.Redirect(w, r, s.home()+"?signed_out="+reason, http.StatusFound)
	}
	raw, ok := cookieValue(r, s.cookieName("login"))
	var l login
	if data, err := base64.RawURLEncoding.DecodeString(raw); !ok || err != nil || json.Unmarshal(data, &l) != nil ||
		l.State == "" || r.URL.Query().Get("state") != l.State {
		refuse(ReasonStateMismatch)
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		refuse(ReasonProviderError, slog.String("error", r.URL.Query().Get("error")))
		return
	}
	claims, idToken, err := s.idp.Exchange(ctx, r.URL.Query().Get("code"), l.Verifier, l.Nonce)
	if err != nil {
		refuse(ReasonProviderError, slog.Any("error", err))
		return
	}
	id := claims.Identity
	attrs := []any{slog.String("iss", id.Issuer), slog.String("sub", id.Subject)}
	reason, err := s.admit(ctx, claims)
	if err != nil {
		s.log.ErrorContext(ctx, "sign in failed", append(attrs, slog.Any("error", err))...)
		httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "Sign in failed; try again.")
		return
	}
	if reason != "" {
		refuse(reason, attrs...)
		return
	}
	token, hash := newToken()
	now := s.now()
	sess := Session{TokenHash: hash, Identity: id, Email: claims.Email, ExpiresAt: now.Add(s.cfg.Max)}
	if s.idp.EndSessionURL() != "" {
		sess.IDToken = idToken
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		s.log.ErrorContext(ctx, "sign in failed", append(attrs, slog.Any("error", err))...)
		httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "Sign in failed; try again.")
		return
	}
	http.SetCookie(w, s.cookie("session", token, int(s.cfg.Max.Seconds())))
	s.log.InfoContext(ctx, "signed in", attrs...)
	http.Redirect(w, r, l.ReturnTo, http.StatusFound)
}

// admit decides whether claims get a session: the bound admin does; while none is bound, the
// admin email (whole address, case insensitive) with a verified email binds itself. It returns
// the refusal reason, or "" to admit.
func (s *Service) admit(ctx context.Context, c Claims) (string, error) {
	bound, ok, err := s.store.AdminBinding(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		if !strings.EqualFold(c.Email, s.cfg.AdminEmail) {
			return ReasonNotAdmin, nil
		}
		if !c.EmailVerified {
			return ReasonEmailUnverified, nil
		}
		if bound, err = s.store.BindAdmin(ctx, c.Identity, c.Email); err != nil {
			return "", err
		}
		if bound == c.Identity {
			s.log.InfoContext(ctx, "admin bound", slog.String("iss", bound.Issuer), slog.String("sub", bound.Subject))
		}
	}
	if bound != c.Identity {
		return ReasonNotAdmin, nil
	}
	return "", nil
}

// Authenticate returns the request's session, recording its activity, or ErrUnauthenticated.
func (s *Service) Authenticate(r *http.Request) (Session, error) {
	sess, err := s.Peek(r)
	if err != nil {
		return Session{}, err
	}
	if err := s.store.TouchSession(r.Context(), sess.TokenHash); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Peek returns the request's session without recording activity, as an event stream does.
func (s *Service) Peek(r *http.Request) (Session, error) {
	raw, ok := cookieValue(r, s.SessionCookieName())
	if !ok {
		return Session{}, ErrUnauthenticated
	}
	hash, ok := tokenHash(raw)
	if !ok {
		return Session{}, ErrUnauthenticated
	}
	return s.Check(r.Context(), hash)
}

// Check returns the session with hash while it is neither expired nor idle.
func (s *Service) Check(ctx context.Context, hash []byte) (Session, error) {
	sess, ok, err := s.store.Session(ctx, hash)
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	if !ok || !now.Before(sess.ExpiresAt) || !now.Before(sess.IdleExpiresAt(s.cfg.Idle)) {
		return Session{}, ErrUnauthenticated
	}
	return sess, nil
}

// Stream reasons, sent with the signed-out event.
const (
	StreamExpired   = "expired"
	StreamSignedOut = "signed_out"
)

// StreamCheck reports whether the session with hash still holds for an open event stream, and
// otherwise why not: signed_out when it was deleted, expired when it ran out.
func (s *Service) StreamCheck(ctx context.Context, hash []byte) (string, error) {
	_, ok, err := s.store.Session(ctx, hash)
	if err != nil {
		return "", err
	}
	if !ok {
		return StreamSignedOut, nil
	}
	if _, err := s.Check(ctx, hash); errors.Is(err, ErrUnauthenticated) {
		return StreamExpired, nil
	} else if err != nil {
		return "", err
	}
	return "", nil
}

// Logout ends sess, clears both cookies and marks the browser so its next sign in asks for
// credentials again. It returns where the browser goes next.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request, sess Session) (string, error) {
	if err := s.store.DeleteSession(r.Context(), sess.TokenHash); err != nil {
		return "", err
	}
	http.SetCookie(w, s.cookie("session", "", -1))
	http.SetCookie(w, s.cookie("login", "", -1))
	relogin := s.cookie("relogin", "1", 0) // a browser session cookie: no Max-Age
	http.SetCookie(w, relogin)
	s.log.InfoContext(r.Context(), "signed out", slog.String("iss", sess.Identity.Issuer), slog.String("sub", sess.Identity.Subject))
	home := s.cfg.PublicOrigin + s.home()
	end := s.idp.EndSessionURL()
	if end == "" {
		return home, nil
	}
	u, err := url.Parse(end)
	if err != nil {
		return home, nil //nolint:nilerr // a broken provider URL falls back to the home page
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", home)
	if sess.IDToken != "" {
		q.Set("id_token_hint", sess.IDToken)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Sweep deletes expired and idle sessions every 10 minutes until ctx ends.
func (s *Service) Sweep(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.store.DeleteStaleSessions(ctx, s.cfg.Idle); err != nil {
				s.log.WarnContext(ctx, "session sweep failed", slog.Any("error", err))
			} else if n > 0 {
				s.log.InfoContext(ctx, "sessions swept", slog.Int64("deleted", n))
			}
		}
	}
}

// CheckLifetimes is the start rule spec 0006 AC-3 adds to the declared bounds: session-max may
// not be below session-idle.
func CheckLifetimes(idle, maxAge time.Duration) error {
	if maxAge < idle {
		return fmt.Errorf("session-max %s is below session-idle %s", maxAge, idle)
	}
	return nil
}

func cookieValue(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
