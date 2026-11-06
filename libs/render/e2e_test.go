//go:build e2e

package render_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/modules/compose"

	"github.com/racherom/modular-platform/libs/render"
)

// TestSDKPairEndToEnd covers AC-12 for real: it builds the two sdk test modules, renders their
// graph, fills the required setting, runs setup.sh and docker compose up, and checks that the
// consumer's call through its binding answers 200 while a call without the token answers 401.
// It needs Docker with the Compose plugin; run it with go test -tags e2e.
func TestSDKPairEndToEnd(t *testing.T) {
	ctx := t.Context()
	for _, target := range []string{"pair-provider", "pair-consumer"} {
		run(t, "../..", "docker", "build", "-q", "-f", "libs/render/internal/sdkpair/Dockerfile",
			"--target", target, "-t", "modular-platform/"+target+":test", ".")
	}

	plan, catalog := pairPlan(t)
	out, err := render.Render(plan, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range out.Files {
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := f.Data
		if f.Path == "config/provider.env" {
			data = bytes.Replace(data, []byte("\nPAIR_GREETING=\n"), []byte("\nPAIR_GREETING=hi\n"), 1)
		}
		if err := os.WriteFile(path, data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "sh", "setup.sh")

	stack, err := compose.NewDockerComposeWith(
		compose.WithStackFiles(filepath.Join(dir, "docker-compose.yml")),
		compose.StackIdentifier("sdk-pair-"+strings.ToLower(filepath.Base(dir))),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// t.Context is done by the time cleanups run
		if err := stack.Down(context.WithoutCancel(ctx), compose.RemoveOrphans(true), compose.RemoveVolumes(true)); err != nil {
			t.Log(err)
		}
	})
	if err := stack.WithEnv(dotEnv(t, filepath.Join(dir, ".env"))).Up(ctx, compose.Wait(true)); err != nil {
		t.Fatal(err)
	}

	consumer, err := stack.ServiceContainer(ctx, "consumer")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ url, want string }{
		{"http://127.0.0.1:8080/call", "200 hi consumer"},
		{"http://provider:8080/hello", "401 "},
	} {
		code, r, err := consumer.Exec(ctx, []string{"/pair-consumer", "get", c.url}, tcexec.Multiplexed())
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(r)
		if code != 0 || !strings.HasPrefix(string(got), c.want) {
			t.Errorf("GET %s = exit %d, %q, want %q", c.url, code, got, c.want)
		}
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// dotEnv reads the KEY=value lines of the stack's .env, which docker compose reads by itself
// but the compose module does not.
func dotEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	env := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		if k, v, ok := strings.Cut(s.Text(), "="); ok && !strings.HasPrefix(k, "#") {
			env[k] = v
		}
	}
	for _, k := range []string{"MP_UID", "MP_GID"} {
		if env[k] == "" {
			t.Fatalf("setup.sh did not write %s into .env", k)
		}
	}
	return env
}
