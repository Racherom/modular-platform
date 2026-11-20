package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// platformGraph is the managed platform graph that `task managed:render` renders.
const platformGraph = "../../../deploy/managed/graph.yaml"

// testCommit is a full commit as `task managed:render` passes it on a clean tree.
const testCommit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"

// renderInto runs the command's run with a fresh out folder and returns that folder, the log
// and the error.
func renderInto(t *testing.T, tag, sourceRef string) (string, string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "managed")
	var log strings.Builder
	err := run(platformGraph, out, tag, sourceRef, &log)
	return out, log.String(), err
}

// readFile returns the file at name under dir, failing the test when it is missing.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// builds lists the Dockerfiles under dir/build, sorted.
func builds(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "build", "*", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range found {
		found[i], _ = filepath.Rel(dir, f)
	}
	return found
}

// TestRun_SourceRef covers spec 0030 AC-10 and AC-12 for render-platform: the ref decides the
// warning on the log and the source_ref setup.sh builds from, and a full commit gives no warning.
func TestRun_SourceRef(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		ref, wantRef, warning string
	}{
		"no flag means main with a warning": {
			ref: "", wantRef: "main",
			warning: "warning: source ref main is a branch, not a commit",
		},
		"a branch warns": {
			ref: "feat/x", wantRef: "feat/x",
			warning: "warning: source ref feat/x is a branch, not a commit",
		},
		"local warns about MP_SOURCE_PATH": {
			ref: "local", wantRef: "local",
			warning: "warning: source ref local: this render comes from a local checkout, and a zip that builds a platform image builds only with MP_SOURCE_PATH",
		},
		"a full commit gives no warning": {ref: testCommit, wantRef: testCommit},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, log, err := renderInto(t, "", tc.ref)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, log)
			}
			if tc.warning != "" && !strings.Contains(log, tc.warning) {
				t.Errorf("log lacks %q:\n%s", tc.warning, log)
			}
			if tc.warning == "" && strings.Contains(log, "warning:") {
				t.Errorf("a full commit warned:\n%s", log)
			}
			if setup := readFile(t, out, "setup.sh"); !strings.Contains(setup, "source_ref='"+tc.wantRef+"'\n") {
				t.Errorf("setup.sh does not build from %s", tc.wantRef)
			}
		})
	}
}

// TestRun_BadSourceRef covers spec 0030 AC-10: a ref git refuses (a space, a quote, a #) or a
// short commit stops the render, names the ref, and writes no out folder.
func TestRun_BadSourceRef(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"a b", "it's", "a#b", "cafe"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			out, log, err := renderInto(t, "", ref)
			if err == nil {
				t.Fatalf("ref %q rendered:\n%s", ref, log)
			}
			if !strings.Contains(err.Error(), ref) {
				t.Errorf("error %q does not name the ref %q", err, ref)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Errorf("ref %q wrote %s", ref, out)
			}
		})
	}
}

// TestRun_UnreleasedBuildsFourPlatformImages covers spec 0030 AC-6 and the value sourcing step
// "Managed render modules": with the catalog tag 0.0.0, exactly the four platform modules get a
// Dockerfile, each the catalog's file byte for byte.
func TestRun_UnreleasedBuildsFourPlatformImages(t *testing.T) {
	t.Parallel()
	out, log, err := renderInto(t, "", testCommit)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	want := []string{"build/api/Dockerfile", "build/deployer-docker/Dockerfile", "build/renderer/Dockerfile", "build/web/Dockerfile"}
	got := builds(t, out)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Dockerfiles %v, want %v", got, want)
	}
	for _, f := range want {
		module := filepath.Base(filepath.Dir(f))
		if catalogFile := readFile(t, "../../modules/"+module, "Dockerfile"); readFile(t, out, f) != catalogFile {
			t.Errorf("%s is not catalog/modules/%s/Dockerfile", f, module)
		}
	}
	if compose := readFile(t, out, "docker-compose.yml"); strings.Count(compose, "pull_policy: never") != 4 {
		t.Errorf("docker-compose.yml has %d pull_policy never, want 4", strings.Count(compose, "pull_policy: never"))
	}
}

// TestRun_ImageTagBuildsNothing covers spec 0030 AC-6: `task managed:render
// MANAGED_IMAGE_TAG=dev` (and ci) retags first, so the folder has no build/ and no pull_policy.
func TestRun_ImageTagBuildsNothing(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"dev", "ci"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			out, log, err := renderInto(t, tag, testCommit)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, log)
			}
			if _, err := os.Stat(filepath.Join(out, "build")); err == nil {
				t.Errorf("a %s render has a build folder", tag)
			}
			compose := readFile(t, out, "docker-compose.yml")
			if strings.Contains(compose, "pull_policy") {
				t.Errorf("a %s render has pull_policy", tag)
			}
			if !strings.Contains(compose, "ghcr.io/racherom/modular-platform/renderer:"+tag+"\n") {
				t.Errorf("the renderer image is not retagged to %s", tag)
			}
			if strings.Contains(readFile(t, out, "setup.sh"), "build_image ") {
				t.Errorf("setup.sh of a %s render builds an image", tag)
			}
		})
	}
}

// TestRun_ReplacesOut checks that run replaces the out folder as a whole, so a file of an older
// render never ships in the zip.
func TestRun_ReplacesOut(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "managed")
	stale := filepath.Join(out, "build", "old", "Dockerfile")
	if err := os.MkdirAll(filepath.Dir(stale), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := run(platformGraph, out, "dev", testCommit, &log); err != nil {
		t.Fatalf("run: %v\n%s", err, log.String())
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("run kept a file of the earlier render")
	}
}
