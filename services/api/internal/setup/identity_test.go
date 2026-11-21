package setup_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
	"github.com/racherom/modular-platform/services/api/internal/setup"
)

type fakeIdentity struct {
	fa setup.FirstAdmin
	ok bool
}

func (f fakeIdentity) FirstAdmin(context.Context) (setup.FirstAdmin, bool, error) {
	return f.fa, f.ok, nil
}

var authIdentity = fakeIdentity{fa: setup.FirstAdmin{Origin: "https://auth.platform.test", Path: "/setup"}, ok: true}

// identityRig is a rig whose service knows the auth host, with a clock the test moves.
func identityRig(t *testing.T, base string) (*rig, *time.Time) {
	t.Helper()
	r := newRig(t, base)
	r.svc.WithIdentity(authIdentity)
	now := time.Now()
	r.svc.SetNow(func() time.Time { return now })
	return r, &now
}

// link returns the handoff token from an identity link.
func (r *rig) link(t *testing.T, withSession *http.Request) (token, link string) {
	t.Helper()
	link, err := r.svc.IdentityLink(withSession)
	if err != nil {
		t.Fatalf("IdentityLink: %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("t"), link
}

func authRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "https://auth.platform.test/api/v1/setup/identity-enter?t="+url.QueryEscape(token), nil)
	return req
}

func TestIdentityHandoff(t *testing.T) {
	for base, wantPrefix := range map[string]string{"": "https://auth.platform.test/api/v1/", "/platform": "https://auth.platform.test/platform/api/v1/"} {
		r, _ := identityRig(t, base)
		token, link := r.link(t, r.redeem(t))
		if !strings.HasPrefix(link, wantPrefix+"setup/identity-enter?t=") {
			t.Errorf("base %q: link = %s", base, link)
		}
		req := authRequest(token)
		c, to, err := r.svc.Enter(req, token)
		if err != nil {
			t.Fatalf("base %q: Enter: %v", base, err)
		}
		if to != "https://auth.platform.test/setup" {
			t.Errorf("base %q: goes to %q", base, to)
		}
		if c.Name != "__Host-mp_gate" || c.Path != "/" || c.Domain != "" || !c.Secure || !c.HttpOnly {
			t.Errorf("base %q: cookie = %+v", base, c)
		}
		if c.MaxAge < 24*60*60-5 || c.MaxAge > 24*60*60 {
			t.Errorf("base %q: max age = %d, want the setup session's end", base, c.MaxAge)
		}
		if strings.Contains(r.logs.String(), token) || strings.Contains(r.logs.String(), c.Value) {
			t.Errorf("the log holds a token or a cookie: %s", r.logs)
		}
		gate := httptest.NewRequest(http.MethodGet, "/api/v1/setup/gate", nil)
		gate.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		if !r.svc.GateOpen(gate) {
			t.Errorf("base %q: the gate refuses a valid cookie", base)
		}
	}
}

