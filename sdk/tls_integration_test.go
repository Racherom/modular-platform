//go:build integration

package sdk_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// pebble v2.10.1, pinned by digest (spec 0005, "Build plan").
const (
	pebbleImage    = "ghcr.io/letsencrypt/pebble@sha256:ddf230642b1a584f519f32e347de1b05a6e4c1f6c35c1863b33effeab5f78199"
	tlsModuleImage = "modular-platform/sdk-tls-module:test"
	// pebbleHost is pebble's alias on the test network; its own certificate names it.
	pebbleHost = "pebble"
)

// pebbleConfig validates tls-alpn-01 on 8443, answers without Retry-After delays worth waiting
// for, and issues certificates valid for validity seconds.
func pebbleConfig(validity int) string {
	return fmt.Sprintf(`{"pebble": {
  "listenAddress": "0.0.0.0:14000",
  "managementListenAddress": "0.0.0.0:15000",
  "certificate": "/test/certs/localhost/cert.pem",
  "privateKey": "/test/certs/localhost/key.pem",
  "httpPort": 5002,
  "tlsPort": 8443,
  "externalAccountBindingRequired": false,
  "retryAfter": {"authz": 1, "order": 1},
  "keyAlgorithm": "ecdsa",
  "profiles": {"default": {"description": "sdk tests", "validityPeriod": %d}}
}}`, validity)
}

// tlsStack is pebble and the TLS test module on one Docker network, so pebble reaches the
// module's host name through Docker's DNS.
type tlsStack struct {
	pebble, module testcontainers.Container
	roots          *x509.CertPool // pebble's issuing root
	addr           string         // the module's 8443 on the host
}

// startTLSStack starts pebble with certificates valid for validity seconds, then the module with
// MP_CA_FILE holding pebble's own root and its issuing root, and waits until the module holds a
// certificate.
func startTLSStack(t *testing.T, validity int) tlsStack {
	t.Helper()
	ctx := t.Context()
	nw, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupNetwork(t, nw)

	pebble, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image: pebbleImage,
			Cmd:   []string{"-config", "/pebble.json"},
			// no random validation sleeps, no refused nonces: the timings below stay tight
			Env:            map[string]string{"PEBBLE_VA_NOSLEEP": "1", "PEBBLE_WFE_NONCEREJECT": "0"},
			Files:          []testcontainers.ContainerFile{{Reader: strings.NewReader(pebbleConfig(validity)), ContainerFilePath: "/pebble.json", FileMode: 0o644}},
			ExposedPorts:   []string{"15000/tcp"},
			Networks:       []string{nw.Name},
			NetworkAliases: map[string][]string{nw.Name: {pebbleHost}},
			WaitingFor:     wait.ForLog("ACME directory available"),
		}})
	testcontainers.CleanupContainer(t, pebble)
	if err != nil {
		t.Fatal(err)
	}
	minica := readContainerFile(t, pebble, "/test/certs/pebble.minica.pem")
	minicaPool := x509.NewCertPool()
	minicaPool.AppendCertsFromPEM(minica)
	mgmt, err := pebble.PortEndpoint(ctx, "15000/tcp", "https")
	if err != nil {
		t.Fatal(err)
	}
	root := httpsGet(t, &tls.Config{RootCAs: minicaPool, ServerName: "localhost"}, mgmt+"/roots/0")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(root) {
		t.Fatalf("pebble's root is no PEM certificate: %q", root)
	}

	module, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image: tlsModuleImage,
			Env: map[string]string{
				"MP_INSTANCE": "tm",
				"MP_DOMAIN":   "sdk-test.internal",
				"MP_ACME_URL": "https://" + pebbleHost + ":14000/dir",
				"MP_CA_FILE":  "/ca.pem",
			},
			Files: []testcontainers.ContainerFile{{
				Reader: strings.NewReader(string(minica) + string(root)), ContainerFilePath: "/ca.pem", FileMode: 0o644,
			}},
			ExposedPorts:   []string{"8443/tcp"},
			Networks:       []string{nw.Name},
			NetworkAliases: map[string][]string{nw.Name: {tmHost}},
			WaitingFor:     wait.ForLog("certificate held").WithStartupTimeout(2 * time.Minute),
		}})
	testcontainers.CleanupContainer(t, module)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := module.PortEndpoint(ctx, "8443/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	return tlsStack{pebble: pebble, module: module, roots: roots, addr: addr}
}

