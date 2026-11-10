package check_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
)

const fixtures = "../../contracts/fixtures/graphs"

// testCatalog is the built in catalog plus the test capabilities and modules in
// testdata/catalog, loaded as one catalog so test modules can name built in capabilities.
func testCatalog(t *testing.T) contract.Catalog {
	t.Helper()
	merged := fstest.MapFS{}
	for _, dir := range []string{"../../catalog", "testdata/catalog"} {
		fsys := os.DirFS(dir)
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".tmpl") {
				return err
			}
			data, err := fs.ReadFile(fsys, path)
			merged[path] = &fstest.MapFile{Data: data}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	c, problems := contract.LoadCatalog(merged)
	if len(problems) > 0 {
		t.Fatalf("catalog: %v", problems)
	}
	return c
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
	// resolved lists bindings the checks must have written into the graph, as
	// instance.slot: provider text, then named values as field=value in name order
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

// bind is a binding of slot to provider.provide; a slot named like its capability passes "" as
// capability.
func bind(consumer, slot, capability, provider, provide string) contract.Binding {
	if capability == "" {
		capability = slot
	}
	return contract.Binding{Consumer: consumer, Slot: slot, Capability: capability, Provider: provider, Provide: provide}
}

// provisioned marks b as a binding to a provisioner; named marks the checks wrote its names.
func provisioned(b contract.Binding, named bool) contract.Binding {
	b.Provisioned, b.Named = true, named
	return b
}

func warningAt(code, path string, candidates ...string) problem {
	return problem{contract.Warning, code, path, candidates}
}

// TestCheck covers every row of spec 0002's critical test scenarios, rewritten in v1, and spec
// 0012's (AC-3, AC-4, AC-7, AC-15).
func TestCheck(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)

	tests := map[string]want{
		"sample": {
			ok:       true,
			bindings: []contract.Binding{provisioned(bind("whoami", "postgresql", "", "postgres", "server"), true)},
			resolved: map[string]string{"whoami.postgresql": "postgres database=whoami_postgresql user=whoami_postgresql"},
		},
		"sample-provisioned": {
			ok:       true,
			bindings: []contract.Binding{provisioned(bind("whoami", "postgresql", "", "postgres", "server"), false)},
			resolved: map[string]string{"whoami.postgresql": "postgres database=demo user=demo"},
		},
		"sample-no-uses": {ok: true, problems: []problem{
			warningAt(check.CodeOptionalUnbound, "modules.whoami.uses.postgresql", "postgres"),
		}},
		"sample-null":   {ok: true},
		"required-one":  {problems: []problem{errorAt(check.CodeBindingRecommended, "modules.app.uses.db", "db")}},
		"required-none": {problems: []problem{errorAt(check.CodeUnmetNeed, "modules.app.uses.db")}},
		"required-two": {problems: []problem{
			errorAt(check.CodeAmbiguousProvider, "modules.app.uses.db", "db-a", "db-b"),
		}},
		"required-null":    {problems: []problem{errorAt(check.CodeUnboundRequired, "modules.app.uses.db")}},
		"self-binding":     {problems: []problem{errorAt(check.CodeSelfBinding, "modules.app.uses.db")}},
		"unknown-provider": {problems: []problem{errorAt(check.CodeUnknownProvider, "modules.app.uses.db")}},
		"not-provided": {problems: []problem{
			warningAt(check.CodeOptionalUnbound, "modules.app.uses.cache", "cache"),
			errorAt(check.CodeCapabilityNotProvided, "modules.app.uses.db"),
		}},
		"unknown-module": {problems: []problem{errorAt(check.CodeUnknownModule, "modules.ghost.module")}},
		"unknown-use": {
			bindings: []contract.Binding{bind("app", "db", "", "db", "db")},
			problems: []problem{errorAt(check.CodeUnknownUse, "modules.app.uses.queue")},
			resolved: map[string]string{"app.db": "db"},
		},
		"cycle": {
			bindings: []contract.Binding{bind("ping", "pong", "", "pong", "pong"), bind("pong", "ping", "", "ping", "ping")},
			problems: []problem{errorAt(check.CodeBindingCycle, "modules.ping")},
		},
		"invalid-names": {problems: []problem{
			errorAt(check.CodeInvalidName, "modules.Web"),
			errorAt(check.CodeInvalidName, "modules.yes"),
		}},
		"required-and-null": {problems: []problem{errorAt(check.CodeBindingRecommended, "modules.app.uses.db", "db")}},
		"missing-token-mapping": {
			bindings: []contract.Binding{bind("caller", "greet", "", "greeter", "greet"), bind("notoken", "greet", "", "greeter", "greet")},
			problems: []problem{errorAt(check.CodeMissingTokenMapping, "modules.notoken.uses.greet")},
		},
		"empty":      {ok: true},
		"empty-uses": {ok: true},
		"range-match": {
			ok:       true,
			bindings: []contract.Binding{provisioned(bind("modern", "db", "postgresql", "postgres", "server"), true)},
			resolved: map[string]string{"modern.db": "postgres database=modern_db user=modern_db"},
		},
		"range-mismatch": {problems: []problem{
			errorAt(check.CodeVersionMismatch, "modules.modern.uses.db", "legacy"),
		}},
		"range-mismatch-explicit": {problems: []problem{
			errorAt(check.CodeVersionMismatch, "modules.modern.uses.db", "legacy.db"),
		}},
		"ambiguous-provide": {problems: []problem{
			errorAt(check.CodeAmbiguousProvide, "modules.modern.uses.db", "pg.primary", "pg.replica"),
		}},
		"named-provide": {
			ok:       true,
			bindings: []contract.Binding{bind("modern", "db", "postgresql", "pg", "replica")},
			resolved: map[string]string{"modern.db": "pg.replica"},
		},
		"unknown-provide": {problems: []problem{errorAt(check.CodeUnknownProvide, "modules.modern.uses.db")}},
		"two-slots": {
			ok: true,
			bindings: []contract.Binding{
				bind("reports", "audit-db", "postgresql", "b", "primary"),
				bind("reports", "main-db", "postgresql", "a", "db"),
			},
		},
		"two-slots-duplicate": {
			bindings: []contract.Binding{bind("reports", "audit-db", "postgresql", "a", "db")},
			problems: []problem{errorAt(check.CodeDuplicateBinding, "modules.reports.uses.main-db")},
		},
		"two-slots-provisioned": {
			ok: true,
			bindings: []contract.Binding{
				provisioned(bind("reports", "audit-db", "postgresql", "postgres", "server"), true),
				provisioned(bind("reports", "main-db", "postgresql", "postgres", "server"), true),
			},
			resolved: map[string]string{
				"reports.audit-db": "postgres database=reports_audit_db user=reports_audit_db",
				"reports.main-db":  "postgres database=reports_main_db user=reports_main_db",
			},
		},
		"named-direct": {problems: []problem{
			errorAt(check.CodeUnboundRequired, "modules.reports.uses.audit-db"),
			errorAt(check.CodeNamedNotAllowed, "modules.reports.uses.main-db"),
		}},
		"named-unknown": {problems: []problem{
			errorAt(check.CodeUnknownNamedField, "modules.whoami.uses.postgresql.owner"),
		}},
		"named-invalid": {problems: []problem{
			errorAt(check.CodeInvalidNamedValue, "modules.whoami.uses.postgresql.database"),
		}},
		"named-admin": {
			bindings: []contract.Binding{provisioned(bind("whoami", "postgresql", "", "postgres", "server"), true)},
			problems: []problem{errorAt(check.CodeInvalidNamedValue, "modules.whoami.uses.postgresql.user")},
		},
		"named-duplicate": {
			bindings: []contract.Binding{
				provisioned(bind("reports", "audit-db", "postgresql", "postgres", "server"), true),
				provisioned(bind("reports", "main-db", "postgresql", "postgres", "server"), true),
			},
			problems: []problem{errorAt(check.CodeDuplicateNamedValue, "modules.reports.uses.main-db.database")},
		},
		"external": {
			ok:       true,
			bindings: []contract.Binding{bind("modern", "db", "postgresql", "billing-db", "db")},
			resolved: map[string]string{"modern.db": "billing-db"},
		},
		"config-set": {ok: true},
		"config-problems": {problems: []problem{
			errorAt(check.CodeUnknownConfig, "modules.a.config.ghost"),
			errorAt(check.CodeInvalidConfig, "modules.a.config.label"),
			errorAt(check.CodeInvalidConfig, "modules.a.config.level"),
			errorAt(check.CodeInvalidConfig, "modules.a.config.mode"),
			errorAt(check.CodeInvalidConfig, "modules.a.config.need"),
			errorAt(check.CodeConfigNotSettable, "modules.a.config.token"),
			errorAt(check.CodeConfigNotSettable, "modules.a.config.uid"),
			errorAt(check.CodeMissingConfig, "modules.c.config.need"),
			errorAt(check.CodeConfigNotAllowed, "modules.e.config.need"),
			warningAt(check.CodeHostAccess, "modules.m"),
			errorAt(check.CodeConfigNotSettable, "modules.m.config.root"),
			warningAt(check.CodeMissingConfig, "modules.s.config.need"),
		}},
		"config-quotes": {problems: []problem{
			errorAt(check.CodeInvalidConfig, "modules.q1.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.q2.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.q3.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.q4.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.q5.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.s1.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.s2.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.s3.config.need"),
			errorAt(check.CodeInvalidConfig, "modules.s4.config.need"),
		}},
		"external-missing": {
			bindings: []contract.Binding{bind("modern", "db", "postgresql", "billing-db", "db")},
			problems: []problem{errorAt(check.CodeExternalFieldMissing, "modules.billing-db.external.provides.db.fields.host")},
		},
	}

	for name, w := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := readFixture(t, name)
			got := check.Graph(g, catalog)

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
			for key, text := range w.resolved {
				instance, slot, _ := strings.Cut(key, ".")
				uses := got.Graph.Modules[instance].Uses
				if uses == nil || (*uses)[slot] == nil || bindingText(*(*uses)[slot]) != text {
					t.Errorf("resolved graph lacks %s: %s", key, text)
				}
			}
			if got.Bindings == nil || got.Problems == nil {
				t.Error("bindings and problems must be arrays, never null")
			}
		})
	}
}

