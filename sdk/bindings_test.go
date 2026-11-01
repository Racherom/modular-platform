package sdk_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpclient"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
)

// syncBuffer is a log buffer the readiness polls may write to while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// provider serves the test module as a provider whose clients directory holds tokens. Its
// handler answers with the consumer instance the binding token proved.
func provider(t *testing.T, tokens map[string]string) *httptest.Server {
	t.Helper()
	env := baseEnv(t)
	env["TM_CLIENTS_DIR"] = clientsDir(t, tokens)
	h, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name, _ := sdk.CallerModule(r.Context())
			_, _ = io.WriteString(w, name)
		}), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// consumerUses builds the test module as a consumer of echo with env added to baseEnv, and
// returns its Uses and its logs.
func consumerUses(t *testing.T, env map[string]string) (testmodule.Uses, *syncBuffer, error) {
	t.Helper()
	base := baseEnv(t)
	for k, v := range env {
		base[k] = v
	}
	var (
		logs syncBuffer
		uses testmodule.Uses
	)
	_, err := sdk.Build(t.Context(), testmodule.Module, func(_ context.Context, m *tmModule) (http.Handler, error) {
		uses = m.Uses
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(base), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return uses, &logs, err
}

func get(t *testing.T, c *httpclient.Client, path string) (int, string) {
	t.Helper()
	req, err := c.NewRequest(t.Context(), http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestHTTPBinding covers the HTTP part of AC-6: the generated getter gives a client that reaches
// the provider (a running sdk module) with the binding's token, which proves the consumer.
func TestHTTPBinding(t *testing.T) {
	t.Parallel()
	srv := provider(t, map[string]string{"consumer": consumerToken})
	envFile := file(t, "TM_ECHO_URL="+srv.URL+"\n")
	uses, logs, err := consumerUses(t, map[string]string{
		"MP_ENV_FILE":        envFile,
		"TM_ECHO_TOKEN_FILE": file(t, consumerToken+"\n"),
	})
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	echo, err := uses.Echo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status, body := get(t, echo, "/hello?x=1"); status != http.StatusOK || body != "consumer" {
		t.Errorf("call = %d %q, want 200 consumer", status, body)
	}
	for _, out := range []string{fmt.Sprintf("%+v", echo), fmt.Sprintf("%#v", uses), logs.String()} {
		if strings.Contains(out, consumerToken) {
			t.Errorf("the token shows in %q", out)
		}
	}
}

// TestUnboundOptional covers an optional binding the graph left unbound: the module starts and
// the getter says so.
func TestUnboundOptional(t *testing.T) {
	t.Parallel()
	uses, logs, err := consumerUses(t, nil)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	if _, err := uses.Echo(t.Context()); !errors.Is(err, sdk.ErrUnbound) {
		t.Errorf("err = %v, want sdk.ErrUnbound", err)
	}
}

// TestBindingProblems covers a bad binding key stopping the start with a line naming it.
func TestBindingProblems(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env  func(t *testing.T) map[string]string
		want string
	}{
		"url not absolute": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_ECHO_URL": "provider:8080"} },
			want: "config: uses.echo.url (TM_ECHO_URL): invalid: ",
		},
		"url not http": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_ECHO_URL": "ftp://provider"} },
			want: "config: uses.echo.url (TM_ECHO_URL): invalid: ",
		},
		"empty token file": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_ECHO_URL": "http://provider:8080", "TM_ECHO_TOKEN_FILE": file(t, "\n")}
			},
			want: "config: uses.echo.token_file (TM_ECHO_TOKEN_FILE): missing: ",
		},
		"token file missing": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_ECHO_URL": "http://provider:8080", "TM_ECHO_TOKEN_FILE": filepath.Join(t.TempDir(), "none")}
			},
			want: "config: uses.echo.token_file (TM_ECHO_TOKEN_FILE): invalid: ",
		},
		"postgresql user missing": {
			env:  dataEnv(map[string]string{"TM_DB_USER": ""}),
			want: "config: uses.postgresql.user (TM_DB_USER): missing: ",
		},
		"postgresql password file unset": {
			env:  dataEnv(map[string]string{"TM_DB_PASSWORD_FILE": ""}),
			want: "config: uses.postgresql.password_file (TM_DB_PASSWORD_FILE): missing: ",
		},
		"postgresql sslmode not allowed": {
			env:  dataEnv(map[string]string{"TM_DB_SSLMODE": "require"}),
			want: "config: uses.postgresql.sslmode (TM_DB_SSLMODE): invalid_binding: ",
		},
		"postgresql url not postgres": {
			env:  dataEnv(map[string]string{"TM_DB_URL": "http://db:5432"}),
			want: "config: uses.postgresql.url (TM_DB_URL): invalid: ",
		},
		"postgresql url with credentials": {
			env:  dataEnv(map[string]string{"TM_DB_URL": "postgres://app:pw@db:5432"}),
			want: "config: uses.postgresql.url (TM_DB_URL): invalid: ",
		},
		"postgresql url with a database": {
			env:  dataEnv(map[string]string{"TM_DB_URL": "postgres://db:5432/app"}),
			want: "config: uses.postgresql.url (TM_DB_URL): invalid: ",
		},
		"postgresql ca file without a certificate": {
			env: func(t *testing.T) map[string]string {
				env := dataEnv(nil)(t)
				env["TM_DB_CA_FILE"] = file(t, "not a certificate\n")
				return env
			},
			want: "config: uses.postgresql.ca_file (TM_DB_CA_FILE): invalid: ",
		},
		"s3 bucket missing": {
			env:  dataEnv(map[string]string{"TM_STORE_BUCKET": ""}),
			want: "config: uses.storage.bucket (TM_STORE_BUCKET): missing: ",
		},
		"s3 secret key file empty": {
			env: func(t *testing.T) map[string]string {
				env := dataEnv(nil)(t)
				env["TM_STORE_SECRET_KEY_FILE"] = file(t, "\n")
				return env
			},
			want: "config: uses.storage.secret_access_key_file (TM_STORE_SECRET_KEY_FILE): missing: ",
		},
		"s3 url not http": {
			env:  dataEnv(map[string]string{"TM_STORE_URL": "s3://store"}),
			want: "config: uses.storage.url (TM_STORE_URL): invalid: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, logs, err := consumerUses(t, tc.env(t))
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			var lines []string
			for _, l := range logLines(t, bytes.NewBufferString(logs.String())) {
				if l.Level == "ERROR" {
					lines = append(lines, l.Msg)
				}
			}
			if len(lines) != 1 || !strings.HasPrefix(lines[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", lines, tc.want)
			}
		})
	}
}

