package signin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/racherom/modular-platform/services/api/internal/signin"
)

// fakeSetup is a setup whose session is whatever the request's "valid" cookie says.
type fakeSetup struct{ bound int }

func (f *fakeSetup) Session(r *http.Request) bool {
	c, err := r.Cookie("setup")
	return err == nil && c.Value == "valid"
}

func (f *fakeSetup) ClearCookie() *http.Cookie {
	return &http.Cookie{Name: "setup", Path: "/", MaxAge: -1}
}
func (f *fakeSetup) Bound() { f.bound++ }

var setupCookie = &http.Cookie{Name: "setup", Value: "valid"}

func setupService(t *testing.T, idp *fakeIDP, st *memStore) (*signin.Service, *fakeSetup) {
	t.Helper()
	svc, _ := newService(t, idp, st)
	fs := &fakeSetup{}
	return svc.WithSetup(fs), fs
}

func clearsSetup(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "setup" && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// TestFirstSignInBindsWithASetupSession covers spec 0036 AC-33 and AC-38: the identity binds
// with no email, the setup cookie is deleted in the same response, and the keeper is told.
func TestFirstSignInBindsWithASetupSession(t *testing.T) {
	t.Parallel()
	id := signin.Identity{Issuer: "https://idp.example", Subject: "u1"}
	st := newMem()
	svc, fs := setupService(t, &fakeIDP{claims: signin.Claims{Identity: id}}, st) // no email, nothing verified

	rec := signIn(t, svc, []*http.Cookie{setupCookie})
	if loc := rec.Header().Get("Location"); loc != "/stacks" {
		t.Fatalf("Location = %q", loc)
	}
	if sessionCookie(rec) == nil {
		t.Error("no session cookie")
	}
	if !clearsSetup(rec) {
		t.Error("the setup cookie was not deleted")
	}
	if fs.bound != 1 {
		t.Errorf("Bound calls = %d, want 1", fs.bound)
	}
	if st.admin == nil || *st.admin != id {
		t.Errorf("admin = %v, want %v", st.admin, id)
	}
}

// TestSignInNeedsTheSetupSession: while no admin is bound, no setup session means no login and
// no bind (spec 0036 AC-30).
func TestSignInNeedsTheSetupSession(t *testing.T) {
	t.Parallel()
	id := signin.Identity{Issuer: "https://idp.example", Subject: "u1"}
	st := newMem()
	svc, fs := setupService(t, &fakeIDP{claims: signin.Claims{Identity: id}}, st)

	login := httptest.NewRecorder()
	svc.Login(login, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/login", nil))
	if login.Code != http.StatusForbidden || login.Header().Get("Location") != "" {
		t.Errorf("login without a session: %d, Location %q", login.Code, login.Header().Get("Location"))
	}

	// a callback with a login cookie but no setup session binds nothing
	okLogin := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/login", nil)
	req.AddCookie(setupCookie)
	svc.Login(okLogin, req)
	u, _ := url.Parse(okLogin.Header().Get("Location"))
	cb := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/callback?code=c&state="+u.Query().Get("state"), nil)
	for _, c := range okLogin.Result().Cookies() {
		cb.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	svc.Callback(rec, cb) // the setup cookie is not on the callback request
	if loc := rec.Header().Get("Location"); loc != "/?signed_out=setup_pending" {
		t.Errorf("Location = %q, want setup_pending", loc)
	}
	if st.admin != nil || fs.bound != 0 || sessionCookie(rec) != nil {
		t.Errorf("bound %v, Bound calls %d, session %v; want nothing", st.admin, fs.bound, sessionCookie(rec))
	}
}

// TestFailedSignInKeepsTheSetupSession covers spec 0036 AC-35.
func TestFailedSignInKeepsTheSetupSession(t *testing.T) {
	t.Parallel()
	first := signin.Identity{Issuer: "https://idp.example", Subject: "u1"}
	other := signin.Identity{Issuer: "https://idp.example", Subject: "u2"}
	st := newMem()
	st.admin = &first
	svc, fs := setupService(t, &fakeIDP{claims: signin.Claims{Identity: other}}, st)

	rec := signIn(t, svc, []*http.Cookie{setupCookie})
	if loc := rec.Header().Get("Location"); loc != "/?signed_out=not_admin" {
		t.Errorf("Location = %q", loc)
	}
	if clearsSetup(rec) || fs.bound != 0 {
		t.Errorf("a refused sign in touched the setup (cleared %v, Bound %d)", clearsSetup(rec), fs.bound)
	}
}

// TestBoundAdminSignsInWithoutASetupSession: after the bind the setup session is not needed, and
// signing in again does not tell the keeper again.
func TestBoundAdminSignsInWithoutASetupSession(t *testing.T) {
	t.Parallel()
	id := signin.Identity{Issuer: "https://idp.example", Subject: "u1"}
	st := newMem()
	st.admin = &id
	svc, fs := setupService(t, &fakeIDP{claims: signin.Claims{Identity: id}}, st)

	rec := signIn(t, svc, nil)
	if sessionCookie(rec) == nil || clearsSetup(rec) || fs.bound != 0 {
		t.Errorf("session %v, cleared %v, Bound %d", sessionCookie(rec), clearsSetup(rec), fs.bound)
	}
}
