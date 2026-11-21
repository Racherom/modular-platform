// Package setup is api's side of the first run setup (spec 0036): it knows whether an admin is
// bound yet, redeems the setup code for a setup session, passes the enroll answers to the
// keeper, closes the api while the setup is pending, and tells the keeper when the setup is done.
// It holds no session format of its own (libs/setupsession) and reads no file or env var (the
// Files port is the sdk's generated getters).
package setup

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

// Errors the service and the Keeper port report; the HTTP layer maps each to a problem code.
var (
	ErrCodeRefused  = errors.New("setup: code refused")
	ErrDone         = errors.New("setup: the setup is done")
	ErrTooManyTries = errors.New("setup: too many tries")
	ErrNoSession    = errors.New("setup: no valid setup session")
	ErrCodeUsed     = errors.New("setup: the code was used")     // a Keeper answer
	ErrCodeChanged  = errors.New("setup: the code changed")      // a Keeper answer
	ErrNoEnroll     = errors.New("setup: no enroll block")       // a Keeper answer
	ErrBusy         = errors.New("setup: the keeper is busy")    // a Keeper answer
	ErrEnrollFailed = errors.New("setup: the enroll run failed") // a Keeper answer
)

// InvalidAnswerError is a Keeper answer: the answer for Field breaks Rule.
type InvalidAnswerError struct{ Field, Rule string }

func (e *InvalidAnswerError) Error() string {
	return fmt.Sprintf("setup: answer %s breaks the rule: %s", e.Field, e.Rule)
}

// Files reads the holder's lifetime: setup files fresh at each call; false means the file is
// absent. The sdk's generated getters are the adapter.
type Files interface {
	Code(ctx context.Context) (string, bool, error)
	Key(ctx context.Context) ([]byte, bool, error)
}

// Ask is one enroll question.
type Ask struct {
	Name, Description string
	Secret            bool
	Pattern           string
	MinLength         int
}

// Keeper is the platform-setup capability, as api uses it.
type Keeper interface {
	// Enroll returns the identity module's enroll questions; nil when it has none.
	Enroll(ctx context.Context) ([]Ask, error)
	// CodeUsed records the code as used: ErrCodeUsed, ErrCodeChanged or ErrDone when refused.
	CodeUsed(ctx context.Context, codeSHA256 string) error
	// RunEnroll runs the enroll with the answers for the setup session value: ErrNoSession,
	// ErrDone, ErrNoEnroll, *InvalidAnswerError, ErrEnrollFailed or ErrBusy when refused.
	RunEnroll(ctx context.Context, session string, answers map[string]string) error
	// Done tells the keeper the setup is finished; it deletes the setup files.
	Done(ctx context.Context) error
}

// Admins answers whether an admin is bound.
type Admins interface {
	AdminBinding(ctx context.Context) (signin.Identity, bool, error)
}

// Provider reports whether the identity provider's discovery has succeeded.
type Provider interface{ Ready() bool }

// Config is what the service needs beside its ports.
type Config struct {
	BasePath string // empty at the site root
	Hops     int    // proxies in front of api that append X-Forwarded-For
}

// Service is the setup application service.
type Service struct {
	cfg      Config
	files    Files
	keeper   Keeper
	admins   Admins
	provider Provider
	identity Identity // nil: no first admin page (WithIdentity)
	handoffs used
	log      *slog.Logger
	limiter  *httpkit.RateLimiter

	now     func() time.Time
	wait    func(ctx context.Context, d time.Duration) error
	settled atomic.Bool   // an admin is bound; never unbound while api runs
	bound   chan struct{} // a bind happened: tell the keeper
}

