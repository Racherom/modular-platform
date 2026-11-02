package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk/auth"
)

// The tokens the tests hand out; each is at least 32 characters.
var (
	apiToken = strings.Repeat("a", 40)
	webToken = strings.Repeat("w", 40)
)

// clientsDir writes each file under a new directory, as Compose mounts a clients directory.
func clientsDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadClients covers the clients directory rules: one file per consumer instance, named after
// it, holding a token of 32 characters or more that no other file holds (covers: AC-7).
func TestLoadClients(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   map[string]string
		setup   func(t *testing.T, dir string) // adds what WriteFile cannot, such as a directory
		wantLen int
		wantErr []string // one substring per broken entry, in any order
	}{
		{
			name:    "one consumer per file, trailing whitespace trimmed",
			files:   map[string]string{"api": apiToken + "\n", "web": webToken + " \t\r\n"},
			wantLen: 2,
		},
		{
			name:    "hidden entries are skipped",
			files:   map[string]string{".keep": "", "..data": "x", "api": apiToken},
			wantLen: 1,
		},
		{
			name:    "a token of exactly 32 characters is long enough",
			files:   map[string]string{"api": strings.Repeat("a", 32)},
			wantLen: 1,
		},
		{
			name:    "a token of 31 characters is refused",
			files:   map[string]string{"api": strings.Repeat("a", 31)},
			wantErr: []string{"holds a token under 32 characters"},
		},
		{
			name:    "an empty or whitespace only file is refused",
			files:   map[string]string{"api": "", "web": " \n\n"},
			wantErr: []string{"api is empty", "web is empty"},
		},
		{
			name:    "a file name that is not an instance name is refused",
			files:   map[string]string{"API": apiToken, "web_1": webToken},
			wantErr: []string{"API: the file name must be", "web_1: the file name must be"},
		},
		{
			name:    "a token two files hold is refused for the second",
			files:   map[string]string{"api": apiToken, "web": apiToken + "\n"},
			wantLen: 1,
			wantErr: []string{"web holds the same token as"},
		},
		{
			name: "a directory is refused",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "api"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: []string{"api is not a regular file"},
		},
		{
			name: "a symlink to a token file counts",
			setup: func(t *testing.T, dir string) {
				target := filepath.Join(t.TempDir(), "token")
				if err := os.WriteFile(target, []byte(apiToken), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(dir, "api")); err != nil {
					t.Fatal(err)
				}
			},
			wantLen: 1,
		},
		{
			name: "a dangling symlink is refused",
			setup: func(t *testing.T, dir string) {
				if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "api")); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: []string{"api cannot be read"},
		},
		{
			name: "every broken entry is reported, the good ones still load",
			files: map[string]string{
				"api":   apiToken,
				"short": "abc",
				"Bad":   webToken,
				"empty": "",
			},
			wantLen: 1,
			wantErr: []string{"short holds a token under", "Bad: the file name", "empty is empty"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := clientsDir(t, tt.files)
			if tt.setup != nil {
				tt.setup(t, dir)
			}

			c, errs := auth.LoadClients(dir)

			if c.Len() != tt.wantLen {
				t.Errorf("Len() = %d, want %d", c.Len(), tt.wantLen)
			}
			if len(errs) != len(tt.wantErr) {
				t.Fatalf("errors = %v, want %d", errs, len(tt.wantErr))
			}
			for _, want := range tt.wantErr {
				if !anyContains(errs, want) {
					t.Errorf("errors = %v, want one containing %q", errs, want)
				}
			}
			for _, err := range errs {
				for _, content := range tt.files {
					if token := strings.TrimSpace(content); len(token) > 2 && strings.Contains(err.Error(), token) {
						t.Errorf("error %q shows a token", err)
					}
				}
			}
		})
	}
}

