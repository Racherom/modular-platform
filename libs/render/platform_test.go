package render_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// platformImagePrefix starts the images of the platform's own services, which release-please
// bumps; the platform golden pins them at testTag instead, so a release never changes it.
const (
	platformImagePrefix = "ghcr.io/racherom/modular-platform/"
	testTag             = "0.0.0-test"
)

// platformResult checks deploy/managed/graph.yaml with the built in catalog, the platform's own
// images tagged testTag (spec 0015, "Platform render flavor").
func platformResult(t *testing.T) (contract.CheckResult, contract.Catalog, contract.Templates) {
	t.Helper()
	catalog, templates := loadCatalogTemplates(t, "../../catalog")
	for i, m := range catalog.Modules {
		if name, ok := strings.CutPrefix(m.Image, platformImagePrefix); ok {
			catalog.Modules[i].Image = platformImagePrefix + name[:strings.LastIndex(name, ":")] + ":" + testTag
		}
	}
	return checkFile(t, "../../deploy/managed/graph.yaml", catalog), catalog, templates
}

// TestRenderPlatformGolden pins every file of the rendered platform folder, one golden per
// rendered template (spec 0015, AC-16, AC-9). Run with -update after an intended change.
func TestRenderPlatformGolden(t *testing.T) {
	t.Parallel()
	result, catalog, templates := platformResult(t)
	out, err := render.Platform(result, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "platform"), out)

	again, err := render.Platform(result, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := out.WriteZip(&a); err != nil {
		t.Fatal(err)
	}
	if err := again.WriteZip(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two renders of the platform graph differ")
	}
}

// TestRenderPlatformFolder checks what spec 0015 asks of the folder beyond its bytes: its files,
// the platform labels and restart policy on every service, ${MP_DIR} paths, the question order
// of setup.sh, and the Caddyfile's routes (AC-16, AC-30).
func TestRenderPlatformFolder(t *testing.T) {
	t.Parallel()
	result, catalog, templates := platformResult(t)
	out, err := render.Platform(result, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	for _, path := range []string{"docker-compose.yml", "graph.yaml", "setup.sh", ".env.example", "README.md", "config/ingress/Caddyfile", "config/authelia/configuration.yml", "config/postgres/postgresql.conf", "config/postgres/pg_hba.conf", "config/api.env"} {
		if _, ok := files[path]; !ok {
			t.Errorf("the folder has no %s", path)
		}
	}
	if _, ok := files[".env"]; ok {
		t.Error("the folder holds .env, which setup.sh writes; an unzip must never replace it")
	}

	var compose struct {
		Name     string `json:"name"`
		Services map[string]struct {
			Restart string            `json:"restart"`
			Labels  map[string]string `json:"labels"`
			Volumes []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"volumes"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal([]byte(files["docker-compose.yml"]), &compose); err != nil {
		t.Fatal(err)
	}
	if compose.Name != "modular-platform" {
		t.Errorf("project name = %q", compose.Name)
	}
	for name, svc := range compose.Services {
		if svc.Labels[render.LabelDeployment] != render.PlatformDeployment || svc.Labels[render.LabelGraph] != "modular-platform" {
			t.Errorf("%s labels = %v", name, svc.Labels)
		}
		if svc.Restart != "unless-stopped" && !strings.Contains(name, "_provision_") && !strings.Contains(name, "_revoke_") {
			t.Errorf("%s restart = %q", name, svc.Restart)
		}
	}
	deployments := "${MP_DIR}/deployments"
	if !slices.ContainsFunc(compose.Services["deployer-docker"].Volumes, func(v struct {
		Source string `json:"source"`
		Target string `json:"target"`
	},
	) bool {
		return v.Source == deployments && v.Target == deployments
	}) {
		t.Errorf("deployer-docker volumes = %+v, want %s at the same path", compose.Services["deployer-docker"].Volumes, deployments)
	}

	// spec 0015's questions, in order: domain, origin, admin email, then the users step's two
	setup := files["setup.sh"]
	order := []string{"ask MP_INGRESS__DOMAIN ", "ask MP_INGRESS__ORIGIN ", "ask MP_AUTHELIA__ADMIN_EMAIL ", "step_ask 'Admin user name", "step_ask 'Admin password"}
	last := -1
	for _, q := range order {
		at := strings.Index(setup, q)
		if at < 0 || at < last {
			t.Errorf("setup.sh asks %q at %d, after %d; want the order %q", q, at, last, order)
		}
		last = at
	}
	if n := regexp.MustCompile(`(?m)^\s*(ask|ask_secret|step_ask) `).FindAllStringIndex(setup, -1); len(n) != len(order) {
		t.Errorf("setup.sh asks %d questions, want %d", len(n), len(order))
	}
	if !strings.Contains(setup, `"https://$(env_get MP_INGRESS__DOMAIN)"`) {
		t.Error("the origin's default is not the https URL of the domain answer")
	}
	if !strings.Contains(setup, `set_id MP_DIR "'$(pwd)'"`) {
		t.Error("setup.sh does not write MP_DIR")
	}

	caddyfile := files["config/ingress/Caddyfile"]
	for _, want := range []string{
		"\timport tls-{$MP_TLS_MODE}\n",
		"@api path {$INGRESS_BASE_PATH}/api {$INGRESS_BASE_PATH}/api/*\n",
		"reverse_proxy https://api.modular-platform.internal:8443 {\n\t\t\t# the event stream reaches the browser as it is written\n\t\t\tflush_interval -1\n",
		"tls_server_name api.modular-platform.internal\n",
		"@authelia path {$INGRESS_BASE_PATH}/auth {$INGRESS_BASE_PATH}/auth/*\n",
		"reverse_proxy {$MP_WEB_UPSTREAM:http://web.modular-platform.internal:8080}\n",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Errorf("Caddyfile lacks %q:\n%s", want, caddyfile)
		}
	}
	// the site root comes last, so a longer route is never shadowed by it
	if strings.Index(caddyfile, "@web ") < strings.Index(caddyfile, "@authelia ") {
		t.Error("the site root route comes before another route")
	}
	for _, m := range catalog.Modules {
		if _, declared := m.Config["web-upstream"]; declared {
			t.Errorf("module %s declares web-upstream; MP_WEB_UPSTREAM is a dev hook only the overlay sets (AC-30)", m.Name)
		}
	}
}

// TestPlatformOnlyFeatures checks that the other flavors do not render the platform graph: the
// static zip has no install folder for {install} paths, and the managed flavor refuses the host
// access and the setup steps (spec 0013).
func TestPlatformOnlyFeatures(t *testing.T) {
	t.Parallel()
	result, catalog, templates := platformResult(t)
	if _, err := render.Render(result, catalog, templates); !errors.Is(err, render.ErrNotRendered) {
		t.Errorf("static render: %v, want ErrNotRendered", err)
	}
	var refused *render.RefusedError
	if _, err := render.Managed(result, catalog, templates, testDeployment); !errors.As(err, &refused) {
		t.Fatalf("managed render: %v, want a RefusedError", err)
	}
	var got []string
	for _, p := range refused.Problems {
		got = append(got, p.Code+" "+p.Path)
	}
	for _, want := range []string{"host_access modules.deployer-docker", "host_access modules.ingress", "setup_required modules.authelia", "setup_required modules.cert-issuer"} {
		if !slices.Contains(got, want) {
			t.Errorf("managed refusals %q lack %q", got, want)
		}
	}
}
