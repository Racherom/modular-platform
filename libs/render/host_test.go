package render_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestRenderHostGolden pins the platform render of a consumer served at a host of its own (spec
// 0026, AC-5, AC-6): the Caddy site, the ingress alias, the consumer's environment and the Compose
// variable of the domain, once with the entry's default host and once with the graph's.
func TestRenderHostGolden(t *testing.T) {
	t.Parallel()
	catalog, templates := loadCatalogTemplates(t, "../../catalog", "testdata/host/catalog")
	for _, c := range []struct{ graph, golden, host string }{
		{"graph.yaml", "golden", "auth.${MP_INGRESS__DOMAIN"},
		{"graph-login.yaml", "golden-login", "login.example.org"},
	} {
		t.Run(c.golden, func(t *testing.T) {
			t.Parallel()
			result := checkFile(t, filepath.Join("testdata", "host", c.graph), catalog)
			out, err := render.Platform(result, catalog, templates)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, filepath.Join("testdata", "host", c.golden), out)
			text := map[string]string{}
			for _, f := range out.Files {
				text[f.Path] = string(f.Data)
			}
			for path, want := range map[string]string{
				"docker-compose.yml":       c.host,
				"config/ingress/Caddyfile": "# hostapp is served at the root of its own host",
			} {
				if !strings.Contains(text[path], want) {
					t.Errorf("%s lacks %q", path, want)
				}
			}
		})
	}
}

// TestHostRefusedOutsidePlatform checks that the static and managed flavors refuse a host the
// graph sets (spec 0026, AC-10), at the binding's path.
func TestHostRefusedOutsidePlatform(t *testing.T) {
	t.Parallel()
	catalog, templates := loadCatalogTemplates(t, "../../catalog", "testdata/host/catalog")
	result := checkFile(t, filepath.Join("testdata", "host", "graph-login.yaml"), catalog)

	_, err := render.Render(result, catalog, templates)
	var refused *render.RefusedError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 ||
		refused.Problems[0].Code != "invalid_binding" || refused.Problems[0].Path != "modules.hostapp.uses.public.host" {
		t.Errorf("static render error = %v, want invalid_binding at modules.hostapp.uses.public.host", err)
	}
	_, err = render.Managed(result, catalog, templates, "00000000-0000-0000-0000-000000000000", nil)
	if !errors.As(err, &refused) {
		t.Errorf("managed render error = %v, want a refusal", err)
	}
}
