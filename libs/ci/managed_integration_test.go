//go:build integration

package ci_test

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// managedZipWarning is what `task managed:zip` prints for an unreleased zip from a tree with
// uncommitted changes.
const managedZipWarning = "managed:zip: the tree has uncommitted changes, so this zip builds only with MP_SOURCE_PATH"

// TestManagedZipSourceRef covers spec 0030 AC-12 and the value sourcing step "Managed render, the
// ref": in a clone of HEAD, `task managed:zip VERSION=0.0.0` on a clean tree prints no warning and
// its setup.sh builds from HEAD; with an untracked file it warns on stderr, still writes the zip,
// and its setup.sh holds source_ref='local'. Your checkout is untouched.
func TestManagedZipSourceRef(t *testing.T) {
	needTools(t, "git", "task", "go", "python3")
	clone := filepath.Join(t.TempDir(), "repo")
	git(t, repoRoot(t), "clone", "--quiet", "--no-hardlinks", repoRoot(t), clone)
	head := git(t, clone, "rev-parse", "HEAD")

	t.Run("clean tree", func(t *testing.T) {
		stderr, setup := managedZip(t, clone)
		if strings.Contains(stderr, managedZipWarning) {
			t.Errorf("a clean tree warned:\n%s", stderr)
		}
		if !strings.Contains(setup, "source_ref='"+head+"'\n") {
			t.Errorf("setup.sh does not build from HEAD %s", head)
		}
	})
	t.Run("uncommitted change", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(clone, "untracked.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stderr, setup := managedZip(t, clone)
		if !strings.Contains(stderr, managedZipWarning) {
			t.Errorf("stderr lacks the warning:\n%s", stderr)
		}
		if !strings.Contains(setup, "source_ref='local'\n") {
			t.Error("setup.sh does not hold source_ref='local'")
		}
	})
}

// managedZip runs `task managed:zip VERSION=0.0.0` in dir and returns its stderr and the setup.sh
// inside dist/modular-platform-0.0.0.zip, which the task must have written.
func managedZip(t *testing.T, dir string) (string, string) {
	t.Helper()
	zipPath := filepath.Join(dir, "dist", "modular-platform-0.0.0.zip")
	_ = os.Remove(zipPath)
	cmd := exec.CommandContext(t.Context(), "task", "--silent", "managed:zip", "VERSION=0.0.0")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err != nil {
		t.Fatalf("task managed:zip: %v\n%s\n%s", err, out, stderr.String())
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("the zip was not written: %v", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "/setup.sh") {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			return stderr.String(), string(data)
		}
	}
	t.Fatal("the zip holds no setup.sh")
	return "", ""
}
