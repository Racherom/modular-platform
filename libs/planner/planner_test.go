package planner_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
)

const fixtures = "../../contracts/fixtures/graphs"

// testCatalog is the built in catalog plus the test modules in testdata/catalog.
func testCatalog(t *testing.T) []contract.CatalogEntry {
	t.Helper()
	var all []contract.CatalogEntry
	for _, dir := range []string{"../../catalog", "testdata/catalog"} {
		entries, problems := contract.LoadCatalog(os.DirFS(dir))
		if len(problems) > 0 {
			t.Fatalf("catalog %s: %v", dir, problems)
		}
		all = append(all, entries...)
	}
	return all
}

func readFixture(t *testing.T, name string) contract.Graph {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		t.Fatalf("fixture %s: %s", name, p.Message)
	}
	return g
}

type want struct {
	ok       bool
	bindings []contract.Binding
	problems []problem
	// resolved lists uses the planner must have written into the graph, as instance.capability: provider
	resolved map[string]string
}

type problem struct {
	severity   contract.ProblemSeverity
	code, path string
	candidates []string
}

func errorAt(code, path string, candidates ...string) problem {
	return problem{contract.Error, code, path, candidates}
}

func bind(consumer, capability, provider string, auto bool) contract.Binding {
	return contract.Binding{Consumer: consumer, Capability: capability, Provider: provider, Auto: auto}
}

// TestPlan covers every row of spec 0002's critical test scenarios (AC-8).
func TestPlan(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)

	tests := map[string]want{
		"sample": {ok: true, bindings: []contract.Binding{bind("whoami", "postgresql", "postgres", false)}},
		"sample-no-uses": {ok: true, problems: []problem{
			{contract.Warning, planner.CodeOptionalUnbound, "modules.whoami.uses.postgresql", []string{"postgres"}},
		}},
		"sample-null": {ok: true},
		"required-one": {
			ok:       true,
			bindings: []contract.Binding{bind("app", "db", "db", true)},
			resolved: map[string]string{"app.db": "db"},
		},
		"required-none": {problems: []problem{errorAt(planner.CodeUnmetNeed, "modules.app.uses.db")}},
		"required-two": {problems: []problem{
			errorAt(planner.CodeAmbiguousProvider, "modules.app.uses.db", "db-a", "db-b"),
		}},
		"required-null":    {problems: []problem{errorAt(planner.CodeUnboundRequired, "modules.app.uses.db")}},
		"self-binding":     {problems: []problem{errorAt(planner.CodeSelfBinding, "modules.app.uses.db")}},
		"unknown-provider": {problems: []problem{errorAt(planner.CodeUnknownProvider, "modules.app.uses.db")}},
		"not-provided": {problems: []problem{
			{contract.Warning, planner.CodeOptionalUnbound, "modules.app.uses.cache", []string{"cache"}},
			errorAt(planner.CodeCapabilityNotProvided, "modules.app.uses.db"),
		}},
		"unknown-module": {problems: []problem{errorAt(planner.CodeUnknownModule, "modules.ghost.module")}},
		"unknown-use": {
			bindings: []contract.Binding{bind("app", "db", "db", true)},
			problems: []problem{errorAt(planner.CodeUnknownUse, "modules.app.uses.queue")},
			resolved: map[string]string{"app.db": "db"},
		},
		"cycle": {
			bindings: []contract.Binding{bind("ping", "pong", "pong", true), bind("pong", "ping", "ping", true)},
			problems: []problem{errorAt(planner.CodeBindingCycle, "modules.ping")},
		},
		"invalid-names": {problems: []problem{
			errorAt(planner.CodeInvalidName, "modules.Web"),
			errorAt(planner.CodeInvalidName, "modules.yes"),
		}},
		"required-and-null": {
			ok:       true,
			bindings: []contract.Binding{bind("app", "db", "db", true)},
			resolved: map[string]string{"app.db": "db"},
		},
		"empty":      {ok: true},
		"empty-uses": {ok: true},
	}

	for name, w := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := readFixture(t, name)
			got := planner.Plan(g, catalog)

			if got.Ok != w.ok {
				t.Errorf("ok = %v, want %v", got.Ok, w.ok)
			}
			if !slices.Equal(got.Bindings, w.bindings) && (len(got.Bindings) > 0 || len(w.bindings) > 0) {
				t.Errorf("bindings = %+v, want %+v", got.Bindings, w.bindings)
			}
			var gotProblems []problem
			for _, p := range got.Problems {
				if p.Message == "" {
					t.Errorf("problem %s at %s has no message", p.Code, p.Path)
				}
				gotProblems = append(gotProblems, problem{p.Severity, p.Code, p.Path, p.Candidates})
			}
			if !slices.EqualFunc(gotProblems, w.problems, equalProblem) {
				t.Errorf("problems = %+v, want %+v", gotProblems, w.problems)
			}
			for key, provider := range w.resolved {
				instance, capability, _ := bytes.Cut([]byte(key), []byte("."))
				uses := got.Graph.Modules[string(instance)].Uses
				if uses == nil || (*uses)[string(capability)] == nil || *(*uses)[string(capability)] != provider {
					t.Errorf("resolved graph lacks %s: %s", key, provider)
				}
			}
			if got.Bindings == nil || got.Problems == nil {
				t.Error("bindings and problems must be arrays, never null")
			}
		})
	}
}

func equalProblem(a, b problem) bool {
	return a.severity == b.severity && a.code == b.code && a.path == b.path && slices.Equal(a.candidates, b.candidates)
}

// TestPlanDeterministic checks that the same request always gives byte identical JSON (AC-8)
// and that planning never changes its input.
func TestPlanDeterministic(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	for _, name := range []string{"cycle", "required-two", "invalid-names", "required-one"} {
		g := readFixture(t, name)
		before, _ := contract.MarshalGraph(g)
		first, err := json.Marshal(planner.Plan(g, catalog))
		if err != nil {
			t.Fatal(err)
		}
		for range 20 {
			again, _ := json.Marshal(planner.Plan(g, catalog))
			if !bytes.Equal(first, again) {
				t.Fatalf("%s: plan differs between runs:\n%s\n%s", name, first, again)
			}
		}
		after, _ := contract.MarshalGraph(g)
		if !bytes.Equal(before, after) {
			t.Errorf("%s: planning changed the input graph", name)
		}
	}
}
