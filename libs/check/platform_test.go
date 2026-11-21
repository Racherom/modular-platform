package check_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
)

// platformGraph is the managed platform's own graph (spec 0015).
const platformGraph = "../../deploy/managed/graph.yaml"

// TestPlatformGraph covers spec 0015: the managed platform's eight modules are built in catalog
// entries, and the platform graph checks ok with warnings host_access at ingress and
// deployer-docker only, every binding where 0015 puts it, postgres.tls on the certificate
// provisioner included (AC-16, AC-17, AC-2).
func TestPlatformGraph(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	for _, name := range []string{"api", "authelia", "caddy", "cert-issuer", "deployer-docker", "postgres", "renderer", "web"} {
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
	result := check.Graph(g, catalog)
	var resultProblems []string
	for _, p := range result.Problems {
		resultProblems = append(resultProblems, string(p.Severity)+" "+p.Code+" "+p.Path)
	}
	// the ingress publishes its ports and the deployer mounts the Docker socket (spec 0015)
	wantProblems := []string{"warning host_access modules.deployer-docker", "warning host_access modules.ingress"}
	if !result.Ok || !slices.Equal(resultProblems, wantProblems) {
		t.Fatalf("result: ok %v, problems %v, want %v", result.Ok, resultProblems, wantProblems)
	}
	var got []string
	for _, b := range result.Bindings {
		got = append(got, b.Consumer+"."+b.Slot+" "+b.Provider+"."+b.Provide)
	}
	want := []string{
		"api.acme cert-issuer.acme", "api.deploy-target deployer-docker.deploy", "api.main-db postgres.server",
		"api.oidc authelia.oidc", "api.public ingress.public", "api.renderer renderer.renderer",
		"authelia.db postgres.server", "authelia.public ingress.public",
		"deployer-docker.acme cert-issuer.acme", "deployer-docker.ingress ingress.public",
		"ingress.trust cert-issuer.acme",
		"postgres.tls cert-issuer.certs",
		"renderer.acme cert-issuer.acme", "renderer.ingress ingress.public",
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

// TestOIDCCapability covers the changes of spec 0037, task 5b (AC-9): the oidc capability has no
// admin_email and a to_provider logout_path, its client_id pattern takes the default id of the
// client step, and a consumer that leaves out logout_path is refused.
func TestOIDCCapability(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	var oidc contract.Capability
	for _, k := range catalog.Capabilities {
		if k.Name == "oidc" {
			oidc = k
		}
	}
	if _, ok := oidc.Fields["admin_email"]; ok {
		t.Error("oidc still has the field admin_email")
	}
	if f, ok := oidc.Fields["logout_path"]; !ok || f.Direction != contract.ToProvider {
		t.Errorf("oidc logout_path = %+v, want a to_provider field", f)
	}
	re := regexp.MustCompile(`^(?:` + oidc.Fields["client_id"].Pattern + `)$`)
	for id, ok := range map[string]bool{"api_oidc": true, "my-app": true, "Api": false, "_x": false, "a b": false} {
		if re.MatchString(id) != ok {
			t.Errorf("client_id pattern on %q: got %v, want %v", id, !ok, ok)
		}
	}
	data, err := os.ReadFile("../../catalog/modules/api/module.yaml")
	if err != nil {
		t.Fatal(err)
	}
	caps := map[string]contract.Capability{}
	for _, k := range catalog.Capabilities {
		caps[k.Name] = k
	}
	without := strings.Replace(string(data), "      logout_path: {value: /}\n", "", 1)
	if without == string(data) {
		t.Fatal("the api entry has no logout_path to remove")
	}
	_, problems := contract.CheckModule("modules/api/module.yaml", []byte(without), caps)
	if !slices.ContainsFunc(problems, func(p contract.CatalogProblem) bool {
		return p.Code == contract.CodeMissingFieldSource && p.Path == "uses.oidc.fields.logout_path"
	}) {
		t.Errorf("an api entry without logout_path: problems %v", problems)
	}
}
