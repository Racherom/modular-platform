//go:build integration

package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// repoRoot returns the absolute path of the repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// taskfileVar reads a top level var of Taskfile.yml, so each pinned version lives in one place.
// It fails when the var is missing or empty.
func taskfileVar(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `: (\S+)$`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("Taskfile.yml has no var %s", name)
	}
	return string(m[1])
}

// needTools fails, never skips, when one of the named programs is not on PATH.
func needTools(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s is needed: %v", name, err)
		}
	}
}
