package sdk_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
)

// callerKeyFiles writes an Ed25519 key pair as an MP_CALLER_SIGNING_KEY_FILE and a public key
// file and returns their paths.
func callerKeyFiles(t *testing.T) (signing, public string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := x509.MarshalPKCS8PrivateKey(key)
	pkix, _ := x509.MarshalPKIXPublicKey(pub)
	return file(t, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}))),
		file(t, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix})))
}

// seen is what the caller provider answers: the caller and the calling module it saw.
type seen struct {
	Caller *sdk.Caller
	Module string
}

// callerKeysDir writes a caller keys directory, one file per consumer with its public key file's
// content, and returns its path.
func callerKeysDir(t *testing.T, keys map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for consumer, public := range keys {
		data, err := os.ReadFile(public)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, consumer), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// callerProvider builds the test module as a provider named localhost (its host name, since
// MP_DOMAIN is unset) that checks MP-Caller with the caller keys directory dir, serves it, and
// returns its port. Its one consumer is the instance caller.
func callerProvider(t *testing.T, dir string) (port string, logs *syncBuffer) {
	t.Helper()
	env := baseEnv(t)
	env["MP_INSTANCE"] = "localhost"
	env["TM_CALLER_KEYS_DIR"] = dir
	logs = &syncBuffer{}
	h, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var s seen
			if c, ok := sdk.CallerFrom(r.Context()); ok {
				s.Caller = &c
			}
			s.Module, _ = sdk.CallerModule(r.Context())
			_ = json.NewEncoder(w).Encode(s)
		}), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL[strings.LastIndex(srv.URL, ":")+1:], logs
}

// TestCallerEndToEnd covers AC-11 across two modules on the sdk: a consumer holding the signing
// key (as api does) sends the caller its code put into ctx as an MP-Caller token on its http
// binding, minted as its own instance for the provider's host name; the provider, holding the
// public key, puts it into ctx next to the binding token's module. A call without a caller in
// ctx carries none, and a token minted for another host name (the binding URL names the provider
// by IP) is refused with 401. So is a token signed with a key that is not the caller's own, even
// one the provider holds for another consumer (spec 0015, AC-18).
func TestCallerEndToEnd(t *testing.T) {
	t.Parallel()
	signing, public := callerKeyFiles(t)
	otherSigning, otherPublic := callerKeyFiles(t)
	port, providerLogs := callerProvider(t, callerKeysDir(t, map[string]string{"caller": public, "other": otherPublic}))

	call := func(url string, ctx context.Context) (int, string) {
		return callAs(ctx, t, signing, url)
	}
	ada := sdk.WithCaller(t.Context(), sdk.Caller{User: "ada", Groups: []string{"admins"}})

	code, body := call("http://localhost:"+port, ada)
	var got seen
	_ = json.Unmarshal([]byte(body), &got)
	if code != http.StatusOK || got.Caller == nil || got.Caller.User != "ada" || got.Caller.Issuer != "caller" ||
		len(got.Caller.Groups) != 1 || got.Caller.Groups[0] != "admins" || got.Module != "caller" {
		t.Errorf("with a caller = %d %s\n%s", code, body, providerLogs)
	}
	if code, body := call("http://localhost:"+port, t.Context()); code != http.StatusOK || !strings.Contains(body, `"Caller":null`) {
		t.Errorf("without a caller = %d %s", code, body)
	}
	if code, body := call("http://127.0.0.1:"+port, ada); code != http.StatusUnauthorized || !strings.Contains(body, "invalid_token") {
		t.Errorf("minted for 127.0.0.1 = %d %s, want 401 invalid_token", code, body)
	}
	if code, body := callAs(ada, t, otherSigning, "http://localhost:"+port); code != http.StatusUnauthorized || !strings.Contains(body, "invalid_token") {
		t.Errorf("signed with the key of another consumer = %d %s, want 401 invalid_token", code, body)
	}
}

// callAs calls the provider at url as the consumer instance caller, whose binding token matches,
// minting MP-Caller tokens with the key in the file signing.
func callAs(ctx context.Context, t *testing.T, signing, url string) (int, string) {
	t.Helper()
	uses, logs, err := consumerUses(t, map[string]string{
		"MP_INSTANCE":                "caller",
		"MP_CALLER_SIGNING_KEY_FILE": signing,
		"TM_ECHO_URL":                url,
		"TM_ECHO_TOKEN_FILE":         file(t, consumerToken),
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}
	echo, err := uses.Echo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	req, _ := echo.NewRequest(ctx, http.MethodGet, "/who", nil)
	resp, err := echo.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestCallerKeyFileProblems covers the key files at start: an unreadable signing key, caller keys
// directory or consumer key file, or one without the right key, is one invalid line naming the
// file, never its content.
func TestCallerKeyFileProblems(t *testing.T) {
	t.Parallel()
	signing, public := callerKeyFiles(t)
	privateDir := callerKeysDir(t, map[string]string{"caller": signing})
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no caller keys directory": {
			env:  map[string]string{"TM_CALLER_KEYS_DIR": "/no/such/keys"},
			want: "config: provides.greet.caller_keys_dir (TM_CALLER_KEYS_DIR): invalid_binding: the caller keys directory /no/such/keys cannot be read: open /no/such/keys: no such file or directory",
		},
		"a private key as a consumer's key": {
			env:  map[string]string{"TM_CALLER_KEYS_DIR": privateDir},
			want: "config: provides.greet.caller_keys_dir (TM_CALLER_KEYS_DIR): invalid_binding: " + filepath.Join(privateDir, "caller") + ": a PRIVATE KEY block is not an Ed25519 public key",
		},
		"no signing key file": {
			env:  map[string]string{"MP_CALLER_SIGNING_KEY_FILE": "/no/such/key.pem"},
			want: "config: MP_CALLER_SIGNING_KEY_FILE (MP_CALLER_SIGNING_KEY_FILE): invalid: the signing key file /no/such/key.pem: it cannot be read",
		},
		"a public key as the signing key": {
			env:  map[string]string{"MP_CALLER_SIGNING_KEY_FILE": public},
			want: "config: MP_CALLER_SIGNING_KEY_FILE (MP_CALLER_SIGNING_KEY_FILE): invalid: the signing key file " + public + ": it holds no PEM PRIVATE KEY block",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := baseEnv(t)
			maps.Copy(env, tc.env)
			_, logs, err := start(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			if msgs := errorMsgs(t, logs); len(msgs) != 1 || msgs[0] != tc.want {
				t.Errorf("errors = %q, want %q", msgs, tc.want)
			}
		})
	}
}