// bindingText writes a graph binding as "provider field=value ...", named values in name order.
func bindingText(b contract.GraphBinding) string {
	parts := []string{b.Provider}
	for _, k := range slices.Sorted(maps.Keys(b.Named)) {
		parts = append(parts, k+"="+b.Named[k])
	}
	return strings.Join(parts, " ")
}

func equalProblem(a, b problem) bool {
	return a.severity == b.severity && a.code == b.code && a.path == b.path && slices.Equal(a.candidates, b.candidates)
}

// TestCheckDeterministic checks that the same request always gives byte identical JSON (AC-8)
// and that checking never changes its input.
func TestCheckDeterministic(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	for _, name := range []string{"cycle", "required-two", "invalid-names", "required-one"} {
		g := readFixture(t, name)
		before, _ := contract.MarshalGraph(g)
		first, err := json.Marshal(check.Graph(g, catalog))
		if err != nil {
			t.Fatal(err)
		}
		for range 20 {
			again, _ := json.Marshal(check.Graph(g, catalog))
			if !bytes.Equal(first, again) {
				t.Fatalf("%s: result differs between runs:\n%s\n%s", name, first, again)
			}
		}
		after, _ := contract.MarshalGraph(g)
		if !bytes.Equal(before, after) {
			t.Errorf("%s: checking changed the input graph", name)
		}
	}
}