func TestIdentityLinkRefusals(t *testing.T) {
	t.Run("no session", func(t *testing.T) {
		r, _ := identityRig(t, "")
		if _, err := r.svc.IdentityLink(request(http.MethodPost, "/", "192.0.2.1")); !errors.Is(err, setup.ErrNoSession) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("done", func(t *testing.T) {
		r, _ := identityRig(t, "")
		req := r.redeem(t)
		r.admins.bound = true
		if _, err := r.svc.IdentityLink(req); !errors.Is(err, setup.ErrDone) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("provider not ready", func(t *testing.T) {
		r, _ := identityRig(t, "")
		req := r.redeem(t)
		r.svc = setup.New(setup.Config{Hops: 1}, r.files, r.keeper, r.admins, fakeProvider{}, nil).WithIdentity(authIdentity)
		if _, err := r.svc.IdentityLink(req); !errors.Is(err, setup.ErrProviderUnavailable) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("module without first admin", func(t *testing.T) {
		r, _ := identityRig(t, "")
		req := r.redeem(t)
		r.svc.WithIdentity(fakeIdentity{})
		if _, err := r.svc.IdentityLink(req); !errors.Is(err, setup.ErrNoFirstAdmin) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("bad origin", func(t *testing.T) {
		r, _ := identityRig(t, "")
		req := r.redeem(t)
		r.svc.WithIdentity(fakeIdentity{fa: setup.FirstAdmin{Origin: "http://auth.platform.test"}, ok: true})
		if _, err := r.svc.IdentityLink(req); err == nil {
			t.Error("an http origin is accepted")
		}
	})
}

func TestEnterRefusals(t *testing.T) {
	setupRig := func(t *testing.T) (*rig, *time.Time, string) {
		r, now := identityRig(t, "")
		token, _ := r.link(t, r.redeem(t))
		return r, now, token
	}
	t.Run("reused", func(t *testing.T) {
		r, _, token := setupRig(t)
		if _, _, err := r.svc.Enter(authRequest(token), token); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.svc.Enter(authRequest(token), token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("second use: %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		r, now, token := setupRig(t)
		*now = now.Add(setupsession.HandoffLifetime)
		if _, _, err := r.svc.Enter(authRequest(token), token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("wrong host", func(t *testing.T) {
		r, _, token := setupRig(t)
		req := httptest.NewRequest(http.MethodGet, "https://platform.test/api/v1/setup/identity-enter", nil)
		if _, _, err := r.svc.Enter(req, token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("wrong mac", func(t *testing.T) {
		r, _, token := setupRig(t)
		bad := token[:len(token)-2] + "xx"
		if _, _, err := r.svc.Enter(authRequest(bad), bad); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("session cookie value is no token", func(t *testing.T) {
		r, _, _ := setupRig(t)
		v := setupsession.MintUntil([]byte(r.files.key), time.Now().Add(time.Hour))
		if _, _, err := r.svc.Enter(authRequest(v), v); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("retired session", func(t *testing.T) {
		r, _, token := setupRig(t)
		r.files.set(r.files.code, "")
		if _, _, err := r.svc.Enter(authRequest(token), token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("key file gone: %v", err)
		}
		r.files.set(r.files.code, "another key another key 0123456")
		if _, _, err := r.svc.Enter(authRequest(token), token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("key replaced: %v", err)
		}
	})
	t.Run("admin bound", func(t *testing.T) {
		r, _, token := setupRig(t)
		r.admins.bound = true
		if _, _, err := r.svc.Enter(authRequest(token), token); !errors.Is(err, setup.ErrHandoffRefused) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestGateFailsClosed(t *testing.T) {
	r, now := identityRig(t, "")
	token, _ := r.link(t, r.redeem(t))
	c, _, err := r.svc.Enter(authRequest(token), token)
	if err != nil {
		t.Fatal(err)
	}
	withCookie := func(c *http.Cookie) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/gate", nil)
		if c != nil {
			req.AddCookie(c)
		}
		return req
	}
	if !r.svc.GateOpen(withCookie(c)) {
		t.Fatal("a valid cookie is refused")
	}
	if r.svc.GateOpen(withCookie(nil)) {
		t.Error("no cookie passes")
	}
	if r.svc.GateOpen(withCookie(&http.Cookie{Name: c.Name, Value: "v1.9999999999.AAAA"})) {
		t.Error("a forged cookie passes")
	}
	if r.svc.GateOpen(withCookie(&http.Cookie{Name: "__Host-mp_setup", Value: c.Value})) {
		t.Error("the cookie under another name passes")
	}
	r.files.set(r.files.code, "")
	if r.svc.GateOpen(withCookie(c)) {
		t.Error("a gone key still passes")
	}
	r.files.set(r.files.code, "0123456789abcdef0123456789abcdef")
	*now = now.Add(setupsession.Lifetime)
	if r.svc.GateOpen(withCookie(c)) {
		t.Error("an expired cookie passes")
	}
	*now = now.Add(-setupsession.Lifetime)
	r.admins.bound = true
	if r.svc.GateOpen(withCookie(c)) {
		t.Error("the cookie passes after an admin is bound")
	}
}

func TestGateRouteIsOpenWhilePending(t *testing.T) {
	r, _ := identityRig(t, "")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := r.svc.Gate("/api/v1", next)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/setup/gate"}, {"GET", "/api/v1/setup/identity-enter"}, {"POST", "/api/v1/setup/identity-link"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s %s = %d, want it to pass the setup gate", tc.method, tc.path, rec.Code)
		}
	}
}

type brokenIdentity struct{}

func (brokenIdentity) FirstAdmin(context.Context) (setup.FirstAdmin, bool, error) {
	return setup.FirstAdmin{}, false, errors.New("lookup failed")
}

func TestStateFirstAdmin(t *testing.T) {
	cases := map[string]struct {
		identity setup.Identity
		session  bool
		bound    bool
		want     bool
		wantErr  bool
	}{
		"session and first_admin": {identity: authIdentity, session: true, want: true},
		"no session":              {identity: authIdentity},
		"module without block":    {identity: fakeIdentity{}, session: true},
		"no identity port":        {session: true},
		"done":                    {identity: authIdentity, session: true, bound: true},
		"failed lookup":           {identity: brokenIdentity{}, session: true, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, "")
			if tc.identity != nil {
				r.svc.WithIdentity(tc.identity)
			}
			req := request(http.MethodGet, "/api/v1/setup", "192.0.2.1")
			if tc.session {
				req = r.redeem(t)
			}
			r.admins.bound = tc.bound
			st, err := r.svc.State(req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error: %v", err, tc.wantErr)
			}
			if st.FirstAdmin != tc.want {
				t.Errorf("FirstAdmin = %v, want %v", st.FirstAdmin, tc.want)
			}
		})
	}
}
