//go:build e2e

package render_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestOIDCClientEndToEnd runs the client step of spec 0037 against the pinned Pocket ID image
// and our own job image (AC-8, AC-12, AC-13): a fresh database with no admin ends with the
// client job exited 0 and the consumer started, three up runs leave one secret on the client, a
// base path change repoints the redirect URI, a client made by hand stays through a revoke, a
// provision job for a client id taken by hand fails with its name, and an unbound consumer's
// client is deleted. It needs Docker with the Compose plugin; run it with go test -tags e2e.
//
// Two stand ins keep it small: whoami replaces the fixture's ingress and consumer images (the
// job reaches Pocket ID by its service name, not through them), and the database connection is
// not verified (the fixture graph has no certificate issuer; the TLS path is covered by the
// spike in the spec's rationale).
func TestOIDCClientEndToEnd(t *testing.T) {
	const jobTag = "mp-oidc-job-test:e2e"
	run(t, "../..", "docker", "build", "-q", "-t", jobTag, "catalog/images/oidc-job")
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", jobTag).Run() })

	_, catalog, templates := pocketManagedResult(t)
	for i, m := range catalog.Modules {
		switch m.Name {
		case "edge", "oidcapp":
			catalog.Modules[i].Image = "traefik/whoami:v1.12.0"
		case "pocket-id":
			for j := range m.Command {
				catalog.Modules[i].Command[j] = strings.ReplaceAll(m.Command[j], "sslmode=verify-full&sslrootcert=$DB_CA_FILE", "sslmode=disable")
			}
		}
	}
	for i, c := range catalog.Capabilities {
		if c.Yields != nil && strings.HasSuffix(strings.SplitN(c.Yields.Job.Image, ":", 2)[0], "/oidc-job") {
			catalog.Capabilities[i].Yields.Job.Image = jobTag
		}
	}
	graph, err := os.ReadFile("testdata/pocket-id-managed/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	// renderGraph writes the files of the graph text into dir, leaving secrets and .env edits alone.
	renderGraph := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "graph.in.yaml"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		res := checkFile(t, filepath.Join(dir, "graph.in.yaml"), catalog)
		out, err := render.Render(res, catalog, templates)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out.Files {
			if f.Path == ".env" {
				if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
					continue // keep the edits of the test
				}
			}
			path := filepath.Join(dir, f.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	renderGraph(string(graph))
	run(t, dir, "sh", "setup.sh")

	project := "mp-e2e-" + strings.ToLower(strings.ReplaceAll(filepath.Base(dir), "_", "-"))
	compose := func(args ...string) (string, error) {
		cmd := exec.CommandContext(t.Context(), "docker", append([]string{"compose", "-p", project}, args...)...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	t.Cleanup(func() {
		cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "compose", "-p", project, "down", "-v", "-t", "1")
		cmd.Dir = dir
		_ = cmd.Run()
	})
	up := func() (string, error) { return compose("up", "-d", "--wait", "--wait-timeout", "240") }
	mustUp := func(when string) {
		t.Helper()
		if out, err := up(); err != nil {
			logs, _ := compose("logs", "--no-color", "--tail", "40")
			t.Fatalf("up %s: %v\n%s\n%s", when, err, out, logs)
		}
	}

	// api calls Pocket ID's admin API from the stack's network with the static API key, the way
	// the job does, and returns the status line and the body.
	api := func(method, path, body string) (int, string) {
		t.Helper()
		keyFile, err := filepath.Abs(filepath.Join(dir, "secrets/pocket-id/static-api-key"))
		if err != nil {
			t.Fatal(err)
		}
		script := `curl -sS -o /tmp/out -w '%{http_code}' -X "$1" -H "X-API-KEY: $(cat /key)" -H 'Content-Type: application/json' ${3:+--data "$3"} "http://pocket-id.pocket-id-managed.internal:1411$2"; echo; cat /tmp/out`
		cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--network", project+"_internal",
			"--user", strconv.Itoa(os.Getuid()), "--tmpfs", "/tmp", "-v", keyFile+":/key:ro", jobTag, "sh", "-c", script, "sh", method, path, body)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, b)
		}
		status, rest, _ := strings.Cut(string(b), "\n")
		code, err := strconv.Atoi(strings.TrimSpace(status))
		if err != nil {
			t.Fatalf("%s %s: status %q", method, path, status)
		}
		return code, rest
	}
	client := func(id string) (map[string]any, bool) {
		t.Helper()
		code, body := api("GET", "/api/oidc/clients/"+id, "")
		if code == 404 {
			return nil, false
		}
		if code != 200 {
			t.Fatalf("GET client %s: %d %s", id, code, body)
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatal(err)
		}
		return c, true
	}
	secretCount := func(id string) int {
		t.Helper()
		c, ok := client(id)
		if !ok {
			t.Fatalf("client %s is missing", id)
		}
		creds, _ := c["credentials"].(map[string]any)
		secrets, _ := creds["secrets"].([]any)
		return len(secrets)
	}
	callback := func(id string) string {
		t.Helper()
		c, ok := client(id)
		if !ok {
			t.Fatalf("client %s is missing", id)
		}
		urls, _ := c["callbackURLs"].([]any)
		if len(urls) != 1 {
			t.Fatalf("client %s has callbackURLs %v", id, urls)
		}
		return fmt.Sprint(urls[0])
	}
	state := func(service string) string {
		out, _ := compose("ps", "-a", "--format", "{{.Service}} {{.State}} {{.ExitCode}}", service)
		return strings.TrimSpace(out)
	}

	// AC-13: a fresh database with no admin; the job exited 0 and the consumer started
	mustUp("on a fresh database")
	if got := state("pocket-id_provision_app_oidc"); !strings.HasSuffix(got, "exited 0") {
		t.Errorf("the client job is %q, want exited 0", got)
	}
	if got := state("app"); !strings.Contains(got, "running") {
		t.Errorf("app is %q, want running", got)
	}
	const want = "https://pocket.example.test/callback"
	if got := callback("app_oidc"); got != want {
		t.Errorf("redirect URI = %q, want %q", got, want)
	}
	if c, _ := client("app_oidc"); fmt.Sprint(c["logoutCallbackURLs"]) != "[https://pocket.example.test/]" {
		t.Errorf("logout URIs = %v, want the home page", c["logoutCallbackURLs"])
	}
	if c, _ := client("app_oidc"); !strings.HasPrefix(fmt.Sprint(c["description"]), "mp-marker:") {
		t.Errorf("client description %v lacks the marker", c["description"])
	}

	// AC-8: three up runs leave one secret on the client
	mustUp("a second time")
	mustUp("a third time")
	if n := secretCount("app_oidc"); n != 1 {
		t.Errorf("app_oidc has %d secrets after three up runs, want 1", n)
	}

	// AC-8: a base path change repoints the redirect URI
	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "MP_EDGE__BASE_PATH=") {
		t.Fatalf(".env lacks MP_EDGE__BASE_PATH:\n%s", env)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(string(env)+"MP_EDGE__BASE_PATH=/platform\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustUp("after a base path change")
	if got := callback("app_oidc"); got != "https://pocket.example.test/platform/callback" {
		t.Errorf("redirect URI after the base path change = %q", got)
	}

	// AC-12: a client made by hand stays through a revoke
	if code, body := api("POST", "/api/oidc/clients", `{"id":"hand_made","name":"hand_made","callbackURLs":["https://x.example.test/cb"],"pkceEnabled":true,"isPublic":false}`); code != 201 {
		t.Fatalf("make a client by hand: %d %s", code, body)
	}
	mustUp("beside a client made by hand")
	if _, ok := client("hand_made"); !ok {
		t.Error("the revoke deleted a client without the marker")
	}

	// AC-12: a provision job for a client id taken by hand fails and names the client
	taken := strings.Replace(string(graph), "client_id: app_oidc", "client_id: hand_made", 1)
	renderGraph(taken)
	out, err := up()
	if err == nil {
		t.Fatalf("up with a client id taken by hand succeeded:\n%s", out)
	}
	logs, _ := compose("logs", "--no-color", "pocket-id_provision_app_oidc")
	const foreign = "client hand_made exists and was not made by the platform; delete it in Pocket ID or name another client id in the graph"
	if !strings.Contains(logs, foreign) {
		t.Errorf("the job log does not name the foreign client:\n%s", logs)
	}
	if code, _ := api("GET", "/api/oidc/clients/hand_made", ""); code != 200 {
		t.Errorf("the failed job damaged the client made by hand: %d", code)
	}

	// AC-12: an unbound consumer's client is deleted, the one made by hand stays
	unbound := strings.Replace(string(graph), "      oidc:\n        client_id: app_oidc\n        provider: pocket-id.oidc\n", "", 1)
	if unbound == string(graph) {
		t.Fatal("the graph text has no oidc binding to remove")
	}
	renderGraph(unbound)
	mustUp("with the consumer unbound")
	if _, ok := client("app_oidc"); ok {
		t.Error("the client of the unbound consumer was not deleted")
	}
	if _, ok := client("hand_made"); !ok {
		t.Error("the revoke deleted the client made by hand")
	}

	// AC-13: a fresh database again, bound again
	renderGraph(string(graph))
	if out, err := compose("down", "-v", "-t", "1"); err != nil {
		t.Fatalf("down: %v\n%s", err, out)
	}
	mustUp("on a second fresh database")
	if _, ok := client("app_oidc"); !ok {
		t.Error("the client is missing on a fresh database")
	}
}
