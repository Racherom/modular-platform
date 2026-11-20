//go:build integration

package ci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackMergeAct covers spec 0032 AC-5 and AC-9 for the back merge job: act runs the real
// release.yml in local Docker with release-please and checkout replaced by stubs and gh replaced
// by the stub in testdata, whose state sits in a folder shared with the container. The job
// creates the branch and the pull request once; a second run on the same state does nothing; a
// branch at another commit fails; no created release, or the gate off, skips the job.
func TestBackMergeAct(t *testing.T) {
	root := repoRoot(t)
	act := newActRunner(t, root)
	mappings := []string{
		act.stubFor(t, "release.yml", "googleapis/release-please-action", "release-please-stub"),
		act.stubFor(t, "release.yml", "actions/checkout", "checkout-stub"),
	}
	event := act.event(t, `{"ref":"refs/heads/main"}`)

	const (
		jobName     = "[Release/Back merge into dev"
		inContainer = "/gh-state"
	)
	created := []string{
		"--var", "RELEASES_ENABLED=true", "-s", "RELEASE_PLEASE_TOKEN=dummy",
		"--env", "STUB_RELEASE_CREATED=true", "--env", "STUB_VERSION=1.2.3", "--env", "STUB_TAG=v1.2.3",
		"--env", "GH=libs/ci/testdata/gh-stub/gh", "--env", "GH_STUB_STATE=" + inContainer,
	}
	runWith := func(t *testing.T, state string, args []string) (string, error) {
		t.Helper()
		all := append(append([]string{}, args...), "--container-options", "-v "+state+":"+inContainer, "--job", "back-merge")
		return act.run(t, "push", ".github/workflows/release.yml", event, mappings, all...)
	}
	calls := func(t *testing.T, state string) string {
		t.Helper()
		data, _ := os.ReadFile(filepath.Join(state, "calls.log"))
		return string(data)
	}

	state := t.TempDir()
	if err := os.Chmod(state, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Run("a created release opens the pull request once", func(t *testing.T) {
		out, err := runWith(t, state, created)
		if err != nil {
			t.Fatalf("act: %v\n%s", err, out)
		}
		log := calls(t, state)
		if n := strings.Count(log, "pr create"); n != 1 {
			t.Errorf("pr create called %d times, want 1\n%s", n, log)
		}
		if !strings.Contains(log, "--title chore(release): back merge 1.2.3") {
			t.Errorf("no back merge title in the calls\n%s", log)
		}
	})
	t.Run("a second run on the same state does nothing", func(t *testing.T) {
		before := strings.Count(calls(t, state), "pr create")
		out, err := runWith(t, state, created)
		if err != nil {
			t.Fatalf("act: %v\n%s", err, out)
		}
		if after := strings.Count(calls(t, state), "pr create"); after != before {
			t.Errorf("pr create called %d more times on the rerun", after-before)
		}
		if !strings.Contains(out, "already open") {
			t.Errorf("output lacks the already open message\n%s", out)
		}
	})
	t.Run("a branch at another commit fails the job", func(t *testing.T) {
		other := t.TempDir()
		if err := os.Chmod(other, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "ref-back-merge__1.2.3"), []byte(otherSHA+"\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		out, err := runWith(t, other, created)
		if err == nil {
			t.Fatalf("act passed, want a failure\n%s", out)
		}
		if !strings.Contains(out, "exists at "+otherSHA) {
			t.Errorf("output lacks the stale branch message\n%s", out)
		}
		if strings.Contains(calls(t, other), "pr create") {
			t.Error("a pull request was opened")
		}
	})
	t.Run("no created release skips the job", func(t *testing.T) {
		out, err := runWith(t, t.TempDir(), []string{"--var", "RELEASES_ENABLED=true", "-s", "RELEASE_PLEASE_TOKEN=dummy"})
		if err != nil {
			t.Fatalf("act: %v\n%s", err, out)
		}
		if strings.Contains(out, "Open the back merge pull request") {
			t.Errorf("the job ran\n%s", out)
		}
	})
	t.Run("the gate off skips the job", func(t *testing.T) {
		out, err := runWith(t, t.TempDir(), nil)
		if err != nil {
			t.Fatalf("act: %v\n%s", err, out)
		}
		if strings.Contains(out, jobName+"/Open") || strings.Contains(out, "Open the back merge pull request") {
			t.Errorf("the job ran\n%s", out)
		}
	})
}
