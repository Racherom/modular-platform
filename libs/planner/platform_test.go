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

// TestPaperCaddyfile executes the paper caddy entry's Caddyfile template with the platform's
// clients of ingress.public: the certificate source comes from MP_TLS_MODE through the two
// snippets, and / goes to the web binding unless MP_WEB_UPSTREAM is set, which no entry
// declares (spec 0025, AC-30).
func TestPaperCaddyfile(t *testing.T) {
	t.Parallel()
	fsys := os.DirFS("../contract/testdata/platform")
	merged := fstest.MapFS{}
	for _, path := range []string{"modules/caddy/module.yaml", "modules/caddy/Caddyfile.tmpl", "capabilities/http-ingress.yaml", "capabilities/tls-certificate.yaml"} {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			t.Fatal(err)
		}
		merged[path] = &fstest.MapFile{Data: data}
	}
	acme, err := os.ReadFile("../../catalog/capabilities/acme.yaml")
	if err != nil {
		t.Fatal(err)
	}
	merged["capabilities/acme.yaml"] = &fstest.MapFile{Data: acme}
	catalog, problems := contract.LoadCatalog(merged)
	if len(problems) > 0 {
		t.Fatalf("catalog: %v", problems)
	}
	templates, err := contract.LoadTemplates(merged, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range catalog.Modules {
		if _, declared := m.Config["web-upstream"]; declared {
			t.Error("the caddy entry declares web-upstream; MP_WEB_UPSTREAM is a dev hook only the overlay sets")
		}
	}
	host := func(i string) string { return i + ".modular-platform.internal" }
	out, err := templates.Execute("caddy", "Caddyfile.tmpl", contract.TemplateData{
		Graph: "modular-platform", Instance: "ingress", Host: host("ingress"),
		Provides: map[string]contract.TemplateProvide{"public": {Port: 443}},
		Uses:     map[string]contract.TemplateSlot{"trust": {Bound: true, Provider: "cert-issuer", Provide: "acme", Host: host("cert-issuer"), Values: map[string]string{}}},
		Clients: map[string][]contract.TemplateClient{"public": {
			{Instance: "api", Slot: "public", Host: host("api"), Values: map[string]string{"route": "/api", "stream": "true"}},
			{Instance: "web", Slot: "public", Host: host("web"), Values: map[string]string{"route": "/", "upstream": "http://web.modular-platform.internal:8080"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"(tls-acme) {\n", "(tls-internal) {\n\ttls internal\n}\n", "\timport tls-{$MP_TLS_MODE}\n",
		"\t\treverse_proxy {$MP_WEB_UPSTREAM:http://web.modular-platform.internal:8080}\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("Caddyfile lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(string(out), "reverse_proxy") != 1 {
		t.Errorf("want one site root route:\n%s", out)
	}
}
