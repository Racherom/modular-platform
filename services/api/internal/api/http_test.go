package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/services/api/internal/signin"
)

type noStore struct{ signin.Store }

func (noStore) Session(context.Context, []byte) (signin.Session, bool, error) {
	return signin.Session{}, false, nil
}

type noIDP struct{ signin.Provider }

func (noIDP) Ready() bool { return false }

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	cfg := signin.Config{PublicOrigin: "https://platform.localhost", Idle: time.Hour, Max: time.Hour}
	return NewHandler(Options{
		Signin: signin.New(cfg, noStore{}, noIDP{}, log), PublicOrigin: cfg.PublicOrigin, Idle: time.Hour, Log: log,
	})
}

// TestGuards covers AC-5 and AC-3: a mutating request from another origin is 403 even before
// the session is checked, and every route but the auth ones needs a session.
func TestGuards(t *testing.T) {
	t.Parallel()
	h := testHandler(t)
	for name, tc := range map[string]struct {
		method, path, origin string
		want                 int
		code                 string
	}{
		"read without session":       {http.MethodGet, "/api/v1/session", "", http.StatusUnauthorized, "unauthenticated"},
		"stream without session":     {http.MethodGet, "/api/v1/events", "", http.StatusUnauthorized, "unauthenticated"},
		"write without origin":       {http.MethodPost, "/api/v1/check", "", http.StatusForbidden, "bad_origin"},
		"write from another origin":  {http.MethodPost, "/api/v1/stacks/x/apply", "https://evil.example", http.StatusForbidden, "bad_origin"},
		"write, origin, no session":  {http.MethodPost, "/api/v1/check", "https://platform.localhost", http.StatusUnauthorized, "unauthenticated"},
		"logout from another origin": {http.MethodPost, "/api/v1/auth/logout", "https://evil.example", http.StatusForbidden, "bad_origin"},
		"login before discovery":     {http.MethodGet, "/api/v1/auth/login", "", http.StatusServiceUnavailable, "provider_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader("{}"))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("status %d %s, want %d %s", rec.Code, rec.Body, tc.want, tc.code)
			}
		})
	}
}

func TestCleanBody(t *testing.T) {
	t.Parallel()
	graph := `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"s","modules":{}}`
	for name, tc := range map[string]struct {
		path, body, wantCode string
	}{
		"check graph":              {"/api/v1/check", graph, ""},
		"check graph, unknown key": {"/api/v1/check", strings.Replace(graph, `"modules"`, `"x":1,"modules"`, 1), "invalid_graph"},
		"review":                   {"/api/v1/stacks/s/review", `{"graph":` + graph + `}`, ""},
		"review, unknown key":      {"/api/v1/stacks/s/review", `{"graph":` + graph + `,"digest":"x"}`, "invalid_request"},
		"apply":                    {"/api/v1/stacks/s/apply", `{"graph":` + graph + `,"digest":"x","placed":[]}`, ""},
		"apply, bad graph":         {"/api/v1/stacks/s/apply", `{"graph":{"kind":"Graph"},"digest":"x"}`, "unsupported_version"},
		"two objects":              {"/api/v1/stacks/s/apply", `{} {}`, "invalid_request"},
		// remove carries no graph, so its keys are not the graph envelope's
		"remove":           {"/api/v1/stacks/s/remove", `{"deleteData":false}`, ""},
		"remove with data": {"/api/v1/stacks/s/remove", `{"deleteData":true,"confirmName":"s"}`, ""},
	} {
		_, problem := cleanBody(tc.path, []byte(tc.body))
		got := ""
		if problem != nil {
			got = problem.Code
		}
		if got != tc.wantCode {
			t.Errorf("%s: code %q, want %q", name, got, tc.wantCode)
		}
	}
}
