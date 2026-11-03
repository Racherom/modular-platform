package signin_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/racherom/modular-platform/services/api/internal/signin"
)

type memStore struct {
	mu       sync.Mutex
	sessions map[string]signin.Session
	admin    *signin.Identity
}

func newMem() *memStore { return &memStore{sessions: map[string]signin.Session{}} }

func (m *memStore) CreateSession(_ context.Context, s signin.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.LastSeenAt = time.Now()
	m.sessions[string(s.TokenHash)] = s
	return nil
}

func (m *memStore) Session(_ context.Context, h []byte) (signin.Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(h)]
	return s, ok, nil
}
func (m *memStore) TouchSession(context.Context, []byte) error { return nil }
func (m *memStore) DeleteSession(_ context.Context, h []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(h))
	return nil
}

func (m *memStore) DeleteStaleSessions(context.Context, time.Duration) (int64, error) { return 0, nil }

func (m *memStore) AdminBinding(context.Context) (signin.Identity, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admin == nil {
		return signin.Identity{}, false, nil
	}
	return *m.admin, true, nil
}

func (m *memStore) BindAdmin(_ context.Context, id signin.Identity, _ string) (signin.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admin == nil {
		m.admin = &id
	}
	return *m.admin, nil
}

// fakeIDP answers every exchange with claims; authURL records the last authorization URL asked.
type fakeIDP struct {
	claims     signin.Claims
	endSession string
	relogin    bool
}

func (f *fakeIDP) Ready() bool { return true }
func (f *fakeIDP) AuthCodeURL(state, _, _ string, relogin bool) string {
	f.relogin = relogin
	return "https://idp.example/auth?state=" + state
}

func (f *fakeIDP) Exchange(context.Context, string, string, string) (signin.Claims, string, error) {
	return f.claims, "raw-id-token", nil
}
func (f *fakeIDP) EndSessionURL() string { return f.endSession }

const admin = "Admin@Example.com"

func newService(t *testing.T, idp *fakeIDP, st *memStore) (*signin.Service, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	return signin.New(signin.Config{
		PublicOrigin: "https://platform.localhost", AdminEmail: admin, Idle: time.Hour, Max: 12 * time.Hour,
	}, st, idp, slog.New(slog.NewJSONHandler(&logs, nil))), &logs
}

// signIn runs login then callback in one cookie jar and returns the callback's answer.
func signIn(t *testing.T, s *signin.Service, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/login?return_to=/deployments", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	s.Login(login, req)
	if login.Code != http.StatusFound {
		t.Fatalf("login: %d", login.Code)
	}
	u, _ := url.Parse(login.Header().Get("Location"))
	cb := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/callback?code=c&state="+u.Query().Get("state"), nil)
	for _, c := range login.Result().Cookies() {
		cb.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Callback(rec, cb)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-mp_session" && c.MaxAge > 0 {
			return c
		}
	}
	return nil
}

func TestAdminBinding(t *testing.T) {
	t.Parallel()
	first := signin.Identity{Issuer: "https://idp.example", Subject: "u1"}
	for name, tc := range map[string]struct {
		bound    *signin.Identity
		claims   signin.Claims
		wantGo   string // where the callback sends the browser
		session  bool
		wantBind bool
	}{
		"first verified admin email binds": {nil, signin.Claims{Identity: first, Email: "admin@example.COM", EmailVerified: true}, "/deployments", true, true},
		"unverified admin email":           {nil, signin.Claims{Identity: first, Email: admin}, "/?signed_out=email_unverified", false, false},
		"another email":                    {nil, signin.Claims{Identity: first, Email: "x@example.com", EmailVerified: true}, "/?signed_out=not_admin", false, false},
		"the bound admin":                  {&first, signin.Claims{Identity: first, Email: "changed@example.com"}, "/deployments", true, false},
		"admin email, other subject":       {&first, signin.Claims{Identity: signin.Identity{Issuer: first.Issuer, Subject: "u2"}, Email: admin, EmailVerified: true}, "/?signed_out=not_admin", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newMem()
			st.admin = tc.bound
			s, logs := newService(t, &fakeIDP{claims: tc.claims}, st)
			rec := signIn(t, s, nil)
			if got := rec.Header().Get("Location"); got != tc.wantGo {
				t.Errorf("callback goes to %q, want %q", got, tc.wantGo)
			}
			c := sessionCookie(rec)
			if (c != nil) != tc.session {
				t.Fatalf("session cookie %v, want %v", c, tc.session)
			}
			if c != nil && (!c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "") {
				t.Errorf("cookie attributes %+v", c)
			}
			if tc.wantBind && (st.admin == nil || *st.admin != first) {
				t.Errorf("admin bound %v", st.admin)
			}
			if strings.Contains(logs.String(), "example.com") || strings.Contains(logs.String(), "example.COM") {
				t.Error("an email reached the log")
			}
		})
	}
}

