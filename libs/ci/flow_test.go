package ci_test

import (
	"os"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"
)

// trigger is one event under on:; a bare `pull_request:` is present with every field empty.
type trigger struct {
	Branches []string `json:"branches"`
	Paths    []string `json:"paths"`
	Types    []string `json:"types"`
}

// readTriggers parses the on: block of a workflow file into its events. YAML 1.1 reads the key
// `on` as the boolean true, which the JSON conversion spells "true".
func readTriggers(t *testing.T, name string) map[string]trigger {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		On map[string]*trigger `json:"true"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	out := map[string]trigger{}
	for event, tr := range doc.On {
		if tr == nil {
			tr = &trigger{}
		}
		out[event] = *tr
	}
	return out
}

// TestFlowTriggers covers spec 0032 AC-1 and AC-4: CI runs on pushes to main and dev and on
// every pull request (no base filter, so both bases are covered), the branch rules check reruns
// on an edited title, and release-please is told its branch explicitly.
func TestFlowTriggers(t *testing.T) {
	t.Parallel()
	ci := readTriggers(t, "ci.yml")
	if pr, ok := ci["pull_request"]; !ok || len(pr.Branches) > 0 || len(pr.Paths) > 0 {
		t.Errorf("ci.yml pull_request = %+v (set: %v), want it set with no branches or paths filter", pr, ok)
	}
	if !slices.Equal(ci["push"].Branches, []string{"main", "dev"}) {
		t.Errorf("ci.yml push = %+v, want branches main and dev", ci["push"])
	}

	br := readTriggers(t, "branch-rules.yml")
	if !slices.Contains(br["pull_request"].Types, "edited") {
		t.Errorf("branch-rules.yml pull_request = %+v, want the edited type", br["pull_request"])
	}

	rel := readTriggers(t, "release.yml")
	if !slices.Equal(rel["push"].Branches, []string{"main"}) {
		t.Errorf("release.yml push = %+v, want branches main only", rel["push"])
	}
	w, _ := readWorkflow(t, "release.yml")
	i := stepIndex(w.Jobs["release-please"], "release-please-action")
	if i < 0 {
		t.Fatal("release.yml has no release-please step")
	}
	if got := w.Jobs["release-please"].Steps[i].With["target-branch"]; got != "main" {
		t.Errorf("release-please target-branch = %v, want main", got)
	}
}
