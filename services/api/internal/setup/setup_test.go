package setup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
	"github.com/racherom/modular-platform/services/api/internal/setup"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

type fakeFiles struct {
	mu        sync.Mutex
	code, key string
}

func (f *fakeFiles) Code(context.Context) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.code, f.code != "", nil
}

func (f *fakeFiles) Key(context.Context) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []byte(f.key), f.key != "", nil
}

func (f *fakeFiles) set(code, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.code, f.key = code, key
}

type fakeKeeper struct {
	mu         sync.Mutex
	asks       []setup.Ask
	codeUsed   map[string]bool
	codeErr    error
	enrollErr  error
	gotSession string
	gotAnswers map[string]string
	doneErrs   []error // one per Done call; then success
	doneCalls  int
}

func (k *fakeKeeper) Enroll(context.Context) ([]setup.Ask, error) { return k.asks, nil }

func (k *fakeKeeper) CodeUsed(_ context.Context, sum string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.codeErr != nil {
		return k.codeErr
	}
	if k.codeUsed[sum] {
		return setup.ErrCodeUsed
	}
	if k.codeUsed == nil {
		k.codeUsed = map[string]bool{}
	}
	k.codeUsed[sum] = true
	return nil
}

func (k *fakeKeeper) RunEnroll(_ context.Context, session string, answers map[string]string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.gotSession, k.gotAnswers = session, answers
	return k.enrollErr
}

func (k *fakeKeeper) Done(context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.doneCalls++
	if k.doneCalls <= len(k.doneErrs) {
		return k.doneErrs[k.doneCalls-1]
	}
	return nil
}

type fakeAdmins struct {
	mu    sync.Mutex
	bound bool
}

func (a *fakeAdmins) AdminBinding(context.Context) (signin.Identity, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return signin.Identity{Issuer: "i", Subject: "s"}, a.bound, nil
}

type fakeProvider struct{ ready bool }

func (p fakeProvider) Ready() bool { return p.ready }

type rig struct {
	svc    *setup.Service
	files  *fakeFiles
	keeper *fakeKeeper
	admins *fakeAdmins
	logs   *bytes.Buffer
}

func newRig(t *testing.T, base string) *rig {
	t.Helper()
	r := &rig{files: &fakeFiles{code: "the-setup-code-0123456789", key: "0123456789abcdef0123456789abcdef"}, keeper: &fakeKeeper{}, admins: &fakeAdmins{}, logs: &bytes.Buffer{}}
	r.svc = setup.New(setup.Config{BasePath: base, Hops: 1}, r.files, r.keeper, r.admins, fakeProvider{ready: true}, slog.New(slog.NewJSONHandler(r.logs, nil)))
	return r
}

func request(method, target, remote string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("X-Forwarded-For", remote)
	return req
}

// redeem trades the code and returns a request that carries the cookie.
func (r *rig) redeem(t *testing.T) *http.Request {
	t.Helper()
	req := request(http.MethodPost, "/api/v1/setup/code", "192.0.2.1")
	c, err := r.svc.Redeem(req, r.files.code)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	next := request(http.MethodGet, "/api/v1/setup", "192.0.2.1")
	next.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	return next
}

func TestRedeemMintsTheCookie(t *testing.T) {
	for base, wantName := range map[string]string{"": "__Host-mp_setup", "/platform": "__Secure-mp_setup"} {
		r := newRig(t, base)
		c, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), r.files.code)
		if err != nil {
			t.Fatalf("base %q: %v", base, err)
		}
		wantPath := "/"
		if base != "" {
			wantPath = base + "/"
		}
		if c.Name != wantName || c.Path != wantPath || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
			t.Errorf("base %q: cookie = %+v", base, c)
		}
		if c.MaxAge < 24*60*60-5 || c.MaxAge > 24*60*60 {
			t.Errorf("max age = %d, want about 24 hours", c.MaxAge)
		}
		if _, err := setupsession.Verify([]byte(r.files.key), c.Value, time.Now()); err != nil {
			t.Errorf("the cookie value is no session: %v", err)
		}
		if strings.Contains(r.logs.String(), r.files.code) || strings.Contains(r.logs.String(), c.Value) {
			t.Errorf("the log holds the code or the cookie: %s", r.logs)
		}
	}
}

