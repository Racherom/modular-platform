//go:build integration

package ci_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBranchRulesAct covers spec 0032 AC-9 for the branch rules job: act runs the real
// branch-rules.yml in local Docker (actions/checkout replaced by a stub) and the job passes for a
// good pull request and fails, with the reason, for a feature branch into main.
func TestBranchRulesAct(t *testing.T) {
	root := repoRoot(t)
	act := newActRunner(t, root)
	mappings := []string{act.stubFor(t, "branch-rules.yml", "actions/checkout", "checkout-stub")}

	tests := []struct {
		name              string
		base, head, title string
		wantFail          bool
		want              string
	}{
		{"feature into dev", "dev", "feat/x", "feat(web): show a failed check", false, "branch rules pass: feat/x into dev"},
		{"bad title into dev", "dev", "feat/x", "Update stuff", true, "the title must be a conventional commit"},
		{"feature into main", "main", "feat/x", "feat: x", true, "main takes only dev"},
		{"dev into main", "main", "dev", "Release", false, "branch rules pass: dev into main"},
		{"quoted title is data", "dev", "feat/x", `feat: "$(echo hacked)" ` + "`id`", false, "branch rules pass"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"action": "opened",
				"pull_request": map[string]any{
					"title": tt.title,
					"base":  map[string]any{"ref": tt.base},
					"head":  map[string]any{"ref": tt.head},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := act.run(t, "pull_request", ".github/workflows/branch-rules.yml", act.event(t, string(payload)), mappings)
			if tt.wantFail == (err == nil) {
				t.Fatalf("act error = %v, want failure = %v\n%s", err, tt.wantFail, output)
			}
			if !strings.Contains(output, tt.want) {
				t.Errorf("output lacks %q\n%s", tt.want, output)
			}
			if strings.Contains(output, "hacked\n") && !strings.Contains(output, `"$(echo hacked)"`) {
				t.Errorf("the title was run as a command\n%s", output)
			}
		})
	}
}
