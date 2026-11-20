package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"

	"github.com/racherom/modular-platform/libs/render"
)

// TestRenderGateGolden pins the platform render of a gated route (spec 0037, AC-2, AC-9): the
// identity module's first admin paths pass through a forward_auth call to the route that carries
// a guard, and a missing answer is a 403.
func TestRenderGateGolden(t *testing.T) {
	t.Parallel()
	catalog, templates := loadCatalogTemplates(t, "../../catalog", "testdata/gate/catalog")
	result := checkFile(t, filepath.Join("testdata", "gate", "graph.yaml"), catalog)
	out, err := render.Platform(result, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "gate", "golden"), out)
	for _, f := range out.Files {
		if f.Path != "config/ingress/Caddyfile" {
			continue
		}
		for _, want := range []string{
			"@gate-idp path /setup /signup/setup /api/signup/setup",
			"forward_auth http://guardapp",
			"/api/v1/setup/gate",
			"respond @gate-down-idp 403",
		} {
			if !strings.Contains(string(f.Data), want) {
				t.Errorf("Caddyfile lacks %q", want)
			}
		}
	}
}

// TestGateNeedsGuard checks that a gated route with no route that answers the gate question is
// an error at the binding's gate path, never a gate that lets requests through (spec 0037).
func TestGateNeedsGuard(t *testing.T) {
	t.Parallel()
	catalog, _ := loadCatalogTemplates(t, "../../catalog", "testdata/gate/catalog")
	data, err := os.ReadFile(filepath.Join("testdata", "gate", "graph.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "  guardapp:\n    module: guardapp\n    uses:\n      public: ingress\n", "", 1)
	g, p := contract.DecodeGraph([]byte(text))
	if p != nil {
		t.Fatal(p.Message)
	}
	result := check.Graph(g, catalog)
	found := false
	for _, pr := range result.Problems {
		found = found || (pr.Code == "invalid_binding" && pr.Path == "modules.idp.uses.public.gate")
	}
	if result.Ok || !found {
		t.Errorf("problems = %+v, want invalid_binding at modules.idp.uses.public.gate", result.Problems)
	}
}
