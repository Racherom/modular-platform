package ci_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"
)

// githubActionsAppID is the id of the GitHub Actions app. A required check pinned to it cannot be
// reported by another app.
const githubActionsAppID = 15368

// ruleset is the part of a GitHub ruleset export this test reads.
type ruleset struct {
	Name         string            `json:"name"`
	Target       string            `json:"target"`
	Enforcement  string            `json:"enforcement"`
	BypassActors []json.RawMessage `json:"bypass_actors"`
	Conditions   struct {
		RefName struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []struct {
		Type       string          `json:"type"`
		Parameters json.RawMessage `json:"parameters"`
	} `json:"rules"`
}

type pullRequestRule struct {
	RequiredApprovals int      `json:"required_approving_review_count"`
	MergeMethods      []string `json:"allowed_merge_methods"`
}

type statusChecksRule struct {
	Strict bool `json:"strict_required_status_checks_policy"`
	Checks []struct {
		Context       string `json:"context"`
		IntegrationID int    `json:"integration_id"`
	} `json:"required_status_checks"`
}

// rule returns the parameters of the rule of the given type, and whether it is there.
func (r ruleset) rule(kind string) (json.RawMessage, bool) {
	for _, rule := range r.Rules {
		if rule.Type == kind {
			return rule.Parameters, true
		}
	}
	return nil, false
}

// requiredChecks is the check contexts the two rulesets must name.
var requiredChecks = []string{
	"Generate, lint, test, build",
	"Integration (Docker, sdk end to end)",
	"Smoke (online editor, sample zip, sub path)",
	"Smoke (managed platform, install to running stack)",
	"Branch rules",
}

func readRuleset(t *testing.T, name string) ruleset {
	t.Helper()
	data, err := os.ReadFile("../../deploy/github/ruleset-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var r ruleset
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("ruleset-%s.json: %v", name, err)
	}
	return r
}

// TestRulesets covers spec 0032 AC-2: both rulesets protect their branch with no bypass actor,
// block delete and force push, require a pull request with no approval count, require the five
// checks pinned to the GitHub Actions app, have no up to date rule, and allow the merge methods
// the spec names (dev: squash and merge; main: merge only).
func TestRulesets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		branch  string
		methods []string
	}{
		{"main", []string{"merge"}},
		{"dev", []string{"squash", "merge"}},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			t.Parallel()
			r := readRuleset(t, tt.branch)
			if r.Name != tt.branch || r.Target != "branch" || r.Enforcement != "active" {
				t.Errorf("name %q target %q enforcement %q, want %q branch active", r.Name, r.Target, r.Enforcement, tt.branch)
			}
			if want := []string{"refs/heads/" + tt.branch}; !slices.Equal(r.Conditions.RefName.Include, want) || len(r.Conditions.RefName.Exclude) > 0 {
				t.Errorf("ref_name = %+v, want include %v only", r.Conditions.RefName, want)
			}
			if len(r.BypassActors) > 0 {
				t.Errorf("bypass actors %s, want none", r.BypassActors)
			}
			for _, kind := range []string{"deletion", "non_fast_forward"} {
				if _, ok := r.rule(kind); !ok {
					t.Errorf("no %s rule", kind)
				}
			}
			var pr pullRequestRule
			raw, ok := r.rule("pull_request")
			if !ok {
				t.Fatal("no pull_request rule")
			}
			if err := json.Unmarshal(raw, &pr); err != nil {
				t.Fatal(err)
			}
			if pr.RequiredApprovals != 0 {
				t.Errorf("required approvals = %d, want 0 (one maintainer)", pr.RequiredApprovals)
			}
			if !slices.Equal(slices.Sorted(slices.Values(pr.MergeMethods)), slices.Sorted(slices.Values(tt.methods))) {
				t.Errorf("merge methods %v, want %v", pr.MergeMethods, tt.methods)
			}
			var sc statusChecksRule
			raw, ok = r.rule("required_status_checks")
			if !ok {
				t.Fatal("no required_status_checks rule")
			}
			if err := json.Unmarshal(raw, &sc); err != nil {
				t.Fatal(err)
			}
			if sc.Strict {
				t.Error("strict_required_status_checks_policy is on; the up to date rule would block the next dev into main pull request")
			}
			var got []string
			for _, c := range sc.Checks {
				got = append(got, c.Context)
				if c.IntegrationID != githubActionsAppID {
					t.Errorf("check %q is pinned to app %d, want the GitHub Actions app %d", c.Context, c.IntegrationID, githubActionsAppID)
				}
			}
			if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(requiredChecks))) {
				t.Errorf("required checks %q, want %q", got, requiredChecks)
			}
		})
	}
}

// workflowFile is the part of a workflow the check name test reads. Strategy and Uses stay loose
// because only their presence matters.
type workflowFile struct {
	Jobs map[string]struct {
		Name     string         `json:"name"`
		If       string         `json:"if"`
		Uses     string         `json:"uses"`
		Strategy map[string]any `json:"strategy"`
	} `json:"jobs"`
}

// TestRequiredChecksMatchJobs covers spec 0032 AC-8: every check a ruleset requires is the name
// (else the id) of exactly one job in .github/workflows/, so a renamed job cannot leave a
// required check that never reports. That job is no matrix or reusable workflow job, has no job
// level if, and its workflow runs on pull_request with no branches or paths filter, so the check
// reports for a pull request into dev and into main alike.
func TestRequiredChecksMatchJobs(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	type found struct{ file, id string }
	byName := map[string][]found{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var w workflowFile
		if err := yaml.Unmarshal(data, &w); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for id, j := range w.Jobs {
			name := j.Name
			if name == "" {
				name = id
			}
			byName[name] = append(byName[name], found{filepath.Base(f), id})
		}
	}
	for _, branch := range []string{"main", "dev"} {
		r := readRuleset(t, branch)
		raw, _ := r.rule("required_status_checks")
		var sc statusChecksRule
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		for _, c := range sc.Checks {
			jobs := byName[c.Context]
			if len(jobs) != 1 {
				t.Errorf("ruleset %s: check %q matches %d jobs (%v), want exactly 1", branch, c.Context, len(jobs), jobs)
				continue
			}
			checkReports(t, branch, c.Context, jobs[0].file, jobs[0].id)
		}
	}
}

// checkReports fails unless the job id of the workflow file reports its check for every pull
// request.
func checkReports(t *testing.T, branch, check, file, id string) {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var w workflowFile
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	j := w.Jobs[id]
	switch {
	case j.Strategy["matrix"] != nil:
		t.Errorf("ruleset %s: %q (%s) is a matrix job, which reports one check per cell", branch, check, file)
	case j.Uses != "":
		t.Errorf("ruleset %s: %q (%s) is a reusable workflow job, which reports under another name", branch, check, file)
	case j.If != "":
		t.Errorf("ruleset %s: %q (%s) has a job level if, so it may never report", branch, check, file)
	}
	tr := readTriggers(t, file)
	pr, ok := tr["pull_request"]
	if !ok || len(pr.Branches) > 0 || len(pr.Paths) > 0 {
		t.Errorf("ruleset %s: %q (%s) does not run on every pull_request (set: %v, filters: %+v)", branch, check, file, ok, pr)
	}
}
