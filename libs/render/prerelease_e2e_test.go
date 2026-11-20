//go:build e2e

package render_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// writePrerelease renders testdata/prerelease/e2e-graph.yaml (the renderer alone, whose catalog
// image is unreleased) with the built in Dockerfiles at ref into a new folder.
func writePrerelease(t *testing.T, ref string) string {
	t.Helper()
	catalog := loadCatalog(t, "../../catalog")
	result := checkFile(t, "testdata/prerelease/e2e-graph.yaml", catalog)
	source, err := render.NewSourceRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	dockerfiles, err := contract.LoadDockerfiles(os.DirFS("../../catalog"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	out, err := render.Render(result, catalog, builtinTemplates(t), render.WithSource(source, dockerfiles))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range out.Files {
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// setupWith runs setup.sh in dir with env added, from the folder from.
func setupWith(t *testing.T, dir, from string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join(dir, "setup.sh"))
	cmd.Dir = from
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestPrereleaseZipEndToEnd covers spec 0030 AC-2, AC-3 and AC-12 for real: setup.sh builds the
// renderer image from this checkout (MP_SOURCE_PATH, given relative to where setup.sh runs) and
// labels it local, never with the zip's commit, and docker compose up then starts it healthy
// without pulling. It needs Docker 23 or newer.
func TestPrereleaseZipEndToEnd(t *testing.T) {
	head, err := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := writePrerelease(t, strings.TrimSpace(string(head)))
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// setup.sh resolves MP_SOURCE_PATH with pwd -P, so the path it prints has no symlink
	physical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	out, err := setupWith(t, dir, repo, "MP_SOURCE_PATH=.")
	if err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, out)
	}
	if !strings.Contains(out, "from the local checkout "+physical+"\n") {
		t.Errorf("setup.sh did not build from the local checkout:\n%s", out)
	}
	label, err := exec.CommandContext(t.Context(), "docker", "image", "inspect", "--format",
		`{{index .Config.Labels "org.opencontainers.image.revision"}}`, "ghcr.io/racherom/modular-platform/renderer:0.0.0").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(label)); got != render.LocalSourceRef {
		t.Errorf("the image built from the checkout is labeled %q, want %q", got, render.LocalSourceRef)
	}

	project := "mp-prerelease-" + strings.ToLower(filepath.Base(dir))
	t.Cleanup(func() {
		cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "compose", "-p", project, "down", "-v", "--remove-orphans")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("down: %v\n%s", err, out)
		}
	})
	// --pull never: the stack starts only from the image setup.sh built
	run(t, dir, "docker", "compose", "-p", project, "up", "-d", "--wait", "--wait-timeout", "120", "--pull", "never")
}

// TestPrereleaseSetupStops covers spec 0030 AC-3 and AC-4: a ref GitHub does not hold, and an
// MP_SOURCE_PATH without go.work, stop setup.sh with a message naming the image, the ref and
// MP_SOURCE_PATH, before it writes to .env or makes any secret.
func TestPrereleaseSetupStops(t *testing.T) {
	const unknown = "0000000000000000000000000000000000000000"
	for name, tc := range map[string]struct {
		env  []string
		want string
	}{
		"ref not on GitHub":        {want: "could not build ghcr.io/racherom/modular-platform/renderer:0.0.0 at " + unknown},
		"source without go.work":   {env: []string{"MP_SOURCE_PATH=" + t.TempDir()}, want: "holds no go.work"},
		"source that is no folder": {env: []string{"MP_SOURCE_PATH=/nonexistent/mp"}, want: "is not a folder"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writePrerelease(t, unknown)
			out, err := setupWith(t, dir, dir, tc.env...)
			if err == nil {
				t.Fatalf("setup.sh succeeded:\n%s", out)
			}
			if !strings.Contains(out, tc.want) || !strings.Contains(out, "MP_SOURCE_PATH") {
				t.Errorf("setup.sh output lacks %q or MP_SOURCE_PATH:\n%s", tc.want, out)
			}
			if tc.env == nil && !strings.Contains(out, "renderer:0.0.0 from Modular Platform at "+unknown) {
				t.Errorf("setup.sh output does not name the image and ref:\n%s", out)
			}
			// the zip ships .env; setup.sh must not have written to it
			if env, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil || strings.Contains(string(env), "MP_UID=") {
				t.Errorf("setup.sh wrote .env before it stopped (%v)", err)
			}
			for _, made := range []string{"secrets", "data"} {
				if _, err := os.Stat(filepath.Join(dir, made)); err == nil {
					t.Errorf("setup.sh made %s before it stopped", made)
				}
			}
		})
	}
}