// TestLoadClientsUnreadableDir covers a clients directory that cannot be read: one error naming
// it.
func TestLoadClientsUnreadableDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "missing")

	c, errs := auth.LoadClients(dir)

	if c.Len() != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "the clients directory "+dir+" cannot be read") {
		t.Errorf("LoadClients = %d clients, %v; want none and one error naming %s", c.Len(), errs, dir)
	}
}

// TestMiddleware covers the binding token check: a call carrying one consumer's token reaches the
// handler with that consumer as CallerModule; anything else gets 401 with one unauthorized problem
// and never reaches it (covers: AC-7).
func TestMiddleware(t *testing.T) {
	t.Parallel()
	c, errs := auth.LoadClients(clientsDir(t, map[string]string{"api": apiToken + "\n", "web": webToken}))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	h := auth.Middleware(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := auth.CallerModule(r.Context())
		if !ok {
			name = "no caller module"
		}
		_, _ = io.WriteString(w, name)
	}))

	tests := []struct {
		name       string
		header     string // Authorization; empty sends none
		wantStatus int
		wantBody   string
	}{
		{"the api token is the api consumer", "Bearer " + apiToken, http.StatusOK, "api"},
		{"the web token is the web consumer", "Bearer " + webToken, http.StatusOK, "web"},
		{"no header", "", http.StatusUnauthorized, ""},
		{"an empty bearer", "Bearer ", http.StatusUnauthorized, ""},
		{"an unknown token", "Bearer " + strings.Repeat("x", 40), http.StatusUnauthorized, ""},
		{"a truncated token", "Bearer " + apiToken[:39], http.StatusUnauthorized, ""},
		{"a token with extra text", "Bearer " + apiToken + "x", http.StatusUnauthorized, ""},
		{"the token with its trailing newline", "Bearer " + apiToken + "\n", http.StatusUnauthorized, ""},
		{"the token without the Bearer scheme", apiToken, http.StatusUnauthorized, ""},
		{"the token as Basic", "Basic " + apiToken, http.StatusUnauthorized, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/hello", nil)
			if tt.header != "" {
				r.Header["Authorization"] = []string{tt.header} // set raw, so a newline stays in
			}
			w := httptest.NewRecorder()

			h.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				if w.Body.String() != tt.wantBody {
					t.Errorf("CallerModule = %q, want %q", w.Body.String(), tt.wantBody)
				}
				return
			}
			assertUnauthorized(t, w)
		})
	}
}

// TestMiddlewareWithoutClients covers a provider whose clients directory holds no consumer: every
// call is refused, an empty bearer too.
func TestMiddlewareWithoutClients(t *testing.T) {
	t.Parallel()
	c, _ := auth.LoadClients(t.TempDir())
	reached := false
	h := auth.Middleware(c, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	for _, header := range []string{"", "Bearer ", "Bearer " + apiToken} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()

		h.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", header, w.Code)
		}
	}
	if reached {
		t.Error("the handler ran without a consumer")
	}
}

// TestCallerModuleOutsideMiddleware covers a context the middleware never saw: no caller module.
func TestCallerModuleOutsideMiddleware(t *testing.T) {
	t.Parallel()

	name, ok := auth.CallerModule(t.Context())

	if ok || name != "" {
		t.Errorf("CallerModule = %q, %v; want \"\", false", name, ok)
	}
}

// assertUnauthorized checks the refusal: WWW-Authenticate Bearer and one unauthorized problem
// that shows no token.
func assertUnauthorized(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	var body struct {
		Problems []struct{ Code string } `json:"problems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not a problem list: %v", w.Body.String(), err)
	}
	if len(body.Problems) != 1 || body.Problems[0].Code != "unauthorized" {
		t.Errorf("problems = %+v, want one unauthorized", body.Problems)
	}
	if strings.Contains(w.Body.String(), apiToken) || strings.Contains(w.Body.String(), webToken) {
		t.Errorf("the refusal shows a token: %s", w.Body.String())
	}
}

func anyContains(errs []error, sub string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), sub) {
			return true
		}
	}
	return false
}
