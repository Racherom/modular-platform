package render_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/render"
)

// setupCompose is the part of a platform Compose file the setup files tests read.
type setupCompose struct {
	Secrets  map[string]any `json:"secrets"`
	Services map[string]struct {
		Environment map[string]string `json:"environment"`
		Secrets     []struct {
			Source string `json:"source"`
		} `json:"secrets"`
		Volumes []struct {
			Type     string `json:"type"`
			Source   string `json:"source"`
			Target   string `json:"target"`
			ReadOnly bool   `json:"read_only"`
		} `json:"volumes"`
	} `json:"services"`
}

func parseSetupCompose(t *testing.T, files map[string]string) setupCompose {
	t.Helper()
	var c setupCompose
	if err := yaml.Unmarshal([]byte(files["docker-compose.yml"]), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// checkSetupMount asserts spec 0037 AC-14 on a platform render: api gets no Compose secret for its
// setup code and key, one read write bind of its setup folder, and MP_SETUP_DIR.
func checkSetupMount(t *testing.T, files map[string]string) {
	t.Helper()
	c := parseSetupCompose(t, files)
	for name := range c.Secrets {
		if name == "api_code" || name == "api_session-key" {
			t.Errorf("the render has a Compose secret %s for a setup file", name)
		}
	}
	api := c.Services["api"]
	for _, s := range api.Secrets {
		if s.Source == "api_code" || s.Source == "api_session-key" {
			t.Errorf("api mounts the setup file %s as a secret", s.Source)
		}
	}
	if api.Environment["MP_SETUP_DIR"] != "/run/secrets/setup" {
		t.Errorf("MP_SETUP_DIR = %q, want /run/secrets/setup", api.Environment["MP_SETUP_DIR"])
	}
	binds := 0
	for _, v := range api.Volumes {
		if v.Target != "/run/secrets/setup" {
			continue
		}
		binds++
		if v.Type != "bind" || v.Source != "./secrets/api/setup" || v.ReadOnly {
			t.Errorf("setup mount = %+v, want a read write bind of ./secrets/api/setup", v)
		}
	}
	if binds != 1 {
		t.Errorf("api has %d setup mounts, want 1", binds)
	}
	// only the setup folder is writable: no other mount of api is a read write bind of secrets/
	for _, v := range api.Volumes {
		if v.Target != "/run/secrets/setup" && !v.ReadOnly && strings.HasPrefix(v.Source, "./secrets/") {
			t.Errorf("api mounts %s read write", v.Source)
		}
	}
}

// TestSetupFilesPlatformRender covers AC-14 in the render with setup.sh: the files are no Compose
// secret, setup.sh makes them in their own stage and no value is rendered. A graph with
// authelia, which has no first admin page, shows none of the pocket-id additions.
func TestSetupFilesPlatformRender(t *testing.T) {
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
	checkSetupMount(t, files)
	script := files["setup.sh"]
	for _, want := range []string{
		"new_setup_file password secrets/api/setup/code",
		"new_setup_file hex secrets/api/setup/session-key",
		"secrets/api/setup/.setup-done",
		"rm -f secrets/api/setup/code-used",
		"set -C",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("setup.sh lacks %q", want)
		}
	}
	if strings.Contains(script, "new_secret secrets/api/setup") {
		t.Error("setup.sh makes a setup file with new_secret, which would print nothing and keep no used code")
	}
	// a render with authelia has no first admin page, no handoff route and no gate
	autheliaFiles := autheliaPlatformFiles(t)
	if strings.Contains(autheliaFiles["docker-compose.yml"], "MP_OIDC_FIRST_ADMIN_PATH") {
		t.Error("an authelia render gives api a first admin path")
	}
	for _, marker := range []string{"@enter-", "@gate-"} {
		if strings.Contains(autheliaFiles["config/ingress/Caddyfile"], marker) {
			t.Errorf("an authelia render has %s in the Caddyfile", marker)
		}
	}
}

// autheliaPlatformFiles renders the managed graph with authelia in place of pocket-id, the
// optional identity provider that stays in the catalog (spec 0037, task 7c).
func autheliaPlatformFiles(t *testing.T) map[string]string {
	t.Helper()
	_, catalog, templates := platformResult(t)
	data, err := os.ReadFile("../../deploy/managed/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	text = strings.Replace(text, "      oidc:\n        client_id: api_oidc\n        provider: pocket-id.oidc\n", "      oidc: authelia.oidc\n", 1)
	text = strings.Replace(text, "  pocket-id:\n    module: pocket-id\n    uses:\n      db:\n        database: pocket_id\n        provider: postgres\n        user: pocket_id\n      public: ingress\n", "  authelia:\n    module: authelia\n    uses:\n      db:\n        database: authelia\n        provider: postgres\n        user: authelia\n      public: ingress\n", 1)
	if strings.Contains(text, "pocket-id") {
		t.Fatal("deploy/managed/graph.yaml changed shape; update autheliaPlatformFiles")
	}
	out, err := render.Platform(resultText(t, text, catalog), catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	return files
}

// TestSetupFilesKeeperRender covers AC-14 and AC-16 in the keeper render: the same mount, the
// keeper's setup.json makes the files, and the pocket-id graph gives api its first admin path and
// the auth host the exact handoff route.
func TestSetupFilesKeeperRender(t *testing.T) {
	t.Parallel()
	files := keeperFiles(t)
	checkSetupMount(t, files)
	c := parseSetupCompose(t, files)
	if got := c.Services["api"].Environment["MP_OIDC_FIRST_ADMIN_PATH"]; got != "/setup" {
		t.Errorf("MP_OIDC_FIRST_ADMIN_PATH = %q, want /setup", got)
	}
	setup, err := render.DecodeSetup([]byte(files["setup.json"]))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, g := range setup.Generate {
		if g.Path == "secrets/api/setup/code" || g.Path == "secrets/api/setup/session-key" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("setup.json generates %d of the two setup files", found)
	}
	caddy := files["config/ingress/Caddyfile"]
	for _, want := range []string{
		"@enter-pocket-id {\n\t\tmethod GET\n\t\tpath {$INGRESS_BASE_PATH}/api/v1/setup/identity-enter\n\t}",
		"@gate-pocket-id path /setup* /signup/setup* /api/signup/setup*",
	} {
		if !strings.Contains(caddy, want) {
			t.Errorf("Caddyfile lacks %q", want)
		}
	}
	// the exact handoff route comes before the gate, which comes before the provider's catch all
	enter, gate := strings.Index(caddy, "handle @enter-pocket-id"), strings.Index(caddy, "handle @gate-pocket-id")
	if enter < 0 || gate < 0 || enter > gate {
		t.Errorf("the handoff route (%d) must come before the gate (%d)", enter, gate)
	}
}

// TestSetupFilesManagedRefusal pins that a deployed stack, which has no setup.sh, refuses an instance
// with setup files instead of rendering a mount nothing fills.
func TestSetupFilesManagedRefusal(t *testing.T) {
	t.Parallel()
	result, catalog, templates := platformResult(t)
	_, err := render.Managed(result, catalog, templates, testStack, nil)
	var refused *render.RefusedError
	if err == nil || !errors.As(err, &refused) {
		t.Fatalf("Managed = %v, want a refusal", err)
	}
	found := false
	for _, p := range refused.Problems {
		found = found || (p.Code == render.CodeSetupRequired && p.Path == "modules.api")
	}
	if !found {
		t.Errorf("problems = %+v, want setup_required at modules.api", refused.Problems)
	}
}
