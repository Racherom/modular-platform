package render_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/check"
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
	for i, c := range catalog.Capabilities {
		if c.Yields == nil {
			continue
		}
		if name, ok := strings.CutPrefix(c.Yields.Job.Image, platformImagePrefix); ok {
			catalog.Capabilities[i].Yields.Job.Image = platformImagePrefix + name[:strings.LastIndex(name, ":")] + ":" + testTag
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
	for _, path := range []string{"docker-compose.yml", "graph.yaml", "setup.sh", ".env.example", "README.md", "config/ingress/Caddyfile", "config/postgres/postgresql.conf", "config/postgres/pg_hba.conf", "config/api.env"} {
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
		if svc.Labels[render.LabelStack] != render.PlatformStack || svc.Labels[render.LabelGraph] != "modular-platform" {
			t.Errorf("%s labels = %v", name, svc.Labels)
		}
		if svc.Restart != "unless-stopped" && !strings.Contains(name, "_provision_") && !strings.Contains(name, "_revoke_") {
			t.Errorf("%s restart = %q", name, svc.Restart)
		}
	}
	stacks := "${MP_DIR}/stacks"
	if !slices.ContainsFunc(compose.Services["deployer-docker"].Volumes, func(v struct {
		Source string `json:"source"`
		Target string `json:"target"`
	},
	) bool {
		return v.Source == stacks && v.Target == stacks
	}) {
		t.Errorf("deployer-docker volumes = %+v, want %s at the same path", compose.Services["deployer-docker"].Volumes, stacks)
	}

	// spec 0015's questions, in order: domain and origin; the identity provider asks nothing (spec 0037)
	setup := files["setup.sh"]
	order := []string{"ask MP_INGRESS__DOMAIN ", "ask MP_INGRESS__ORIGIN "}
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
		"reverse_proxy {$MP_WEB_UPSTREAM:http://web.modular-platform.internal:8080}\n",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Errorf("Caddyfile lacks %q:\n%s", want, caddyfile)
		}
	}
	// the site root comes last, so a longer route is never shadowed by it
	if strings.Index(caddyfile, "@web ") < strings.Index(caddyfile, "@api ") {
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
	if _, err := render.Managed(result, catalog, templates, testStack, nil); !errors.As(err, &refused) {
		t.Fatalf("managed render: %v, want a RefusedError", err)
	}
	var got []string
	for _, p := range refused.Problems {
		got = append(got, p.Code+" "+p.Path)
	}
	for _, want := range []string{"host_access modules.deployer-docker", "host_access modules.ingress", "setup_required modules.cert-issuer"} {
		if !slices.Contains(got, want) {
			t.Errorf("managed refusals %q lack %q", got, want)
		}
	}
}

// pocketGraphWithSignup is deploy/managed/graph.yaml with the config signup set on pocket-id.
func pocketGraphWithSignup(t *testing.T, value string) string {
	t.Helper()
	data, err := os.ReadFile("../../deploy/managed/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "  pocket-id:\n    module: pocket-id\n"
	text := string(data)
	if !strings.Contains(text, anchor) {
		t.Fatal("deploy/managed/graph.yaml has no pocket-id instance in the expected shape")
	}
	return strings.Replace(text, anchor, anchor+"    config:\n      signup: "+value+"\n", 1)
}

// TestPlatformSignup checks the sign up mode of the bundled provider (spec 0037, AC-6): the
// default is disabled, each listed value only changes the provider's setting, and any other
// value is refused at modules.pocket-id.config.signup with invalid_config.
func TestPlatformSignup(t *testing.T) {
	t.Parallel()
	_, catalog, templates := platformResult(t)
	const setting = "ALLOW_USER_SIGNUPS: ${MP_POCKET_ID__SIGNUP:-disabled}"

	base, err := render.Platform(checkFile(t, "../../deploy/managed/graph.yaml", catalog), catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	baseFiles := map[string]string{}
	for _, f := range base.Files {
		baseFiles[f.Path] = string(f.Data)
	}
	if !strings.Contains(baseFiles["docker-compose.yml"], setting) {
		t.Errorf("the compose file does not map %s", setting)
	}
	if !strings.Contains(baseFiles[".env.example"], "# MP_POCKET_ID__SIGNUP=disabled") {
		t.Error(".env.example does not list MP_POCKET_ID__SIGNUP with the default disabled")
	}

	for _, value := range []string{"disabled", "withToken", "open"} {
		out, err := render.Platform(resultText(t, pocketGraphWithSignup(t, value), catalog), catalog, templates)
		if err != nil {
			t.Fatalf("signup %s: %v", value, err)
		}
		for _, f := range out.Files {
			want, ok := baseFiles[f.Path]
			if !ok {
				t.Errorf("signup %s adds the file %s", value, f.Path)
				continue
			}
			// the graph text itself and the .env.example default are the only files that may differ
			if f.Path == "graph.yaml" || f.Path == ".env.example" {
				continue
			}
			// the graph's value becomes the default of the setting, and nothing else moves
			got := strings.Replace(string(f.Data), "${MP_POCKET_ID__SIGNUP:-"+value+"}", "${MP_POCKET_ID__SIGNUP:-disabled}", 1)
			if got != want {
				t.Errorf("signup %s changes %s beyond the sign up setting", value, f.Path)
			}
		}
	}

	g, p := contract.DecodeGraph([]byte(pocketGraphWithSignup(t, "bogus")))
	if p != nil {
		t.Fatal(p.Message)
	}
	result := check.Graph(g, catalog)
	if result.Ok {
		t.Fatal("signup: bogus passed the check")
	}
	found := slices.ContainsFunc(result.Problems, func(p contract.Problem) bool {
		return p.Path == "modules.pocket-id.config.signup" && p.Code == check.CodeInvalidConfig
	})
	if !found {
		t.Errorf("no invalid_config problem at modules.pocket-id.config.signup: %+v", result.Problems)
	}
}

// TestPlatformREADMEIdentity checks that the README of the platform folder says what spec 0037
// AC-10 asks, and that nothing in it still describes the old password sign in.
func TestPlatformREADMEIdentity(t *testing.T) {
	t.Parallel()
	result, catalog, templates := platformResult(t)
	out, err := render.Platform(result, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	var readme string
	for _, f := range out.Files {
		if f.Path == "README.md" {
			readme = string(f.Data)
		}
	}
	for name, want := range map[string]string{
		"passkeys":           "signs people in with passkeys only",
		"the auth host DNS":  "Give it its own DNS name",
		"the api key file":   "secrets/pocket-id/static-api-key",
		"why the key stays":  "on every `docker compose up`",
		"lost passkey":       "one-time-access-token",
		"sign up setting":    "MP_POCKET_ID__SIGNUP",
		"lost setup code":    "Delete the files in `secrets/api/setup/`",
		"the client job log": "docker compose logs pocket-id_provision_api_oidc",
		"the marker":         "carries a marker",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README has no text for %s (%q)", name, want)
		}
	}
	for _, gone := range []string{"Authelia", "ADMIN_EMAIL", "admin password", "admin user name"} {
		if strings.Contains(readme, gone) {
			t.Errorf("README still mentions %q", gone)
		}
	}
}
