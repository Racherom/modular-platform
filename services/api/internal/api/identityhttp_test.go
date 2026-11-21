package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
	"github.com/racherom/modular-platform/services/api/internal/setup"
)

type stubIdentity struct{ ok bool }

func (s stubIdentity) FirstAdmin(_ context.Context) (setup.FirstAdmin, bool, error) {
	return setup.FirstAdmin{Origin: "https://auth.platform.test", Path: "/setup"}, s.ok, nil
}

func identityMux(t *testing.T, id stubIdentity, bound bool) (*http.ServeMux, *bytes.Buffer, stubFiles) {
	t.Helper()
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	files := stubFiles{code: "the-code-0123456789abcdef", key: "0123456789abcdef0123456789abcdef"}
	svc := setup.New(setup.Config{Hops: 1}, files, stubKeeper{}, stubAdmins{bound: bound}, stubProvider{}, log).WithIdentity(id)
	mux := http.NewServeMux()
	setupRoutes{svc: svc, origin: testOrigin, log: log}.mount(mux, "/api/v1")
	return mux, &logs, files
}

func serve(mux http.Handler, method, target, host string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if host != "" {
		req.Host = host
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIdentityRoutes(t *testing.T) {
	mux, logs, _ := identityMux(t, stubIdentity{ok: true}, false)

	// the link needs the origin and a session
	if rec := post(mux, "/api/v1/setup/identity-link", "", ""); rec.Code != 403 {
		t.Errorf("no origin = %d, want 403", rec.Code)
	}
	rec := post(mux, "/api/v1/setup/identity-link", testOrigin, "")
	if code, _ := problemCode(t, rec); rec.Code != 403 || code != "setup_pending" {
		t.Errorf("no session = %d %s", rec.Code, code)
	}

	rec = post(mux, "/api/v1/setup/code", testOrigin, `{"code":"the-code-0123456789abcdef"}`)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		session = c
	}
	if rec.Code != 204 || session == nil {
		t.Fatalf("redeem = %d", rec.Code)
	}
	rec = post(mux, "/api/v1/setup/identity-link", testOrigin, "", session)
	var out struct{ URL string }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.URL, "https://auth.platform.test/api/v1/setup/identity-enter?t=") {
		t.Fatalf("link = %d %s", rec.Code, rec.Body)
	}
	u, _ := url.Parse(out.URL)
	enter := u.RequestURI()

	// no cookie: the gate refuses
	if rec := serve(mux, "GET", "/api/v1/setup/gate", ""); rec.Code != 403 {
		t.Errorf("gate without a cookie = %d", rec.Code)
	}

	// a token on the platform's own host is refused; no cookie comes out
	if rec := serve(mux, "GET", enter, "platform.test"); rec.Code != 403 || len(rec.Result().Cookies()) != 0 {
		t.Errorf("wrong host = %d", rec.Code)
	}

	rec = serve(mux, "GET", enter, "auth.platform.test")
	if rec.Code != 302 || rec.Header().Get("Location") != "https://auth.platform.test/setup" {
		t.Fatalf("enter = %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", rec.Header())
	}
	var gate *http.Cookie
	for _, c := range rec.Result().Cookies() {
		gate = c
	}
	if gate == nil || gate.Name != setupsession.GateCookieName() || gate.Domain != "" || !gate.Secure || !gate.HttpOnly || gate.Path != "/" {
		t.Fatalf("cookie = %+v", gate)
	}

	// the token works once
	rec = serve(mux, "GET", enter, "auth.platform.test")
	if code, _ := problemCode(t, rec); rec.Code != 403 || code != "handoff_refused" {
		t.Errorf("reuse = %d %s", rec.Code, code)
	}

	// the cookie opens the gate
	if rec := serve(mux, "GET", "/api/v1/setup/gate", "", gate); rec.Code != 204 {
		t.Errorf("gate with the cookie = %d", rec.Code)
	}
	for _, leak := range []string{u.Query().Get("t"), gate.Value, session.Value} {
		if strings.Contains(logs.String(), leak) {
			t.Errorf("the log holds a token or cookie value")
		}
	}
}

func TestIdentityRoutesAfterTheBind(t *testing.T) {
	mux, _, _ := identityMux(t, stubIdentity{ok: true}, true)
	if rec := serve(mux, "GET", "/api/v1/setup/gate", "", &http.Cookie{Name: setupsession.GateCookieName(), Value: setupsession.MintUntil([]byte("0123456789abcdef0123456789abcdef"), time.Now().Add(time.Hour))}); rec.Code != 403 {
		t.Errorf("gate after the bind = %d, want 403", rec.Code)
	}
	if rec := post(mux, "/api/v1/setup/identity-link", testOrigin, ""); rec.Code != 409 {
		t.Errorf("link after the bind = %d, want 409", rec.Code)
	}
}

func TestIdentityLinkWithoutFirstAdmin(t *testing.T) {
	mux, _, _ := identityMux(t, stubIdentity{}, false)
	rec := post(mux, "/api/v1/setup/code", testOrigin, `{"code":"the-code-0123456789abcdef"}`)
	session := rec.Result().Cookies()[0]
	rec = post(mux, "/api/v1/setup/identity-link", testOrigin, "", session)
	if code, _ := problemCode(t, rec); rec.Code != 404 || code != "no_first_admin" {
		t.Errorf("= %d %s", rec.Code, code)
	}
}