func TestRedeemRefusals(t *testing.T) {
	t.Run("wrong code", func(t *testing.T) {
		r := newRig(t, "")
		_, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), "nope")
		if !errors.Is(err, setup.ErrCodeRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("no code file", func(t *testing.T) {
		r := newRig(t, "")
		r.files.set("", r.files.key)
		_, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), "")
		if !errors.Is(err, setup.ErrCodeRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("used once", func(t *testing.T) {
		r := newRig(t, "")
		if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), r.files.code); err != nil {
			t.Fatal(err)
		}
		_, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.2"), r.files.code)
		if !errors.Is(err, setup.ErrCodeRefused) {
			t.Errorf("second redeem err = %v, want refused", err)
		}
	})
	t.Run("code changed", func(t *testing.T) {
		r := newRig(t, "")
		r.keeper.codeErr = setup.ErrCodeChanged
		if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), r.files.code); !errors.Is(err, setup.ErrCodeRefused) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("done at the keeper", func(t *testing.T) {
		r := newRig(t, "")
		r.keeper.codeErr = setup.ErrDone
		if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), r.files.code); !errors.Is(err, setup.ErrDone) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("admin bound", func(t *testing.T) {
		r := newRig(t, "")
		r.admins.bound = true
		if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.1"), r.files.code); !errors.Is(err, setup.ErrDone) {
			t.Errorf("err = %v", err)
		}
	})
}

// TestRedeemRateLimit: 10 tries a minute per client address, no total limit.
func TestRedeemRateLimit(t *testing.T) {
	r := newRig(t, "")
	for i := range 10 {
		if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.9"), "wrong"); !errors.Is(err, setup.ErrCodeRefused) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.9"), "wrong"); !errors.Is(err, setup.ErrTooManyTries) {
		t.Errorf("11th try err = %v, want too many tries", err)
	}
	// even the right code is refused for the blocked address, and another address still gets in
	if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.9"), r.files.code); !errors.Is(err, setup.ErrTooManyTries) {
		t.Errorf("right code from the blocked address: %v", err)
	}
	if _, err := r.svc.Redeem(request(http.MethodPost, "/", "192.0.2.10"), r.files.code); err != nil {
		t.Errorf("another address: %v", err)
	}
}

// TestSessionForgeries covers spec 0036 AC-37: only a MAC made with the current key counts.
func TestSessionForgeries(t *testing.T) {
	r := newRig(t, "")
	good := r.redeem(t)
	if !r.svc.Session(good) {
		t.Fatal("the cookie from the redemption is no session")
	}
	cookieName := setupsession.CookieName("")
	other := []byte("another-key-another-key-another!")
	expired := setupsession.MintUntil([]byte(r.files.key), time.Now().Add(-time.Minute))
	for name, value := range map[string]string{
		"wrong key": setupsession.MintUntil(other, time.Now().Add(time.Hour)),
		"expired":   expired,
		"garbage":   "v1.1.x",
		"empty":     "",
	} {
		req := request(http.MethodGet, "/api/v1/setup", "192.0.2.1")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: value})
		if r.svc.Session(req) {
			t.Errorf("%s: counted as a session", name)
		}
	}
	// a new key (a new code) ends every older session at once
	r.files.set(r.files.code, "a-brand-new-session-key-0000000")
	if r.svc.Session(good) {
		t.Error("a session survived a key change")
	}
	r.files.set("", "")
	if r.svc.Session(good) {
		t.Error("a session survived the deleted key")
	}
}

func TestState(t *testing.T) {
	r := newRig(t, "")
	r.keeper.asks = []setup.Ask{{Name: "admin-user", Description: "User."}}

	st, err := r.svc.State(request(http.MethodGet, "/api/v1/setup", "192.0.2.1"))
	if err != nil || !st.Pending || st.Session || st.ProviderReady || st.Enroll != nil || st.NoCode {
		t.Errorf("no session: %+v, %v", st, err)
	}
	st, err = r.svc.State(r.redeem(t))
	if err != nil || !st.Pending || !st.Session || !st.ProviderReady || len(st.Enroll) != 1 {
		t.Errorf("with session: %+v, %v", st, err)
	}
	r.files.set("", r.files.key)
	if st, _ = r.svc.State(request(http.MethodGet, "/api/v1/setup", "192.0.2.1")); !st.NoCode {
		t.Errorf("no code file: %+v", st)
	}
	r.admins.bound = true
	if st, _ = r.svc.State(request(http.MethodGet, "/api/v1/setup", "192.0.2.1")); st.Pending {
		t.Errorf("admin bound: %+v", st)
	}
}

