package planner_test

import (
	"os"
	"slices"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
)

// platformGraph is the managed platform's own graph (spec 0015).
const platformGraph = "../../deploy/managed/graph.yaml"

// TestPlatformGraph covers spec 0015: the managed platform's eight modules are built in catalog
// entries, and the platform graph plans ok with warnings host_access at ingress and
// deployer-docker only, every binding where 0015 puts it, postgres.tls on the certificate
// provisioner included (AC-16, AC-17, AC-2).
func TestPlatformGraph(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	for _, name := range []string{"api", "authelia", "caddy", "cert-issuer", "deployer-docker", "planner", "postgres", "web"} {
		if !slices.ContainsFunc(catalog.Modules, func(m contract.Module) bool { return m.Name == name }) {
			t.Errorf("the built in catalog has no module %s", name)
		}
	}
	data, err := os.ReadFile(platformGraph)
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
	// the graph file is in canonical form, so the editor and the render write it back unchanged
	if text, err := contract.MarshalGraph(g); err != nil || string(text) != string(data) {
		t.Errorf("deploy/managed/graph.yaml is not in canonical form:\n%s", text)
	}
}