// Credentials of the data bindings in dataEnv; no test may find them in a log line.
const (
	dbPassword = "db-password-0123456789"
	s3Secret   = "s3-secret-key-0123456789"
)

// dataEnv binds the test module's postgresql and storage bindings to hosts that need not exist,
// then applies over (an empty value unsets a key).
func dataEnv(over map[string]string) func(t *testing.T) map[string]string {
	return func(t *testing.T) map[string]string {
		env := map[string]string{
			"TM_DB_URL":                "postgres://db.test:5432",
			"TM_DB_NAME":               "app",
			"TM_DB_USER":               "app",
			"TM_DB_PASSWORD_FILE":      file(t, dbPassword+"\n"),
			"TM_DB_SSLMODE":            "disable",
			"TM_STORE_URL":             "http://store.test:9000",
			"TM_STORE_BUCKET":          "files",
			"TM_STORE_ACCESS_KEY_ID":   "access",
			"TM_STORE_SECRET_KEY_FILE": file(t, s3Secret+"\n"),
		}
		for k, v := range over {
			env[k] = v
		}
		return env
	}
}

// TestDataBindings covers the getters of the postgresql and s3 bindings without a server: each
// gives its client built from the binding's keys, and neither credential reaches the logs.
func TestDataBindings(t *testing.T) {
	t.Parallel()
	uses, logs, err := consumerUses(t, dataEnv(map[string]string{"TM_STORE_REGION": "eu-west-1"})(t))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	pool, err := uses.Postgresql(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cc := pool.Config().ConnConfig
	if cc.Host != "db.test" || cc.Port != 5432 || cc.Database != "app" || cc.User != "app" || cc.Password != dbPassword || cc.TLSConfig != nil {
		t.Errorf("pool config = host %q port %d database %q user %q tls %v", cc.Host, cc.Port, cc.Database, cc.User, cc.TLSConfig != nil)
	}
	store, err := uses.Storage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if store.Bucket != "files" || store.EndpointURL().Host != "store.test:9000" {
		t.Errorf("s3 client = %v", store)
	}
	for _, out := range []string{logs.String(), fmt.Sprintf("%v %+v", store, store)} {
		if strings.Contains(out, dbPassword) || strings.Contains(out, s3Secret) {
			t.Errorf("a credential shows: %s", out)
		}
	}
}

// TestVerifyFull covers sslmode verify-full: the pool speaks TLS to the url's host and trusts the
// binding's ca_file.
func TestVerifyFull(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	uses, logs, err := consumerUses(t, dataEnv(map[string]string{"TM_DB_SSLMODE": "verify-full", "TM_DB_CA_FILE": file(t, string(ca))})(t))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	pool, err := uses.Postgresql(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tc := pool.Config().ConnConfig.TLSConfig
	if tc == nil || tc.ServerName != "db.test" || tc.RootCAs == nil || tc.InsecureSkipVerify {
		t.Fatalf("tls config = %+v", tc)
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{Roots: tc.RootCAs}); err != nil {
		t.Errorf("the pool does not trust the ca_file: %v", err)
	}
}

func problemCodes(t *testing.T, body []byte) []string {
	t.Helper()
	var list struct{ Problems []struct{ Code string } }
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	var codes []string
	for _, p := range list.Problems {
		codes = append(codes, p.Code)
	}
	return codes
}

// TestBindingAuth covers AC-7: no token, a token not in the clients directory, and a malformed
// header each get 401 with one unauthorized problem; health is open; a valid token sets
// CallerModule to the consumer's instance; removing one consumer's file refuses only it.
func TestBindingAuth(t *testing.T) {
	t.Parallel()
	const otherToken = "other-token-abcdefghijklmnopqrstuvwxyz"
	srv := provider(t, map[string]string{"consumer": consumerToken, "other": otherToken})

	call := func(srv *httptest.Server, path, auth string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}

	for name, auth := range map[string]string{
		"no token":                "",
		"another binding's token": "Bearer some-other-binding-token-0123456789",
		"token as basic auth":     "Basic " + consumerToken,
		"token prefix":            "Bearer " + consumerToken[:len(consumerToken)-1],
	} {
		status, body := call(srv, "/x", auth)
		if status != http.StatusUnauthorized || fmt.Sprint(problemCodes(t, body)) != "[unauthorized]" {
			t.Errorf("%s: %d %s, want 401 with one unauthorized problem", name, status, body)
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if status, _ := call(srv, path, ""); status != http.StatusOK {
			t.Errorf("%s without a token = %d, want 200", path, status)
		}
	}
	for token, want := range map[string]string{consumerToken: "consumer", otherToken: "other"} {
		if status, body := call(srv, "/x", "Bearer "+token); status != http.StatusOK || string(body) != want {
			t.Errorf("valid token: %d %q, want 200 %q", status, body, want)
		}
	}

	// after a restart without other's file, only other is refused
	restarted := provider(t, map[string]string{"consumer": consumerToken})
	if status, _ := call(restarted, "/x", "Bearer "+otherToken); status != http.StatusUnauthorized {
		t.Errorf("removed consumer = %d, want 401", status)
	}
	if status, _ := call(restarted, "/x", "Bearer "+consumerToken); status != http.StatusOK {
		t.Errorf("kept consumer = %d, want 200", status)
	}
}

// TestClientsDirProblems covers the clients directory rules: each broken entry is an
// invalid_binding line, and an empty directory refuses the start.
func TestClientsDirProblems(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"empty directory": {files: nil, want: "config: provides.greet.clients_dir (TM_CLIENTS_DIR): missing: "},
		"short token":     {files: map[string]string{"api": "short"}, want: "config: provides.greet.clients_dir (TM_CLIENTS_DIR): invalid_binding: "},
		"bad file name":   {files: map[string]string{"Api": consumerToken}, want: "config: provides.greet.clients_dir (TM_CLIENTS_DIR): invalid_binding: "},
		"same token twice": {
			files: map[string]string{"a": consumerToken, "b": consumerToken},
			want:  "config: provides.greet.clients_dir (TM_CLIENTS_DIR): invalid_binding: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := baseEnv(t)
			env["TM_CLIENTS_DIR"] = clientsDir(t, tc.files)
			_, logs, err := start(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			msgs := errorMsgs(t, logs)
			if len(msgs) != 1 || !strings.HasPrefix(msgs[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", msgs, tc.want)
			}
			if strings.Contains(logs.String(), consumerToken) {
				t.Error("a token reached the log")
			}
		})
	}
}

// TestHiddenAndLinkedClients covers the entries a clients directory may hold besides plain
// files: hidden entries are skipped, and a symlink to a file counts like the file.
func TestHiddenAndLinkedClients(t *testing.T) {
	t.Parallel()
	dir := clientsDir(t, map[string]string{".hidden": "x"})
	target := file(t, consumerToken)
	if err := os.Symlink(target, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	env := baseEnv(t)
	env["TM_CLIENTS_DIR"] = dir
	if _, logs, err := start(t, env); err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
}

// requiredEcho declares one required http binding, for the readiness tests: the test module's
// own binding is optional, and readiness waits on required ones only.
var requiredEcho = sdk.Declaration[struct{}, struct{}]{
	Name:     "ready",
	Bindings: []sdk.BindingSpec{{Capability: "echo", Client: "http", Env: map[string]string{"url": "R_ECHO_URL"}, Open: httpclient.Open}},
	New:      func(*sdk.Values, *sdk.Bindings) (struct{}, struct{}) { return struct{}{}, struct{}{} },
}

func readyz(t *testing.T, h http.Handler) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
	return rec.Code
}

// TestReadiness covers AC-9 for http bindings: /readyz answers 503 while a required provider
// answers 500 or more, logging a warning per failed poll; the module still starts, and turns ready
// once the provider answers below 500 (a 404 counts).
func TestReadiness(t *testing.T) {
	t.Parallel()
	var up atomic.Bool
	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() || r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(providerSrv.Close)

	var logs syncBuffer
	h, err := sdk.Build(t.Context(), requiredEcho, func(context.Context, *sdk.Module[struct{}, struct{}]) (http.Handler, error) {
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(map[string]string{"MP_INSTANCE": "ready", "R_ECHO_URL": providerSrv.URL}),
		sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs.String())
	}
	if code := readyz(t, h); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz while the provider is down = %d, want 503", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), `"msg":"binding not ready","capability":"echo"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no binding not ready warning:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	up.Store(true)
	for readyz(t, h) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("/readyz stayed 503 after the provider answered")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
