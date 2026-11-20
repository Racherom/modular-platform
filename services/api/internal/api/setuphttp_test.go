package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/services/api/internal/setup"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

type stubFiles struct{ code, key string }

func (f stubFiles) Code(context.Context) (string, bool, error) { return f.code, f.code != "", nil }
func (f stubFiles) Key(context.Context) ([]byte, bool, error)  { return []byte(f.key), f.key != "", nil }

type stubKeeper struct{ enrollErr error }

func (stubKeeper) Enroll(context.Context) ([]setup.Ask, error) {
	return []setup.Ask{{Name: "admin-user", Description: "User name.", MinLength: 3}}, nil
}
func (stubKeeper) CodeUsed(context.Context, string) error { return nil }
func (k stubKeeper) RunEnroll(context.Context, string, map[string]string) error {
	return k.enrollErr
}
func (stubKeeper) Done(context.Context) error { return nil }

type stubAdmins struct{ bound bool }

func (a stubAdmins) AdminBinding(context.Context) (signin.Identity, bool, error) {
	return signin.Identity{}, a.bound, nil
}

type stubProvider struct{}

func (stubProvider) Ready() bool { return true }

const testOrigin = "https://platform.test"

func setupMux(t *testing.T, k stubKeeper, bound bool) (*http.ServeMux, *bytes.Buffer, stubFiles) {
	t.Helper()
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	files := stubFiles{code: "the-code-0123456789abcdef", key: "0123456789abcdef0123456789abcdef"}
	svc := setup.New(setup.Config{Hops: 1}, files, k, stubAdmins{bound: bound}, stubProvider{}, log)
	mux := http.NewServeMux()
	setupRoutes{svc: svc, origin: testOrigin, log: log}.mount(mux, "/api/v1")
	return mux, &logs, files
}

func post(mux http.Handler, path, origin, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var pl struct {
		Problems []struct{ Code, Path string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pl); err != nil || len(pl.Problems) != 1 {
		t.Fatalf("body = %s", rec.Body)
	}
	return pl.Problems[0].Code, pl.Problems[0].Path
}

func TestSetupRoutes(t *testing.T) {
	mux, logs, files := setupMux(t, stubKeeper{}, false)

	get := func(cookies ...*http.Cookie) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("GET setup = %d %s", rec.Code, rec.Body)
		}
		return out
	}
	if out := get(); out["state"] != "pending" || out["enroll"] != nil || out["providerReady"] != nil {
		t.Errorf("no session: %v", out)
	}

	if rec := post(mux, "/api/v1/setup/code", "https://evil.test", `{"code":"x"}`); rec.Code != 403 {
		t.Errorf("foreign origin = %d, want 403", rec.Code)
	}
	if rec := post(mux, "/api/v1/setup/code", "", `{"code":"x"}`); rec.Code != 403 {
		t.Errorf("no origin = %d, want 403", rec.Code)
	}
	rec := post(mux, "/api/v1/setup/code", testOrigin, `{"code":"wrong"}`)
	if code, _ := problemCode(t, rec); rec.Code != 401 || code != "code_refused" {
		t.Errorf("wrong code = %d %s", rec.Code, code)
	}
	if rec := post(mux, "/api/v1/setup/code", testOrigin, `{"code":"x","extra":1}`); rec.Code != 400 {
		t.Errorf("unknown key = %d, want 400", rec.Code)
	}
	rec = post(mux, "/api/v1/setup/code", testOrigin, `{"code":"`+files.code+`"}`)
	if rec.Code != 204 {
		t.Fatalf("right code = %d %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-mp_setup" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("cookies = %v", cookies)
	}
	out := get(cookies[0])
	asks, _ := out["enroll"].(map[string]any)["asks"].([]any)
	if out["session"] != true || out["providerReady"] != true || len(asks) != 1 {
		t.Errorf("with session: %v", out)
	}
	if rec := post(mux, "/api/v1/setup/enroll", testOrigin, `{"answers":{"admin-user":"ada"}}`); rec.Code != 401 {
		t.Errorf("enroll without cookie = %d, want 401", rec.Code)
	}
	if rec := post(mux, "/api/v1/setup/enroll", testOrigin, `{"answers":{"admin-user":"ada"}}`, cookies[0]); rec.Code != 204 {
		t.Errorf("enroll = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(logs.String(), files.code) || strings.Contains(logs.String(), cookies[0].Value) || strings.Contains(logs.String(), "ada") {
		t.Errorf("the log holds a code, a session or an answer: %s", logs)
	}
}

func TestSetupEnrollErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
		path   string
	}{
		"invalid": {&setup.InvalidAnswerError{Field: "admin-user", Rule: "min_length"}, 422, "invalid_answer", "answers.admin-user"},
		"failed":  {setup.ErrEnrollFailed, 502, "enroll_failed", ""},
		"busy":    {setup.ErrBusy, 503, "busy", ""},
		"no":      {setup.ErrNoEnroll, 404, "no_enroll", ""},
		"session": {setup.ErrNoSession, 401, "no_session", ""},
		"done":    {setup.ErrDone, 409, "setup_done", ""},
	} {
		t.Run(name, func(t *testing.T) {
			mux, _, files := setupMux(t, stubKeeper{enrollErr: tc.err}, false)
			redeem := post(mux, "/api/v1/setup/code", testOrigin, `{"code":"`+files.code+`"}`)
			rec := post(mux, "/api/v1/setup/enroll", testOrigin, `{"answers":{"admin-user":"a"}}`, redeem.Result().Cookies()...)
			code, path := problemCode(t, rec)
			if rec.Code != tc.status || code != tc.code || path != tc.path {
				t.Errorf("= %d %s %q, want %d %s %q", rec.Code, code, path, tc.status, tc.code, tc.path)
			}
			if body, _ := io.ReadAll(rec.Body); strings.Contains(string(body), "a\"") && name == "invalid" {
				t.Errorf("the answer came back: %s", body)
			}
		})
	}
}

func TestSetupDoneState(t *testing.T) {
	mux, _, files := setupMux(t, stubKeeper{}, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != `{"state":"done"}` {
		t.Errorf("body = %s", rec.Body)
	}
	if rec := post(mux, "/api/v1/setup/code", testOrigin, `{"code":"`+files.code+`"}`); rec.Code != 409 {
		t.Errorf("code after the bind = %d, want 409", rec.Code)
	}
	if rec := post(mux, "/api/v1/setup/enroll", testOrigin, `{"answers":{}}`); rec.Code != 409 {
		t.Errorf("enroll after the bind = %d, want 409", rec.Code)
	}
}
