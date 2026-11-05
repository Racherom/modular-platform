package planner_test

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
)

// TestPaperPlatform is build task 9 of spec 0011: the managed platform's eight modules of spec
// 0015, written as v1 entries beside the built in capabilities, load with no catalog problem,
// and the platform graph plans ok, every binding where 0015 puts it (AC-16, AC-2).
func TestPaperPlatform(t *testing.T) {
	t.Parallel()
	merged := fstest.MapFS{}
	add := func(dir, prefix string) {
		fsys := os.DirFS(dir)
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasPrefix(path, prefix) {
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
	add("../../catalog", "capabilities/")
	add("../contract/testdata/platform", "")
	catalog, problems := contract.LoadCatalog(merged)
	for _, p := range problems {
		t.Error(p)
	}
	if len(catalog.Modules) != 8 {
		t.Fatalf("modules = %d, want the eight platform modules", len(catalog.Modules))
	}

	data, err := os.ReadFile("../contract/testdata/platform/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		t.Fatal(p.Message)
	}
	plan := planner.Plan(g, catalog)
	var planProblems []string
	for _, p := range plan.Problems {
		planProblems = append(planProblems, string(p.Severity)+" "+p.Code+" "+p.Path)
	}
	// the ingress publishes its ports and the deployer mounts the Docker socket (spec 0015)
	wantProblems := []string{"warning host_access modules.deployer-docker", "warning host_access modules.ingress"}
	if !plan.Ok || !slices.Equal(planProblems, wantProblems) {
		t.Fatalf("plan: ok %v, problems %v, want %v", plan.Ok, planProblems, wantProblems)
	}
	var got []string
	for _, b := range plan.Bindings {
		got = append(got, b.Consumer+"."+b.Slot+" "+b.Provider+"."+b.Provide)
	}
	want := []string{
		"api.acme cert-issuer.acme", "api.deploy-target deployer-docker.deploy", "api.main-db postgres.server",
		"api.oidc authelia.oidc", "api.planner planner.planner", "api.public ingress.public",
		"authelia.db postgres.server", "authelia.public ingress.public",
		"deployer-docker.acme cert-issuer.acme", "deployer-docker.ingress ingress.public",
		"ingress.trust cert-issuer.acme",
		"planner.acme cert-issuer.acme", "planner.ingress ingress.public",
		"postgres.tls cert-issuer.certs",
		"web.public ingress.public",
	}
	if !slices.Equal(got, want) {
		t.Errorf("bindings =\n%q\nwant\n%q", got, want)
	}
}
