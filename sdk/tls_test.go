package sdk_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
	"github.com/racherom/modular-platform/sdk/internal/testpki"
	"github.com/racherom/modular-platform/sdk/tlsacme"
)

// tmHost is the test module's host name in TLS mode.
const tmHost = "tm.sdk-test.internal"

// fakeCerts stands in for autocert: it signs a 24 hour certificate for the hello's server name
// with its CA, once release is closed (at once when release is nil), and keeps it.
type fakeCerts struct {
	t       *testing.T
	ca      *testpki.CA
	release chan struct{}
	held    atomic.Pointer[tls.Certificate]
}

func (f *fakeCerts) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if c := f.held.Load(); c != nil {
		return c, nil
	}
	if f.release != nil {
		select {
		case <-f.release:
		default:
			return nil, errors.New("the issuer is not there yet")
		}
	}
	f.held.CompareAndSwap(nil, f.ca.Leaf(f.t, 24*time.Hour, hello.ServerName))
	return f.held.Load(), nil
}

var _ tlsacme.Getter = (*fakeCerts)(nil)

// tlsEnv is baseEnv in TLS mode, with ca as MP_CA_FILE and the host name tmHost.
func tlsEnv(t *testing.T, ca *testpki.CA) map[string]string {
	env := baseEnv(t)
	env["MP_DOMAIN"] = "sdk-test.internal"
	env["MP_ACME_URL"] = "https://issuer.test/directory"
	env["MP_CA_FILE"] = file(t, string(ca.PEM()))
	return env
}

// freeAddr returns a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// TestTLSProblems covers the start rules of TLS mode (AC-8): only one of the two TLS settings
// set, an ACME URL that is not https, a CA file that cannot be read or holds no certificate, TLS
// without MP_DOMAIN, and an http binding URL to a module on the sdk. Each gives one line.
func TestTLSProblems(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	for name, tc := range map[string]struct {
		env   func(t *testing.T) map[string]string
		unset string
		want  string
	}{
		"only the ACME URL": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ACME_URL": "https://issuer.test/directory"}
			},
			unset: "MP_CA_FILE",
			want:  "config: MP_CA_FILE (MP_CA_FILE): tls_config: MP_ACME_URL is set, so MP_CA_FILE must be set too",
		},
		"only the CA file": {
			env:   func(*testing.T) map[string]string { return nil },
			unset: "MP_ACME_URL",
			want:  "config: MP_ACME_URL (MP_ACME_URL): tls_config: MP_CA_FILE is set, so MP_ACME_URL must be set too",
		},
		"an http ACME URL": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ACME_URL": "http://issuer.test/directory"}
			},
			want: "config: MP_ACME_URL (MP_ACME_URL): invalid: ",
		},
		"an unreadable CA file": {
			env:  func(t *testing.T) map[string]string { return map[string]string{"MP_CA_FILE": "/no/such/ca.pem"} },
			want: "config: MP_CA_FILE (MP_CA_FILE): tls_config: the CA file /no/such/ca.pem cannot be read",
		},
		"a CA file without a certificate": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_CA_FILE": file(t, "not a certificate\n")}
			},
			want: "config: MP_CA_FILE (MP_CA_FILE): tls_config: the CA file ",
		},
		"TLS without a domain": {
			env:   func(*testing.T) map[string]string { return nil },
			unset: "MP_DOMAIN",
			want:  "config: MP_DOMAIN (MP_DOMAIN): tls_config: TLS mode needs the host name's domain",
		},
		"an http URL to a module on the sdk": {
			env: func(*testing.T) map[string]string {
				return map[string]string{"TM_ECHO_URL": "http://echo.SDK-TEST.internal:8080"}
			},
			want: "config: uses.echo.url (TM_ECHO_URL): downgrade_refused: echo.sdk-test.internal is a module on the sdk",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := tlsEnv(t, ca)
			delete(env, tc.unset)
			for k, v := range tc.env(t) {
				env[k] = v
			}
			var logs syncBuffer
			_, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
				return http.NotFoundHandler(), nil
			}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))), sdk.WithCertificates(&fakeCerts{t: t, ca: ca}))
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			var lines []string
			for _, l := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				if strings.Contains(l, `"level":"ERROR"`) {
					lines = append(lines, l)
				}
			}
			if len(lines) != 1 || !strings.Contains(lines[0], tc.want) {
				t.Errorf("error lines = %q, want one with %q", lines, tc.want)
			}
		})
	}
}

// TestTLSHTTPSBindingToSDK covers the other side of downgrade_refused: in TLS mode an https URL to
// a module on the sdk, and an http URL to a host outside MP_DOMAIN, both start.
func TestTLSHTTPSBindingToSDK(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	for _, u := range []string{"https://echo.sdk-test.internal:8443", "http://whoami:80", "http://sdk-test.internal.example:80"} {
		env := tlsEnv(t, ca)
		env["TM_ECHO_URL"] = u
		_, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
			return http.NotFoundHandler(), nil
		}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.DiscardHandler)), sdk.WithCertificates(&fakeCerts{t: t, ca: ca}))
		if err != nil {
			t.Errorf("TM_ECHO_URL=%s: %v", u, err)
		}
	}
}

