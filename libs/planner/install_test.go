package planner_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
)

// TestPlanInstallTime covers the install time checks of spec 0013: setup_order for every kind
// of read a step cannot have when it runs, and env_name_collision (AC-10).
func TestPlanInstallTime(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)

	tests := map[string]struct {
		graph    string
		problems []problem
		messages []string // a text each problem message holds, in order
	}{
		"steps in order": {graph: `
  sealer: {module: sealer}
  web: {module: sealed-client}
  api: {module: sealed-client}`},
		"client key written after the provider's steps": {
			graph: `
  sealer: {module: sealer}
  late: {module: late-client}`,
			problems: []problem{errorAt(planner.CodeSetupOrder, "modules.sealer")},
			messages: []string{"Setup step mark of sealer reads field key of provide seal, which client late writes in a setup step that runs after this one."},
		},
		"later output, run time file, text field": {
			graph: `
  sealer: {module: sealer}
  wrong: {module: wrong-steps}`,
			problems: []problem{
				errorAt(planner.CodeSetupOrder, "modules.wrong"),
				errorAt(planner.CodeSetupOrder, "modules.wrong"),
				errorAt(planner.CodeSetupOrder, "modules.wrong"),
			},
			messages: []string{
				"reads secret second-out, which no setup step before it writes.",
				"reads field stamp of slot seal, a file the provider writes into its data folder at run time.",
				"reads field url of slot seal, which is text, and a step reads only files.",
			},
		},
		"placed file of an external": {
			graph: `
  outside:
    external:
      provides:
        seal: {capability: seal, version: "1.0", fields: {url: "https://seal.example.test"}}
  web: {module: sealed-client}`,
			problems: []problem{errorAt(planner.CodeSetupOrder, "modules.web")},
			messages: []string{"reads field root of slot seal, a file you place, which setup.sh checks only after the setup steps."},
		},
		"two values, one variable": {
			graph: `
  x: {module: twin}
  x--a: {module: twin}`,
			problems: []problem{errorAt(planner.CodeEnvNameCollision, "modules.x--a")},
			messages: []string{"The config value a--b of x and the config value b of x--a both become the Compose variable MP_X__A__B"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g, p := contract.DecodeGraph([]byte("apiVersion: modular-platform/v1\nkind: Graph\nname: install\nmodules:" + tc.graph + "\n"))
			if p != nil {
				t.Fatal(p.Message)
			}
			got := planner.Plan(g, catalog)
			var gotProblems []problem
			for _, pr := range got.Problems {
				gotProblems = append(gotProblems, problem{pr.Severity, pr.Code, pr.Path, pr.Candidates})
			}
			if !slices.EqualFunc(gotProblems, tc.problems, equalProblem) {
				t.Fatalf("problems = %+v, want %+v", got.Problems, tc.problems)
			}
			if got.Ok != (len(tc.problems) == 0) {
				t.Errorf("ok = %v", got.Ok)
			}
			for n, text := range tc.messages {
				if !strings.Contains(got.Problems[n].Message, text) {
					t.Errorf("problem %d says %q, want it to hold %q", n, got.Problems[n].Message, text)
				}
			}
		})
	}
}

// TestBindingOrder pins the order setup.sh runs setup steps in: providers before consumers, ties
// by name.
func TestBindingOrder(t *testing.T) {
	t.Parallel()
	bindings := []contract.Binding{
		{Consumer: "alpha", Provider: "keeper"},
		{Consumer: "beta", Provider: "keeper"},
		{Consumer: "keeper", Provider: "zeta"},
	}
	got := contract.BindingOrder([]string{"alpha", "beta", "keeper", "zeta", "gamma"}, bindings)
	if want := []string{"gamma", "zeta", "keeper", "alpha", "beta"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestPlanHostAccess warns, never refuses, at each instance whose module publishes a port,
// mounts a host path or adds a Linux capability other than NET_BIND_SERVICE, one warning per
// instance naming every reason (spec 0013, Host access; spec 0022; AC-11).
func TestPlanHostAccess(t *testing.T) {
	t.Parallel()
	g, p := contract.DecodeGraph([]byte(`apiVersion: modular-platform/v1
kind: Graph
name: reach
modules:
  web: {module: edge}
  agent: {module: socket}
  plain: {module: twin}
  admin: {module: sysadmin}
  low: {module: lowport}
  gate: {module: gateway}
`))
	if p != nil {
		t.Fatal(p.Message)
	}
	got := planner.Plan(g, testCatalog(t))
	if !got.Ok {
		t.Fatalf("plan not ok: %+v", got.Problems)
	}
	var gotProblems []string
	for _, pr := range got.Problems {
		gotProblems = append(gotProblems, fmt.Sprintf("%s %s %s: %s", pr.Severity, pr.Code, pr.Path, pr.Message))
	}
	const tail = ", so it reaches the host it runs on; a deployed graph refuses this."
	want := []string{
		"warning host_access modules.admin: Instance admin adds the Linux capability SYS_ADMIN" + tail,
		"warning host_access modules.agent: Instance agent mounts host paths" + tail,
		"warning host_access modules.gate: Instance gate publishes host ports and adds the Linux capability SYS_ADMIN" + tail,
		"warning host_access modules.web: Instance web publishes host ports" + tail,
	}
	if !slices.Equal(gotProblems, want) {
		t.Fatalf("problems =\n%s\nwant\n%s", strings.Join(gotProblems, "\n"), strings.Join(want, "\n"))
	}
	if got.Digest == "" {
		t.Error("an ok plan with warnings has no digest")
	}
}
