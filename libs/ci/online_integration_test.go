//go:build integration

package ci_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestOnlineUpCheckout covers spec 0030 AC-8 before a push: on a fresh commit that no remote
// holds, `task online:up` with MP_SOURCE_PATH builds web and renderer from that checkout and
// reaches no git remote. Both images carry the commit as their revision label, the renderer logs
// it as source_ref with no warning, a zip rendered through the ingress names it, and
// deploy/online/versions.env stays as it was.
func TestOnlineUpCheckout(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker is needed: %v", err)
	}

	// 1. a clone of the committed state plus one commit that is on no remote
	src := filepath.Join(t.TempDir(), "src")
	run(t, root, nil, "git", "clone", "--quiet", root, src)
	run(t, src, nil, "git", "-c", "user.name=mp-test", "-c", "user.email=mp-test@example.invalid",
		"commit", "--quiet", "--allow-empty", "-m", "test: AC-8, a commit on no remote")
	commit := strings.TrimSpace(run(t, src, nil, "git", "rev-parse", "HEAD"))
	srcAbs, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatal(err)
	}

	// 2. versions.env must not change
	versionsPath := filepath.Join(root, "deploy/online/versions.env")
	versions, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatal(err)
	}

	// 3. two free ports; the small race until Compose binds them is accepted
	httpsPort, httpPort := freePort(t), freePort(t)

	// 4. the task, with every value it reads set in the shell, which wins over --env-file
	const project = "mp-online-ac8"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "compose", "-p", project, "down", "-v", "--remove-orphans")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("compose down: %v\n%s", err, out)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "task", "online:up")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"MP_SOURCE_PATH="+src,
		"MP_VERSION=0.0.0",
		"MP_SOURCE_REF=main", // the checkout mode ignores it
		"MP_SOURCE_REMOTE=http://127.0.0.1:9/no-remote.git", // any remote access fails at once
		"COMPOSE_PROJECT_NAME="+project,
		"PLATFORM_HTTPS_PORT="+httpsPort,
		"PLATFORM_HTTP_PORT="+httpPort,
		"PLATFORM_DOMAIN=platform.localhost",
		"PLATFORM_TLS=internal",
		"MP_BASE_PATH=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task online:up: %v\n%s", err, out)
	}

	// 5. the checks
	if want := "online:up: building web and renderer from the checkout " + srcAbs + " at " + commit; !strings.Contains(string(out), want) {
		t.Errorf("the output lacks %q:\n%s", want, out)
	}
	for _, image := range []string{"web", "renderer"} {
		ref := "ghcr.io/racherom/modular-platform/" + image + ":0.0.0"
		label := strings.TrimSpace(run(t, root, nil, "docker", "image", "inspect", "--format",
			`{{ index .Config.Labels "org.opencontainers.image.revision" }}`, ref))
		if label != commit {
			t.Errorf("%s has the revision label %q, want %s", ref, label, commit)
		}
	}
	checkRendererLog(t, run(t, root, nil, "docker", "compose", "-p", project, "logs", "--no-log-prefix", "renderer"), commit)

	setup := renderSetup(t, httpsPort)
	if want := "source_ref='" + commit + "'"; !strings.Contains(setup, want) {
		t.Errorf("the zip's setup.sh lacks %q", want)
	}

	after, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, versions) {
		t.Errorf("deploy/online/versions.env changed:\n%s", after)
	}
}

// run runs a command in dir with env added and fails the test on an error; it returns stdout.
func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, stderr.String())
	}
	return string(out)
}

// freePort returns a TCP port on 127.0.0.1 that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// checkRendererLog checks the renderer's JSON log: the start line names commit as source_ref,
// and the renderer gives no source ref warning (neither the moving branch nor the local one). The
// SDK's own warnings of the online editor (plain HTTP inside the network, binding auth off) are
// not about the source, so they do not count.
func checkRendererLog(t *testing.T, logs, commit string) {
	t.Helper()
	started := false
	scanner := bufio.NewScanner(strings.NewReader(logs))
	for scanner.Scan() {
		var entry struct {
			Level     string `json:"level"`
			Msg       string `json:"msg"`
			SourceRef string `json:"source_ref"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Level == "WARN" && (entry.SourceRef != "" || strings.HasPrefix(entry.Msg, "the renderer's source ref")) {
			t.Errorf("the renderer warns about its source ref: %s", scanner.Text())
		}
		if entry.Msg == "renderer starting" {
			started = true
			if entry.SourceRef != commit {
				t.Errorf("the renderer logs source_ref %q, want %s", entry.SourceRef, commit)
			}
		}
	}
	if !started {
		t.Errorf("no renderer starting line in the log:\n%s", logs)
	}
}

// renderSetup renders a graph with the renderer through the ingress on httpsPort and returns the
// zip's setup.sh. Caddy's internal CA signs the certificate, so it is not checked.
func renderSetup(t *testing.T, httpsPort string) string {
	t.Helper()
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	client := &http.Client{
		Timeout: time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, "127.0.0.1:"+httpsPort)
			},
			TLSClientConfig: &tls.Config{ServerName: "platform.localhost", InsecureSkipVerify: true}, // Caddy's internal CA signs it
		},
	}
	// the renderer's only use, ingress, is optional, so no binding is needed
	body := `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"ac8","modules":{"postgres":{"module":"postgres"},"renderer":{"module":"renderer"}}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://platform.localhost:"+httpsPort+"/renderer/v1/render", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render: %s\n%s", resp.Status, data)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != "setup.sh" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		setup, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(setup)
	}
	t.Fatal("the zip holds no setup.sh")
	return ""
}