// New returns the service.
func New(cfg Config, files Files, keeper Keeper, admins Admins, provider Provider, log *slog.Logger) *Service {
	return &Service{
		cfg: cfg, files: files, keeper: keeper, admins: admins, provider: provider, log: log,
		limiter: httpkit.NewRateLimiter(10, 10, 10*time.Minute, 4096),
		now:     time.Now, wait: sleep, bound: make(chan struct{}, 1), handoffs: used{limit: 1024},
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Pending reports whether no admin is bound. Once an admin is bound it stays false for the life
// of the process (an admin is unbound only by `api admin reset`, a separate process).
func (s *Service) Pending(ctx context.Context) (bool, error) {
	if s.settled.Load() {
		return false, nil
	}
	_, ok, err := s.admins.AdminBinding(ctx)
	if err != nil {
		return false, fmt.Errorf("checking the admin binding: %w", err)
	}
	if ok {
		s.settled.Store(true)
	}
	return !ok, nil
}

// SessionValue returns the request's setup cookie value when it is a valid session. The key is
// read at each check, so a deleted or replaced key ends every session at once.
func (s *Service) SessionValue(r *http.Request) (string, bool) {
	c, err := r.Cookie(setupsession.CookieName(s.cfg.BasePath))
	if err != nil || c.Value == "" {
		return "", false
	}
	key, ok, err := s.files.Key(r.Context())
	if err != nil || !ok {
		return "", false
	}
	if _, err := setupsession.Verify(key, c.Value, s.now()); err != nil {
		return "", false
	}
	return c.Value, true
}

// Session reports whether the request carries a valid setup session.
func (s *Service) Session(r *http.Request) bool {
	_, ok := s.SessionValue(r)
	return ok
}

// ClearCookie is the cookie that deletes the setup cookie.
func (s *Service) ClearCookie() *http.Cookie {
	c := s.cookie("", 0) //nolint:gosec // cookie sets Secure, HttpOnly and SameSite
	c.MaxAge = -1
	return c
}

func (s *Service) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: setupsession.CookieName(s.cfg.BasePath), Value: value, Path: setupsession.CookiePath(s.cfg.BasePath),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
}

// State is what GET /api/v1/setup answers.
type State struct {
	Pending       bool
	NoCode        bool
	Session       bool
	ProviderReady bool
	Enroll        []Ask // nil: the identity module has no enroll block; only with a session
	FirstAdmin    bool  // the identity module has a first_admin block; only with a session
}

// State describes the setup for the request.
func (s *Service) State(r *http.Request) (State, error) {
	ctx := r.Context()
	pending, err := s.Pending(ctx)
	if err != nil || !pending {
		return State{}, err
	}
	st := State{Pending: true, Session: s.Session(r)}
	if _, ok, err := s.files.Code(ctx); err != nil {
		return State{}, fmt.Errorf("reading the setup code: %w", err)
	} else if !ok {
		st.NoCode = true
	}
	if st.Session {
		st.ProviderReady = s.provider.Ready()
		if st.Enroll, err = s.keeper.Enroll(ctx); err != nil {
			return State{}, fmt.Errorf("asking the keeper for the enroll questions: %w", err)
		}
		// A failed lookup fails the whole state, so the page never hides the button by guessing.
		if _, _, st.FirstAdmin, err = s.firstAdmin(ctx); err != nil {
			return State{}, err
		}
	}
	return st, nil
}

// Redeem trades the code for the setup cookie. The code is accepted once: the keeper's code-used
// record is the single truth.
func (s *Service) Redeem(r *http.Request, code string) (*http.Cookie, error) {
	ctx := r.Context()
	remote := httpkit.RateKey(httpkit.ClientIP(r, s.cfg.Hops))
	refuse := func(reason string) (*http.Cookie, error) {
		s.log.InfoContext(ctx, "setup code refused", slog.String("remote", remote), slog.String("reason", reason))
		return nil, ErrCodeRefused
	}
	pending, err := s.Pending(ctx)
	if err != nil {
		return nil, err
	}
	if !pending {
		return nil, ErrDone
	}
	if !s.limiter.Allow(remote) {
		s.log.InfoContext(ctx, "setup code refused", slog.String("remote", remote), slog.String("reason", "rate"))
		return nil, ErrTooManyTries
	}
	want, ok, err := s.files.Code(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the setup code: %w", err)
	}
	if !ok {
		return refuse("wrong")
	}
	got, wantSum := sha256.Sum256([]byte(code)), sha256.Sum256([]byte(want))
	if subtle.ConstantTimeCompare(got[:], wantSum[:]) != 1 {
		return refuse("wrong")
	}
	// The key is read before the code is recorded as used, so a missing or unreadable key never
	// burns the code: the same code works again once the key is back.
	key, ok, err := s.files.Key(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the session key: %w", err)
	}
	if !ok {
		return nil, errors.New("setup: the session key file is missing")
	}
	switch err := s.keeper.CodeUsed(ctx, hex.EncodeToString(got[:])); {
	case errors.Is(err, ErrCodeUsed), errors.Is(err, ErrCodeChanged):
		return refuse("used")
	case errors.Is(err, ErrDone):
		return nil, ErrDone
	case err != nil:
		return nil, fmt.Errorf("recording the used code: %w", err)
	}
	value, expiry := setupsession.Mint(key, s.now())
	s.log.InfoContext(ctx, "setup code accepted", slog.String("remote", remote))
	return s.cookie(value, int(expiry.Sub(s.now()).Seconds())), nil
}

// Enroll passes the answers to the keeper with the request's setup session.
func (s *Service) Enroll(r *http.Request, answers map[string]string) error {
	ctx := r.Context()
	pending, err := s.Pending(ctx)
	if err != nil {
		return err
	}
	if !pending {
		return ErrDone
	}
	value, ok := s.SessionValue(r)
	if !ok {
		return ErrNoSession
	}
	if err := s.keeper.RunEnroll(ctx, value, answers); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "setup enrolled")
	return nil
}

// openPaths are the routes (under <base>/api/v1) that stay open while the setup is pending.
var openPaths = map[string]bool{
	"GET /setup": true, "POST /setup/code": true, "POST /setup/enroll": true,
	"GET /auth/login": true, "GET /auth/callback": true,
	"POST /setup/identity-link": true, "GET /setup/identity-enter": true, "GET /setup/gate": true,
}

// Gate closes the api while the setup is pending: every route but the setup and sign in routes
// answers 403 setup_pending. prefix is <base>/api/v1. Anything outside the prefix passes.
func (s *Service) Gate(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel, inside := cutPrefix(r.URL.Path, prefix)
		if !inside || openPaths[r.Method+" "+rel] {
			next.ServeHTTP(w, r)
			return
		}
		pending, err := s.Pending(r.Context())
		if err != nil {
			s.log.ErrorContext(r.Context(), "setup check failed", slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "api failed; try again.")
			return
		}
		if pending {
			httpkit.WriteProblem(w, http.StatusForbidden, "setup_pending", "The setup is not finished; open the setup page.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cutPrefix(path, prefix string) (string, bool) {
	if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
		return path[len(prefix):], true
	}
	return "", false
}