// TestTLSMode covers serving in TLS mode (AC-8, AC-9): the module listens on its TLS port only,
// is unready and fails its healthcheck until a certificate is held, then serves TLS 1.3 for its
// host name, refuses plain HTTP and older TLS on that port, and logs its host name and tls in
// its start line. The healthcheck fails without MP_CA_FILE or MP_DOMAIN.
func TestTLSMode(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	env := tlsEnv(t, ca)
	plainAddr, tlsAddr := freeAddr(t), freeAddr(t)
	addrs := sdk.WithAddrs(plainAddr, tlsAddr)
	certs := &fakeCerts{t: t, ca: ca, release: make(chan struct{})}
	var (
		logs    syncBuffer
		reached atomic.Int64
	)
	ctx, cancel := context.WithCancel(t.Context())
	exit := make(chan int, 1)
	go func() {
		exit <- sdk.Main(ctx, testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached.Add(1)
				_, _ = io.WriteString(w, "hi")
			}), nil
		}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))), sdk.WithCertificates(certs), addrs)
	}()
	defer func() {
		cancel()
		if code := <-exit; code != 0 {
			t.Errorf("Main returned %d\n%s", code, logs.String())
		}
	}()

	eventually(t, "the TLS port listens", func() bool {
		c, err := net.Dial("tcp", tlsAddr)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	})
	if c, err := net.Dial("tcp", plainAddr); err == nil {
		_ = c.Close()
		t.Error("the plain port listens in TLS mode")
	}
	if code := sdk.Healthcheck(sdk.WithEnv(env), addrs); code != 1 {
		t.Errorf("healthcheck before a certificate = %d, want 1", code)
	}

	close(certs.release)
	eventually(t, "the healthcheck passes", func() bool { return sdk.Healthcheck(sdk.WithEnv(env), addrs) == 0 })
	for _, k := range []string{"MP_CA_FILE", "MP_DOMAIN"} {
		without := maps.Clone(env)
		delete(without, k)
		if code := sdk.Healthcheck(sdk.WithEnv(without), addrs); code != 1 {
			t.Errorf("healthcheck without %s = %d, want 1", k, code)
		}
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsacme.ClientConfig(ca.Pool(), tmHost)}}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+tlsAddr+"/hello", nil)
	req.Header.Set("Authorization", "Bearer "+consumerToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hi" || resp.TLS.Version != tls.VersionTLS13 {
		t.Errorf("GET /hello = %d %q over %x", resp.StatusCode, body, resp.TLS.Version)
	}

	before := reached.Load()
	if resp, err := http.Get("http://" + tlsAddr + "/hello"); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("plain HTTP on the TLS port answered %d", resp.StatusCode)
		}
	}
	old := &tls.Config{RootCAs: ca.Pool(), ServerName: tmHost, MaxVersion: tls.VersionTLS12}
	if c, err := tls.Dial("tcp", tlsAddr, old); err == nil {
		_ = c.Close()
		t.Error("a TLS 1.2 handshake succeeded")
	}
	if reached.Load() != before {
		t.Error("a refused request reached the handler")
	}

	text := logs.String()
	if !strings.Contains(text, `"host":"`+tmHost+`","tls":"tls"`) || strings.Contains(text, "serving plain HTTP") {
		t.Errorf("the start log does not show the host name and TLS mode\n%s", text)
	}
}

// TestTLSUnreadyWithoutCertificate covers AC-9 in TLS mode: a built module answers /readyz 503
// while it holds no certificate (Build enrols none).
func TestTLSUnreadyWithoutCertificate(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	h, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(tlsEnv(t, ca)), sdk.WithLogger(slog.New(slog.DiscardHandler)), sdk.WithCertificates(&fakeCerts{t: t, ca: ca}))
	if err != nil {
		t.Fatal(err)
	}
	if code := readyz(t, h); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz = %d, want 503", code)
	}
}

// TestTLSBindingTrust covers client trust in TLS mode: an http binding without its own ca_file
// trusts MP_CA_FILE, so it reaches a provider whose certificate the module CA signed.
func TestTLSBindingTrust(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "echo")
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{*ca.Leaf(t, time.Hour, "127.0.0.1")}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	env := tlsEnv(t, ca)
	env["TM_ECHO_URL"] = srv.URL
	var uses testmodule.Uses
	_, err := sdk.Build(t.Context(), testmodule.Module, func(_ context.Context, m *tmModule) (http.Handler, error) {
		uses = m.Uses
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.DiscardHandler)), sdk.WithCertificates(&fakeCerts{t: t, ca: ca}))
	if err != nil {
		t.Fatal(err)
	}
	echo, err := uses.Echo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, echo, "/"); code != http.StatusOK || body != "echo" {
		t.Errorf("GET / = %d %q", code, body)
	}
}