func TestEnroll(t *testing.T) {
	r := newRig(t, "")
	answers := map[string]string{"admin-user": "ada"}
	if err := r.svc.Enroll(request(http.MethodPost, "/", "192.0.2.1"), answers); !errors.Is(err, setup.ErrNoSession) {
		t.Errorf("no session: %v", err)
	}
	req := r.redeem(t)
	if err := r.svc.Enroll(req, answers); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	cookie, _ := req.Cookie(setupsession.CookieName(""))
	if r.keeper.gotSession != cookie.Value || r.keeper.gotAnswers["admin-user"] != "ada" {
		t.Errorf("keeper got %q, %v", r.keeper.gotSession, r.keeper.gotAnswers)
	}
	r.keeper.enrollErr = &setup.InvalidAnswerError{Field: "admin-user", Rule: "pattern"}
	if _, bad := errors.AsType[*setup.InvalidAnswerError](r.svc.Enroll(req, answers)); !bad {
		t.Error("an invalid answer was not passed on")
	}
	r.admins.bound = true
	if err := r.svc.Enroll(req, answers); !errors.Is(err, setup.ErrDone) {
		t.Errorf("after the bind: %v", err)
	}
}

// TestGate covers spec 0036 AC-30: while pending only the setup and sign in routes answer.
func TestGate(t *testing.T) {
	r := newRig(t, "")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	gate := r.svc.Gate("/api/v1", next)
	serve := func(method, path string) int {
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/setup"},
		{"POST", "/api/v1/setup/code"},
		{"POST", "/api/v1/setup/enroll"},
		{"GET", "/api/v1/auth/login"},
		{"GET", "/api/v1/auth/callback"},
		{"GET", "/healthz"},
	} {
		if got := serve(rt.method, rt.path); got != http.StatusTeapot {
			t.Errorf("%s %s = %d while pending, want it open", rt.method, rt.path, got)
		}
	}
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/session"},
		{"GET", "/api/v1/catalog"},
		{"POST", "/api/v1/check"},
		{"GET", "/api/v1/events"},
		{"POST", "/api/v1/auth/logout"},
		{"POST", "/api/v1/setup"},
		{"GET", "/api/v1/setup/code"},
	} {
		if got := serve(rt.method, rt.path); got != http.StatusForbidden {
			t.Errorf("%s %s = %d while pending, want 403", rt.method, rt.path, got)
		}
	}
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	if body, _ := io.ReadAll(rec.Body); !strings.Contains(string(body), `"setup_pending"`) {
		t.Errorf("body = %s", body)
	}
	r.admins.bound = true
	if got := serve("GET", "/api/v1/catalog"); got != http.StatusTeapot {
		t.Errorf("after the bind = %d, want open", got)
	}
}

// TestDoneRetry covers spec 0036 AC-34: the done call is retried at 1 second doubling up to 5
// minutes until it answers, after a bind and at start when the code file is still there.
func TestDoneRetry(t *testing.T) {
	r := newRig(t, "")
	r.keeper.doneErrs = []error{errors.New("500"), errors.New("500"), errors.New("500")}
	var waits []time.Duration
	done := make(chan struct{})
	r.svc.SetWait(func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	})
	r.files.set("", r.files.key) // no code file at start: only the bind starts the calls
	r.admins.bound = true
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		r.svc.Run(ctx)
		close(done)
	}()
	r.svc.Bound()
	eventually(t, func() bool { r.keeper.mu.Lock(); defer r.keeper.mu.Unlock(); return r.keeper.doneCalls == 4 })
	cancel()
	<-done
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("wait %d = %v, want %v", i, waits[i], want[i])
		}
	}
	if !strings.Contains(r.logs.String(), `"setup done"`) || !strings.Contains(r.logs.String(), `"setup done failed"`) {
		t.Errorf("logs = %s", r.logs)
	}
}

func TestDoneBackoffCaps(t *testing.T) {
	r := newRig(t, "")
	for range 12 {
		r.keeper.doneErrs = append(r.keeper.doneErrs, errors.New("down"))
	}
	var waits []time.Duration
	r.svc.SetWait(func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	r.admins.bound = true
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.svc.Run(ctx); close(done) }() // the code file exists and an admin is bound: a start resume
	eventually(t, func() bool { r.keeper.mu.Lock(); defer r.keeper.mu.Unlock(); return r.keeper.doneCalls == 13 })
	cancel()
	<-done
	if got := waits[len(waits)-1]; got != 5*time.Minute {
		t.Errorf("last wait = %v, want 5m", got)
	}
}

func TestNoResumeWithoutCodeFile(t *testing.T) {
	r := newRig(t, "")
	r.admins.bound = true
	r.files.set("", r.files.key)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.svc.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if r.keeper.doneCalls != 0 {
		t.Errorf("done calls = %d, want 0", r.keeper.doneCalls)
	}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