// TestDefaultName checks spec 0014's default for a missing named value (AC-7).
func TestDefaultName(t *testing.T) {
	t.Parallel()
	if got := check.DefaultName("reports", "main-db"); got != "reports_main_db" {
		t.Errorf("DefaultName = %q, want reports_main_db", got)
	}
	long := strings.Repeat("a", 60)
	got := check.DefaultName(long, "slot")
	if len(got) != 63 || !strings.HasPrefix(got, long[:54]+"_") || got == check.DefaultName(long, "other") {
		t.Errorf("DefaultName of a long name = %q, want 54 characters, _ and 8 hex digits, unique per slot", got)
	}
}

// TestCheckV0alpha1 checks every v0alpha1 fixture and its v1 twin: the same bindings and problems
// plus the converted_from_v0alpha1 warning, and the same digest (AC-14).
func TestCheckV0alpha1(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	files, err := filepath.Glob(filepath.Join(fixtures, "v0alpha1", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no v0alpha1 fixtures: %v", err)
	}
	// twins whose v1 fixture names the bindings the v0alpha1 one leaves out (spec 0028): the
	// conversion adds no binding, so the converted graph shows a recommendation for each gap
	gaps := map[string][]string{
		"cycle": {"error binding_recommended modules.ping.uses.pong [pong]", "error binding_recommended modules.pong.uses.ping [ping]"},
		"missing-token-mapping": {
			"error binding_recommended modules.caller.uses.greet [greeter]", "error binding_recommended modules.notoken.uses.greet [greeter]",
		},
		"unknown-use": {"error binding_recommended modules.app.uses.db [db]", "error unknown_use modules.app.uses.queue []"},
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			old := check.Graph(readFixture(t, filepath.Join("v0alpha1", name)), catalog)
			v1 := check.Graph(readFixture(t, name), catalog)
			warn := contract.Problem{Severity: contract.Warning, Code: contract.CodeConvertedFromV0alpha1, Path: "apiVersion"}
			idx := slices.IndexFunc(old.Problems, func(p contract.Problem) bool { return p.Code == warn.Code })
			if idx < 0 || old.Problems[idx].Path != warn.Path || old.Problems[idx].Severity != warn.Severity {
				t.Fatalf("problems = %+v, want the converted warning", old.Problems)
			}
			rest := slices.Delete(slices.Clone(old.Problems), idx, idx+1)
			if want, ok := gaps[name]; ok {
				var got []string
				for _, p := range rest {
					got = append(got, fmt.Sprintf("%s %s %s %v", p.Severity, p.Code, p.Path, p.Candidates))
				}
				if old.Ok || len(old.Bindings) != 0 || !slices.Equal(got, want) {
					t.Errorf("ok %v, bindings %v, problems %q, want not ok, no binding, %q", old.Ok, old.Bindings, got, want)
				}
				return
			}
			oldJSON, _ := json.Marshal([]any{old.Ok, old.Graph, old.Bindings, rest, old.Digest})
			v1JSON, _ := json.Marshal([]any{v1.Ok, v1.Graph, v1.Bindings, v1.Problems, v1.Digest})
			if !bytes.Equal(oldJSON, v1JSON) {
				t.Errorf("v0alpha1 result differs from v1:\n%s\n%s", oldJSON, v1JSON)
			}
		})
	}
}

// TestRecommendationIsNotABinding covers spec 0027 AC-7 and spec 0028: a required use left out
// with one match is the error binding_recommended with that provide as its only candidate, and
// the checks write nothing for it, neither into the graph nor into the bindings.
func TestRecommendationIsNotABinding(t *testing.T) {
	t.Parallel()
	result := check.Graph(readFixture(t, "required-one"), testCatalog(t))
	if result.Ok || result.Digest != "" || len(result.Bindings) != 0 {
		t.Errorf("ok %v, digest %q, bindings %v, want not ok, no digest, no binding", result.Ok, result.Digest, result.Bindings)
	}
	if uses := result.Graph.Modules["app"].Uses; uses != nil {
		if _, written := (*uses)["db"]; written {
			t.Errorf("the resolved graph binds app.db: %v", (*uses)["db"])
		}
	}
	want := "Instance app needs db for db; db provides it. Accept the recommendation or pick another provider."
	if len(result.Problems) != 1 || result.Problems[0].Message != want {
		t.Errorf("problems = %+v, want one with message %q", result.Problems, want)
	}
}
