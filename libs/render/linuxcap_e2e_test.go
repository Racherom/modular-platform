//go:build e2e

package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestLinuxCapsEndToEnd renders a one module graph whose root service adds NET_BIND_SERVICE
// with Managed, shows the guard accepts it, and starts it: /proc/1/status in the container shows
// that the one Linux capability the guard admits grants only bit 10, and that no new privileges
// is on (spec 0022, AC-21).
func TestLinuxCapsEndToEnd(t *testing.T) {
	catalog := loadCatalog(t, "testdata/linuxcap/catalog")
	result := checkFile(t, "testdata/linuxcap/graph.yaml", catalog)
	m, err := render.Managed(result, catalog, builtinTemplates(t), testDeployment)
	if err != nil {
		t.Fatal(err)
	}
	project, err := render.LoadProject(t.Context(), fileData(t, m.Output, "docker-compose.yml"), fileData(t, m.Output, ".env"), deployEnv)
	if err != nil {
		t.Fatal(err)
	}
	if problems := render.Guard(project, reserved); len(problems) > 0 {
		t.Fatalf("the guard refuses the render: %+v", problems)
	}

	s := newStack(t, catalog)
	for _, f := range m.Files {
		path := filepath.Join(s.dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.dir, "deploy.env"), deployEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	envFiles := []string{"--env-file", ".env", "--env-file", "deploy.env"}
	if out, err := s.compose(append(envFiles, "up", "-d", "--wait", "--wait-timeout", "120")...); err != nil {
		t.Fatalf("up: %v\n%s", err, out)
	}
	out, err := s.compose(append(envFiles, "exec", "-T", "low", "cat", "/proc/1/status")...)
	if err != nil {
		t.Fatalf("read /proc/1/status: %v\n%s", err, out)
	}
	status := map[string]string{}
	for line := range strings.Lines(out) {
		if k, v, ok := strings.Cut(line, ":"); ok {
			status[k] = strings.TrimSpace(v)
		}
	}
	for k, want := range map[string]string{"Uid": "0\t0\t0\t0", "CapEff": "0000000000000400", "NoNewPrivs": "1"} {
		if status[k] != want {
			t.Errorf("%s = %q, want %q", k, status[k], want)
		}
	}
}
