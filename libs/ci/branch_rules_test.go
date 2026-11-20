package ci_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBranchRulesScript covers spec 0032 AC-3: scripts/branch-rules.sh lets a pull request into
// main come only from dev, hotfix/* or the release pull request branch (a hotfix needs a
// conventional title), makes a pull request into dev carry a conventional commit or Revert
// title, and passes every other base. A hostile title or branch name is data, never run.
func TestBranchRulesScript(t *testing.T) {
	t.Parallel()
	canary := t.TempDir() + "/ran"
	tests := []struct {
		name              string
		base, head, title string
		wantFail          bool
	}{
		{"feature branch into main", "main", "feat/x", "feat: x", true},
		{"dev into main", "main", "dev", "Release", false},
		{"release pull request into main", "main", "release-please--branches--main", "chore(main): release 1.0.0", false},
		{"hotfix into main with a conventional title", "main", "hotfix/x", "fix(api): handle nil", false},
		{"hotfix into main with a plain title", "main", "hotfix/x", "Fix the thing", true},
		{"back merge branch into main", "main", "back-merge/1.0.0", "chore(release): back merge 1.0.0", true},
		{"plain title into dev", "dev", "feat/x", "Update stuff", true},
		{"conventional title into dev", "dev", "feat/x", "feat(web): show failed check", false},
		{"breaking change title into dev", "dev", "feat/x", "feat(api)!: drop v1", false},
		{"title without a space after the colon", "dev", "feat/x", "feat:x", true},
		{"unknown type into dev", "dev", "feat/x", "wip: x", true},
		{"renovate title into dev", "dev", "renovate/build-image-digests", "chore(deps): update build image digests", false},
		{"back merge title into dev", "dev", "back-merge/1.0.0", "chore(release): back merge 1.0.0", false},
		{"revert title into dev", "dev", "revert-1", `Revert "feat: x"`, false},
		{"revert word without quotes", "dev", "revert-1", "Revert feat: x", true},
		{"other base passes", "feat/other", "feat/x", "anything", false},
		{"command substitution in the title", "dev", "feat/x", "feat: $(touch " + canary + ")", false},
		{"backticks in the title", "dev", "feat/x", "feat: `touch " + canary + "`", false},
		{"quote in the title", "dev", "feat/x", `feat: it's "quoted"`, false},
		{"command substitution in the head", "main", "$(touch " + canary + ")", "feat: x", true},
		{"newline hides a bad line", "dev", "feat/x", "Update stuff\nfeat: x", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command("sh", "../../scripts/branch-rules.sh", tt.base, tt.head, tt.title).CombinedOutput()
			if failed := err != nil; failed != tt.wantFail {
				t.Fatalf("failed = %v, want %v\n%s", failed, tt.wantFail, out)
			}
			if tt.wantFail && !strings.Contains(string(out), "::error::") {
				t.Errorf("failure without a reason line\n%s", out)
			}
		})
	}
	if _, err := os.Stat(canary); err == nil {
		t.Error("a title or branch name was run as a command")
	}
}

// TestBranchRulesWorkflow covers the value sourcing of spec 0032: branch-rules.yml runs on
// pull_request including edited titles, holds contents: read only, and hands the base, head and
// title to the script through env, never through the script text.
func TestBranchRulesWorkflow(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../.github/workflows/branch-rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"edited", "opened", "synchronize", "reopened", "contents: read",
		"github.base_ref", "github.head_ref", "github.event.pull_request.title",
		`scripts/branch-rules.sh "$BASE" "$HEAD" "$TITLE"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("branch-rules.yml lacks %q", want)
		}
	}
	w, _ := readWorkflow(t, "branch-rules.yml")
	for id, j := range w.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, "${{") {
				t.Errorf("job %s expands an expression in its script: %q", id, s.Run)
			}
		}
	}
}
