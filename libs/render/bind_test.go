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
	Image       string                    `json:"image"`
	Entrypoint  []string                  `json:"entrypoint"`
	Init        bool                      `json:"init"`
	NetworkMode string                    `json:"network_mode"`
	ReadOnly    bool                      `json:"read_only"`
	User        string                    `json:"user"`
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
	out, err := render.Render(plan, catalog, contract.Templates{})
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

// TestWaitAnchor checks the wait anchor mp_jobs (spec 0014, "Waiting"; AC-6): it waits for
// every revoke job and nothing else, idles in the first such job's image on no network with the
// hardening defaults, and is not rendered when every job has a dependent.
func TestWaitAnchor(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	external := contract.GraphModule{External: &contract.ExternalInstance{Provides: map[string]contract.ExternalProvide{"db": {
		Capability: "postgresql", Version: "18.6",
		Fields: map[string]string{"host": "db.example.test", "port": "5432", "url": "postgres://db.example.test:5432", "database": "x", "user": "x", "sslmode": "disable"},
	}}}}
	tests := map[string]struct {
		modules map[string]contract.GraphModule
		waits   []string // nil: no mp_jobs
	}{
		"revoke jobs of two servers": {
			map[string]contract.GraphModule{"postgres": {Module: "postgres"}, "second": {Module: "postgres"}},
			[]string{"postgres_revoke_server", "second_revoke_server"},
		},
		"no binding":    {map[string]contract.GraphModule{"postgres": {Module: "postgres"}}, []string{"postgres_revoke_server"}},
		"no job at all": {map[string]contract.GraphModule{"ext": external, "app": {Module: "pg-roaming"}}, nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: "anchor", Modules: tc.modules}
			services := renderServices(t, g, catalog)
			anchor, ok := services["mp_jobs"]
			if tc.waits == nil {
				if ok {
					t.Fatalf("mp_jobs rendered with no job alone: %+v", anchor)
				}
				return
			}
			if !ok {
				t.Fatalf("no mp_jobs in %v", slices.Sorted(maps.Keys(services)))
			}
			if waits := slices.Sorted(maps.Keys(anchor.DependsOn)); !slices.Equal(waits, tc.waits) {
				t.Errorf("mp_jobs waits for %v, want %v", waits, tc.waits)
			}
			for dep, cond := range anchor.DependsOn {
				if cond["condition"] != "service_completed_successfully" {
					t.Errorf("mp_jobs waits for %s with %v", dep, cond["condition"])
				}
			}
			if anchor.Image != services[tc.waits[0]].Image || anchor.Image == "" {
				t.Errorf("mp_jobs image %q, want the first job's %q", anchor.Image, services[tc.waits[0]].Image)
			}
			if !slices.Equal(anchor.Entrypoint, []string{"sleep", "infinity"}) || !anchor.Init {
				t.Errorf("mp_jobs entrypoint %v init %v", anchor.Entrypoint, anchor.Init)
			}
			if anchor.NetworkMode != "none" || len(anchor.Networks) > 0 {
				t.Errorf("mp_jobs network_mode %q networks %v, want none and no networks", anchor.NetworkMode, anchor.Networks)
			}
			if !anchor.ReadOnly || anchor.User != "${MP_UID}:${MP_GID}" {
				t.Errorf("mp_jobs lacks hardening: read_only %v user %q", anchor.ReadOnly, anchor.User)
			}
		})
	}
}

// TestEgressFromBindings checks that a consumer joins egress by itself when a slot is bound to an
// external instance, directly or through an external provisioner, and stays on internal when
// bound inside the stack (spec 0013, "Host name and networks"; AC-12, AC-1).
func TestEgressFromBindings(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	pgFields := map[string]string{"host": "db.example.test", "port": "5432", "url": "postgres://db.example.test:5432", "sslmode": "disable"}
	direct := map[string]string{"database": "x", "user": "x"}
	maps.Copy(direct, pgFields)
	admin := map[string]string{"admin_user": "admin"}
	maps.Copy(admin, pgFields)
	tests := map[string]struct {
		provider contract.GraphModule
		egress   bool
	}{
		"bound inside the stack": {contract.GraphModule{Module: "postgres"}, false},
		"bound to an external server": {contract.GraphModule{External: &contract.ExternalInstance{Provides: map[string]contract.ExternalProvide{
			"db": {Capability: "postgresql", Version: "18.6", Fields: direct},
		}}}, true},
		"bound through an external provisioner": {contract.GraphModule{External: &contract.ExternalInstance{Provides: map[string]contract.ExternalProvide{
			"admin": {Capability: "postgresql-provisioner", Version: "18.6", Fields: admin},
		}}}, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: "egress", Modules: map[string]contract.GraphModule{
				"app": {Module: "pg-roaming"}, "pg": tc.provider,
			}}
			app := renderServices(t, g, catalog)["app"]
			_, joined := app.Networks["egress"]
			if joined != tc.egress {
				t.Errorf("app joins egress: %v, want %v (networks %v)", joined, tc.egress, app.Networks)
			}
			if _, ok := app.Networks["internal"]; !ok {
				t.Errorf("app is not on internal: %v", app.Networks)
			}
		})
	}
}
