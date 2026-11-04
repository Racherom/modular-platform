package render_test

import (
	"maps"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/render"
)

// composeService is the part of a rendered service these tests read.
type composeService struct {
	Environment map[string]string         `json:"environment"`
	DependsOn   map[string]map[string]any `json:"depends_on"`
	Networks    map[string]any            `json:"networks"`
}

// renderServices renders g and returns its services by name.
func renderServices(t *testing.T, g contract.Graph, catalog contract.Catalog) map[string]composeService {
	t.Helper()
	plan := planner.Plan(g, catalog)
	if !plan.Ok {
		t.Fatalf("plan: %+v", plan.Problems)
	}
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if f.Path != "docker-compose.yml" {
			continue
		}
		var project struct {
			Services map[string]composeService `json:"services"`
		}
		if err := yaml.Unmarshal(f.Data, &project); err != nil {
			t.Fatal(err)
		}
		return project.Services
	}
	t.Fatal("no docker-compose.yml")
	return nil
}

// TestRevokeJob checks the revoke job of a provisioner provide (spec 0014, AC-6): MP_KEEP holds
// the logins of every current binding, sorted, and the job runs after each provision job; with
// no binding it still runs, with an empty MP_KEEP.
func TestRevokeJob(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	pg := contract.GraphModule{Module: "postgres"}
	client := contract.GraphModule{Module: "pg-client"}
	tests := map[string]struct {
		modules map[string]contract.GraphModule
		keep    string
		after   []string
	}{
		"several bindings": {
			map[string]contract.GraphModule{"postgres": pg, "b": client, "a": client, "reports": {Module: "pg-pair"}},
			"a_db,b_db,reports_audit_db,reports_main_db",
			[]string{"postgres", "postgres_provision_a_db", "postgres_provision_b_db", "postgres_provision_reports_audit-db", "postgres_provision_reports_main-db"},
		},
		"no binding": {map[string]contract.GraphModule{"postgres": pg}, "", []string{"postgres"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: "revoke", Modules: tc.modules}
			services := renderServices(t, g, catalog)
			job, ok := services["postgres_revoke_server"]
			if !ok {
				t.Fatalf("no revoke job in %v", slices.Sorted(maps.Keys(services)))
			}
			if keep, set := job.Environment["MP_KEEP"]; !set || keep != tc.keep {
				t.Errorf("MP_KEEP = %q (set %v), want %q", keep, set, tc.keep)
			}
			if after := slices.Sorted(maps.Keys(job.DependsOn)); !slices.Equal(after, tc.after) {
				t.Errorf("depends_on = %v, want %v", after, tc.after)
			}
			for dep, cond := range job.DependsOn {
				if dep != "postgres" && cond["condition"] != "service_completed_successfully" {
					t.Errorf("revoke waits for %s with %v, want service_completed_successfully", dep, cond["condition"])
				}
			}
		})
	}
}