// leaf does a handshake with cfg and returns the certificate the module presented.
func (s tlsStack) leaf(cfg *tls.Config) (*x509.Certificate, error) {
	conn, err := tls.Dial("tcp", s.addr, cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.ConnectionState().PeerCertificates[0], nil
}

// orders counts the new orders pebble has seen.
func (s tlsStack) orders(t *testing.T) int {
	t.Helper()
	return strings.Count(containerLogs(t, s.pebble), "POST /order-plz")
}

// TestTLSAgainstPebble covers AC-8 and AC-9 against a real ACME server: the module enrols its
// host name through the SDK's ACME client over tls-alpn-01 on 8443 (pebble's validation hello
// offers only acme-tls/1), a client offering TLS 1.2 and 1.3 and a TLS 1.3 only client get the
// same ECDSA P-256 certificate from one order, the healthcheck passes over TLS and fails without
// MP_CA_FILE, and the module renews once two thirds of the lifetime have passed. It needs Docker;
// run it with go test -tags integration.
func TestTLSAgainstPebble(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "docker", "build", "-q", "-f", "sdk/internal/tlsmodule/Dockerfile", "-t", tlsModuleImage, ".")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}

	t.Run("24 hour certificate", func(t *testing.T) {
		t.Parallel()
		s := startTLSStack(t, 24*60*60)
		both := &tls.Config{RootCAs: s.roots, ServerName: tmHost}
		only13 := &tls.Config{RootCAs: s.roots, ServerName: tmHost, MinVersion: tls.VersionTLS13}
		a, err := s.leaf(both)
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.leaf(only13)
		if err != nil {
			t.Fatal(err)
		}
		key, ok := a.PublicKey.(*ecdsa.PublicKey)
		if !ok || key.Curve != elliptic.P256() || a.SerialNumber.Cmp(b.SerialNumber) != 0 {
			t.Errorf("certificates: %T, serials %s and %s, want one ECDSA P-256 certificate", a.PublicKey, a.SerialNumber, b.SerialNumber)
		}
		if err := a.VerifyHostname(tmHost); err != nil {
			t.Error(err)
		}
		if n := s.orders(t); n != 1 {
			t.Errorf("pebble saw %d orders, want 1\n%s", n, containerLogs(t, s.pebble))
		}

		if body := httpsGet(t, only13, "https://"+s.addr+"/hello"); string(body) != "hi" {
			t.Errorf("GET /hello = %q", body)
		}
		if resp, err := http.Get("http://" + s.addr + "/hello"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Errorf("plain HTTP on 8443 answered %d", resp.StatusCode)
			}
		}

		ctx := t.Context()
		if code, _, err := s.module.Exec(ctx, []string{"/tls-module", "healthcheck"}); err != nil || code != 0 {
			t.Errorf("healthcheck = %d, %v; want 0", code, err)
		}
		if code, _, err := s.module.Exec(ctx, []string{"/tls-module", "healthcheck"}, tcexec.WithEnv([]string{"MP_CA_FILE="})); err != nil || code != 1 {
			t.Errorf("healthcheck without MP_CA_FILE = %d, %v; want 1", code, err)
		}
	})

	t.Run("90 second certificate", func(t *testing.T) {
		t.Parallel()
		s := startTLSStack(t, 90)
		cfg := &tls.Config{RootCAs: s.roots, ServerName: tmHost, MinVersion: tls.VersionTLS13}
		first, err := s.leaf(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var renewed *x509.Certificate
		for time.Now().Before(first.NotAfter) {
			if l, err := s.leaf(cfg); err == nil && l.SerialNumber.Cmp(first.SerialNumber) != 0 {
				renewed = l
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if renewed == nil {
			t.Fatalf("no renewal before the first certificate expired\n%s", containerLogs(t, s.module))
		}
		// the module renews at two thirds of the 90 second lifetime, 30s before expiry; the order,
		// the poll above and pebble's clock take a few seconds either way
		if left := time.Until(first.NotAfter); left < 25*time.Second || left > 35*time.Second {
			t.Errorf("the module presented its renewed certificate %s before the first expired, want 25s to 35s", left)
		}
		if n := s.orders(t); n != 2 {
			t.Errorf("pebble saw %d orders, want 2", n)
		}
	})
}

func readContainerFile(t *testing.T, c testcontainers.Container, path string) []byte {
	t.Helper()
	r, err := c.CopyFileFromContainer(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func containerLogs(t *testing.T, c testcontainers.Container) string {
	t.Helper()
	r, err := c.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

func httpsGet(t *testing.T, cfg *tls.Config, url string) []byte {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d %v", url, resp.StatusCode, err)
	}
	return body
}