func TestSessionLifetime(t *testing.T) {
	t.Parallel()
	id := signin.Identity{Issuer: "i", Subject: "s"}
	st := newMem()
	s, _ := newService(t, &fakeIDP{claims: signin.Claims{Identity: id, Email: admin, EmailVerified: true}}, st)
	c := sessionCookie(signIn(t, s, nil))
	if c == nil {
		t.Fatal("no session")
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/session", nil)
	req.AddCookie(c)
	if _, err := s.Authenticate(req); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	for k, sess := range st.sessions {
		sess.LastSeenAt = time.Now().Add(-2 * time.Hour)
		st.sessions[k] = sess
	}
	if _, err := s.Authenticate(req); err != signin.ErrUnauthenticated { //nolint:errorlint // returned as is
		t.Errorf("idle session: %v, want ErrUnauthenticated", err)
	}
	bad := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	bad.AddCookie(&http.Cookie{Name: "__Host-mp_session", Value: "not-a-token"})
	if _, err := s.Authenticate(bad); err != signin.ErrUnauthenticated { //nolint:errorlint // returned as is
		t.Errorf("malformed cookie: %v", err)
	}
}

func TestLogoutAsksForCredentialsNextTime(t *testing.T) {
	t.Parallel()
	idp := &fakeIDP{claims: signin.Claims{Identity: signin.Identity{Issuer: "i", Subject: "s"}, Email: admin, EmailVerified: true}}
	st := newMem()
	s, _ := newService(t, idp, st)
	c := sessionCookie(signIn(t, s, nil))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(c)
	sess, err := s.Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	next, err := s.Logout(rec, req, sess)
	if err != nil || next != "https://platform.localhost/" {
		t.Errorf("next %q, err %v", next, err)
	}
	if _, err := s.Authenticate(req); err == nil {
		t.Error("the old cookie still works")
	}
	var relogin *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-mp_relogin" {
			relogin = c
		}
	}
	if relogin == nil || relogin.MaxAge != 0 {
		t.Fatalf("relogin cookie %+v", relogin)
	}
	signIn(t, s, []*http.Cookie{relogin})
	if !idp.relogin {
		t.Error("the next sign in did not ask for prompt=login")
	}
}

func TestLoginKeepsReturnToOnSite(t *testing.T) {
	t.Parallel()
	s, _ := newService(t, &fakeIDP{}, newMem())
	for _, v := range []string{"https://evil.example/", "//evil.example/x", `/\evil.example`, "relative"} {
		rec := httptest.NewRecorder()
		s.Login(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/login?return_to="+url.QueryEscape(v), nil))
		for _, c := range rec.Result().Cookies() {
			if c.Name == "__Host-mp_login" && strings.Contains(c.Value, "evil") {
				t.Errorf("return_to %q was kept", v)
			}
		}
	}
}

func TestCallbackRefusesAForeignState(t *testing.T) {
	t.Parallel()
	s, _ := newService(t, &fakeIDP{}, newMem())
	rec := httptest.NewRecorder()
	s.Callback(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/callback?code=c&state=x", nil))
	if got := rec.Header().Get("Location"); got != "/?signed_out=state_mismatch" {
		t.Errorf("goes to %q", got)
	}
}

func TestCheckLifetimes(t *testing.T) {
	t.Parallel()
	if signin.CheckLifetimes(time.Hour, 30*time.Minute) == nil {
		t.Error("session-max below session-idle was accepted")
	}
	if err := signin.CheckLifetimes(time.Hour, time.Hour); err != nil {
		t.Error(err)
	}
}
