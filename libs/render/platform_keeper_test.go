package render_test

import (
	"bytes"
	"errors"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// keeperDir holds the keeper render's goldens, the graph and, under catalog/, the pocket-id and
// deployer-docker entries, which replace the real authelia and deployer-docker ones in
// keeperResult (spec 0016, "Task 3 golden"; spec 0037, task 2). The cut over moves the entries
// into catalog/modules/ and removes this folder.
const keeperDir = "testdata/platform-keeper"

// keeperResult checks the fixture graph with the built in catalog and the two fixture entries, the platform's own images tagged testTag as platformResult does.
func keeperResult(t *testing.T) (contract.CheckResult, contract.Catalog, contract.Templates) {
	t.Helper()
	catalog, templates := loadCatalogTemplates(t, "../../catalog", filepath.Join(keeperDir, "catalog"))
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
	return checkFile(t, filepath.Join(keeperDir, "graph.yaml"), catalog), catalog, templates
}

// keeperFiles renders the keeper output of the platform graph, path to text.
func keeperFiles(t *testing.T) map[string]string {
	t.Helper()
	result, catalog, templates := keeperResult(t)
	out, err := render.Platform(result, catalog, templates, render.WithKeeper())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	return files
}

// TestRenderPlatformKeeperGolden pins every file of the keeper render (spec 0016, AC-1, AC-10,
// AC-11). Run with -update after an intended change.
func TestRenderPlatformKeeperGolden(t *testing.T) {
	t.Parallel()
	result, catalog, templates := keeperResult(t)
	out, err := render.Platform(result, catalog, templates, render.WithKeeper())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if strings.HasPrefix(f.Path, "catalog/") {
			t.Fatalf("the render writes %s, which would clash with the fixtures beside the goldens", f.Path)
		}
	}
	checkGolden(t, keeperDir, out)

	again, err := render.Platform(result, catalog, templates, render.WithKeeper())
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
		t.Error("two keeper renders of the platform graph differ")
	}
}

// composeFile is the part of a rendered Compose file the keeper tests read.
type composeFile struct {
	Name     string `json:"name"`
	Services map[string]struct {
		Image       string                    `json:"image"`
		Restart     string                    `json:"restart"`
		Labels      map[string]string         `json:"labels"`
		DependsOn   map[string]any            `json:"depends_on"`
		Environment map[string]string         `json:"environment"`
		Networks    map[string]map[string]any `json:"networks"`
		HealthCheck struct {
			StartPeriod string `json:"start_period"`
		} `json:"healthcheck"`
		Secrets []struct {
			Source string `json:"source"`
		} `json:"secrets"`
		Configs []struct {
			Source string `json:"source"`
		} `json:"configs"`
		Volumes []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			ReadOnly bool   `json:"read_only"`
		} `json:"volumes"`
	} `json:"services"`
	Networks map[string]struct {
		Name     string `json:"name"`
		External bool   `json:"external"`
	} `json:"networks"`
	Secrets map[string]struct {
		File string `json:"file"`
	} `json:"secrets"`
	Configs map[string]struct {
		File string `json:"file"`
	} `json:"configs"`
}

func parseCompose(t *testing.T, text string) composeFile {
	t.Helper()
	var c composeFile
	if err := yaml.Unmarshal([]byte(text), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestPlatformKeeperSplit checks the two Compose files (spec 0016, "Render changes"): the
// platform project without the keeper and with no dependency on it; keeper.yml with only the
// keeper, no project name, the platform's internal network as an external one, the image
// variable, the 5 minute start period, the state folder at the same path, and MP_STATE_DIR.
func TestPlatformKeeperSplit(t *testing.T) {
	t.Parallel()
	files := keeperFiles(t)
	platform := parseCompose(t, files["docker-compose.yml"])
	if platform.Name != "modular-platform" {
		t.Errorf("platform project name = %q", platform.Name)
	}
	if _, ok := platform.Services["deployer-docker"]; ok {
		t.Error("docker-compose.yml still holds the keeper")
	}
	for name, svc := range platform.Services {
		if _, ok := svc.DependsOn["deployer-docker"]; ok {
			t.Errorf("%s depends on the keeper", name)
		}
		for _, s := range svc.Secrets {
			if _, ok := platform.Secrets[s.Source]; !ok {
				t.Errorf("%s mounts the undeclared secret %s", name, s.Source)
			}
		}
	}
	if _, ok := platform.Secrets["deployer-docker_uses_acme_ca"]; ok {
		t.Error("docker-compose.yml declares a secret only the keeper mounts")
	}

	if strings.Contains(files["keeper.yml"], "\nname:") || strings.HasPrefix(files["keeper.yml"], "name:") {
		t.Error("keeper.yml names its project; the keeper names it per definition")
	}
	keeper := parseCompose(t, files["keeper.yml"])
	if len(keeper.Services) != 1 {
		t.Fatalf("keeper.yml services = %d, want only the keeper", len(keeper.Services))
	}
	svc, ok := keeper.Services["deployer-docker"]
	if !ok {
		t.Fatal("keeper.yml lacks the keeper deployer-docker")
	}
	want := "${MP_KEEPER_IMAGE:-" + platformImagePrefix + "deployer-docker:" + testTag + "}"
	if svc.Image != want {
		t.Errorf("keeper image = %q, want %q", svc.Image, want)
	}
	if svc.Restart != "unless-stopped" || svc.HealthCheck.StartPeriod != "5m0s" || len(svc.DependsOn) > 0 {
		t.Errorf("keeper restart %q, start_period %q, depends_on %v", svc.Restart, svc.HealthCheck.StartPeriod, svc.DependsOn)
	}
	for label, v := range map[string]string{render.LabelStack: render.PlatformStack, render.LabelInstance: "deployer-docker", render.LabelModule: "deployer-docker"} {
		if svc.Labels[label] != v {
			t.Errorf("keeper label %s = %q, want %q", label, svc.Labels[label], v)
		}
	}
	if n := keeper.Networks["internal"]; !n.External || n.Name != "modular-platform_internal" || len(keeper.Networks) != 1 {
		t.Errorf("keeper networks = %+v, want only internal as external modular-platform_internal", keeper.Networks)
	}
	if _, ok := svc.Networks["internal"]["aliases"]; !ok {
		t.Error("the keeper lost its alias on internal")
	}
	if svc.Environment["MP_STATE_DIR"] != "${MP_DIR}" {
		t.Errorf("MP_STATE_DIR = %q, want ${MP_DIR}", svc.Environment["MP_STATE_DIR"])
	}
	if !slices.ContainsFunc(svc.Volumes, func(v struct {
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"read_only"`
	},
	) bool {
		return v.Source == "${MP_DIR}" && v.Target == "${MP_DIR}" && !v.ReadOnly
	}) {
		t.Errorf("keeper volumes = %+v, want the state folder at the same path, writable", svc.Volumes)
	}
}

// TestPlatformKeeperMountsExist checks that every file keeper.yml mounts is made before the
// keeper starts (spec 0016, "Render changes"): a generate path, a step's write or a folder in
// setup.json, or a file the render ships.
func TestPlatformKeeperMountsExist(t *testing.T) {
	t.Parallel()
	files := keeperFiles(t)
	setup, err := render.DecodeSetup([]byte(files["setup.json"]))
	if err != nil {
		t.Fatal(err)
	}
	made := map[string]bool{"": true} // the state folder itself
	for _, g := range setup.Generate {
		made[g.Path] = true
	}
	for _, s := range setup.Steps {
		for _, w := range s.Writes {
			made["secrets/"+s.Instance+"/"+strings.TrimSuffix(w.Name, "/")] = true
		}
	}
	for _, f := range setup.Folders {
		made[f.Path] = true
	}
	for path := range files {
		made[path] = true
	}
	keeper := parseCompose(t, files["keeper.yml"])
	var mounts []string
	for _, s := range keeper.Secrets {
		mounts = append(mounts, strings.TrimPrefix(s.File, "./"))
	}
	for _, c := range keeper.Configs {
		mounts = append(mounts, strings.TrimPrefix(c.File, "./"))
	}
	for _, v := range keeper.Services["deployer-docker"].Volumes {
		if rel, ok := strings.CutPrefix(v.Source, "${MP_DIR}"); ok {
			mounts = append(mounts, strings.TrimPrefix(rel, "/"))
		}
	}
	if len(mounts) < 4 {
		t.Fatalf("keeper.yml mounts %q; the test reads no mount", mounts)
	}
	for _, m := range mounts {
		if !made[m] {
			t.Errorf("keeper.yml mounts %s, which neither setup.json nor the render makes", m)
		}
	}
}

// TestPlatformKeeperSetup checks setup.json (spec 0016, AC-3, AC-10; spec 0037, AC-7): the
// generated pocket-id secrets, no shown secret and no asked step, the folders and the keeper;
// and that it reads back with DecodeSetup.
func TestPlatformKeeperSetup(t *testing.T) {
	t.Parallel()
	files := keeperFiles(t)
	setup, err := render.DecodeSetup([]byte(files["setup.json"]))
	if err != nil {
		t.Fatal(err)
	}
	again, err := render.MarshalSetup(setup)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != files["setup.json"] {
		t.Error("setup.json does not read back to the same bytes")
	}

	for _, name := range []string{"encryption-key", "static-api-key"} {
		path := "secrets/pocket-id/" + name
		at := slices.IndexFunc(setup.Generate, func(g render.GenerateFile) bool { return g.Path == path })
		if at < 0 {
			t.Errorf("setup.json does not generate %s", path)
			continue
		}
		if g := setup.Generate[at]; g.Show || len(g.Readers) > 0 {
			t.Errorf("%s = %+v, want a plain generated secret", path, g)
		}
	}
	// module secrets, then per binding files, then empty job outputs
	tier := 0
	for _, g := range setup.Generate {
		t2 := map[bool]int{false: 0, true: 1}[strings.Contains(g.Path, "/uses_") || strings.Contains(g.Path, "/clients/")]
		if g.Format == render.GenerateEmpty {
			t2 = 2
		}
		if t2 < tier {
			t.Errorf("%s comes after a file of a later kind", g.Path)
		}
		tier = max(tier, t2)
	}

	if len(setup.Show) != 0 {
		t.Errorf("show = %+v, want none: the bundled provider has no initial password", setup.Show)
	}
	for _, s := range setup.Steps {
		if s.Instance == "pocket-id" || len(s.AskEnv) > 0 {
			t.Errorf("step %s/%s runs at install time or asks; the platform keeper takes neither", s.Instance, s.Name)
		}
	}
	if !slices.Contains(setup.Folders, render.SetupFolder{Path: "stacks", Mode: "0700"}) {
		t.Errorf("folders = %+v, want stacks", setup.Folders)
	}
	if setup.Keeper.Instance != "deployer-docker" || setup.Keeper.Module != "deployer-docker" || !slices.Equal(setup.Keeper.Command, []string{"seed"}) {
		t.Errorf("keeper = %+v", setup.Keeper)
	}
}

// TestPlatformKeeperScript checks the keeper's setup.sh (spec 0016, AC-1, AC-3): root first,
// only the domain and origin asked, the fixed ids, no secret made and none printed, and the seed
// with its labels and command.
func TestPlatformKeeperScript(t *testing.T) {
	t.Parallel()
	setup := keeperFiles(t)["setup.sh"]
	if !strings.Contains(setup, "set -eu\n\nif [ \"$(id -u)\" -ne 0 ]; then\n\techo \"run as root: sudo sh setup.sh\" >&2") {
		t.Error("setup.sh does not stop first unless it runs as root")
	}
	asks := regexp.MustCompile(`(?m)^\s*(ask|ask_secret|step_ask|graph_set) (\S+)`).FindAllStringSubmatch(setup, -1)
	var got []string
	for _, a := range asks {
		got = append(got, a[1]+" "+a[2])
	}
	if want := []string{"ask MP_INGRESS__DOMAIN", "ask MP_INGRESS__ORIGIN"}; !slices.Equal(got, want) {
		t.Errorf("setup.sh asks %q, want %q", got, want)
	}
	for _, never := range []string{"new_secret", "ask_secret", "step_ask", "docker compose up", "/dev/urandom", "$(id -u):"} {
		if strings.Contains(setup, never) {
			t.Errorf("setup.sh holds %q; the keeper does that now", never)
		}
	}
	for _, want := range []string{
		"env_set MP_UID 65532\n", "env_set MP_GID 65532\n",
		`docker run -d --name "$seed" --restart unless-stopped`,
		`--label "io.github.racherom.modular-platform.stack=platform" --label "io.github.racherom.modular-platform.module=deployer-docker"`,
		`-v /var/run/docker.sock:/var/run/docker.sock -v "$dir:$dir"`,
		`"$image" 'seed' --state "$dir"`,
		"image='" + platformImagePrefix + "deployer-docker:" + testTag + "'",
		"echo \"Open $(env_get MP_INGRESS__ORIGIN) and sign in.\"",
		"grep -q '^modular-platform-keeper-'",
		"seed='modular-platform-seed'",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup.sh lacks %q", want)
		}
	}
	if strings.Contains(setup, "--label \"io.github.racherom.modular-platform.instance=") {
		t.Error("the seed carries the instance label, so it would count as a keeper")
	}
}

// TestPlatformKeeperRefusals checks the platform flavor's refusals and keeper rule (spec 0016,
// AC-10): each case changes the fixture catalog after the check, so the check result stays ok
// and only the render refuses it.
func TestPlatformKeeperRefusals(t *testing.T) {
	t.Parallel()
	module := func(c contract.Catalog, name string) *contract.Module {
		for i := range c.Modules {
			if c.Modules[i].Name == name {
				return &c.Modules[i]
			}
		}
		t.Fatalf("no module %s", name)
		return nil
	}
	tests := map[string]struct {
		change      func(contract.Catalog)
		path, code  string
		messageHits string
	}{
		"no keeper": {func(c contract.Catalog) { module(c, "deployer-docker").Keeper = nil }, "modules", render.CodeKeeperMissing, "no instance of a keeper"},
		"two keepers": {func(c contract.Catalog) {
			module(c, "renderer").Keeper = &contract.Keeper{Command: []string{"seed"}}
		}, "modules", render.CodeKeeperDuplicate, "deployer-docker, renderer"},
		"asked secret": {func(c contract.Catalog) {
			m := module(c, "web")
			m.Secrets = append(slices.Clone(m.Secrets), contract.Secret{Name: "key", From: contract.SecretFromAsk, Description: "A key."})
		}, "modules.web", render.CodeSetupRequired, "secrets.key (from: ask)"},
		"step asks": {func(c contract.Catalog) {
			m := module(c, "pocket-id")
			m.Setup = []contract.SetupStep{{Name: "users", Command: []string{"true"}, Asks: []contract.SetupAsk{{Name: "x", Description: "X"}}}}
		}, "modules.pocket-id", render.CodeSetupRequired, "setup.users.asks"},
		"asked secret config": {func(c contract.Catalog) {
			m := module(c, "web")
			cfg := map[string]contract.ConfigValue{}
			maps.Copy(cfg, m.Config)
			cfg["token"] = contract.ConfigValue{Env: "WEB_TOKEN", Type: contract.ConfigValueTypeSecret, Setup: contract.ConfigValueSetupAsk, Description: "A token."}
			m.Config = cfg
		}, "modules.web", render.CodeSetupRequired, "config.token (an asked secret)"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, catalog, templates := keeperResult(t)
			tc.change(catalog)
			_, err := render.Platform(result, catalog, templates, render.WithKeeper())
			var refused *render.RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("render: %v, want a RefusedError", err)
			}
			if !strings.Contains(err.Error(), "platform flavor") {
				t.Errorf("error %q does not name the platform flavor", err)
			}
			if len(refused.Problems) != 1 {
				t.Fatalf("problems = %+v, want one", refused.Problems)
			}
			p := refused.Problems[0]
			if p.Path != tc.path || p.Code != tc.code || p.Severity != contract.Error || !strings.Contains(p.Message, tc.messageHits) {
				t.Errorf("problem = %+v, want %s at %s naming %q", p, tc.code, tc.path, tc.messageHits)
			}
		})
	}

	// the real authelia entry still asks the admin password until task 4, so the option refuses it
	result, catalog, templates := platformResult(t)
	_, err := render.Platform(result, catalog, templates, render.WithKeeper())
	var refused *render.RefusedError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 || refused.Problems[0].Path != "modules.authelia" {
		t.Errorf("keeper render of the milestone 4 catalog: %v, want setup_required at modules.authelia", err)
	}
}

// TestDecodeSetup refuses what the keeper must not apply (spec 0016, check): another
// formatVersion, an unknown field, text after the object, and text that is no object.
func TestDecodeSetup(t *testing.T) {
	t.Parallel()
	good := `{"folders":[],"formatVersion":1,"generate":[],"keeper":{"command":["seed"],"instance":"k","module":"k"},"show":[],"steps":[]}`
	if _, err := render.DecodeSetup([]byte(good)); err != nil {
		t.Fatalf("good: %v", err)
	}
	for name, text := range map[string]string{
		"version 2":     strings.Replace(good, `"formatVersion":1`, `"formatVersion":2`, 1),
		"no version":    strings.Replace(good, `"formatVersion":1,`, "", 1),
		"unknown field": strings.Replace(good, `"show":[]`, `"show":[],"later":1`, 1),
		"unknown deep":  strings.Replace(good, `"instance":"k"`, `"instance":"k","later":1`, 1),
		"trailing":      good + `{}`,
		"not json":      "formatVersion: 1",
	} {
		if _, err := render.DecodeSetup([]byte(text)); !errors.Is(err, render.ErrSetupInvalid) {
			t.Errorf("%s: %v, want ErrSetupInvalid", name, err)
		}
	}
}

// TestPlatformKeeperOIDCClientJob checks the client step of spec 0037 (AC-7, AC-8): api waits
// for its provision job, the job is on the internal network only and mounts the static API key,
// api does not, and no key value is in the Compose file.
func TestPlatformKeeperOIDCClientJob(t *testing.T) {
	t.Parallel()
	files := keeperFiles(t)
	var c struct {
		Services map[string]struct {
			DependsOn map[string]struct {
				Condition string `json:"condition"`
			} `json:"depends_on"`
			Networks map[string]any `json:"networks"`
			Secrets  []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"secrets"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal([]byte(files["docker-compose.yml"]), &c); err != nil {
		t.Fatal(err)
	}
	const job = "pocket-id_provision_api_oidc"
	if got := c.Services["api"].DependsOn[job].Condition; got != "service_completed_successfully" {
		t.Errorf("api depends_on %s = %q, want service_completed_successfully", job, got)
	}
	j := c.Services[job]
	if len(j.Networks) != 1 || !hasKey(j.Networks, "internal") {
		t.Errorf("job networks = %v, want internal only", j.Networks)
	}
	if j.Environment["MP_REDIRECT_PATH"] != "/api/v1/auth/callback" || j.Environment["MP_CLIENT_ID"] != "api_oidc" {
		t.Errorf("job env = %v", j.Environment)
	}
	hasKeyMount := func(name string) bool {
		for _, s := range c.Services[name].Secrets {
			if s.Target == "/run/secrets/admin/api_key" {
				return true
			}
		}
		return false
	}
	if !hasKeyMount(job) {
		t.Error("the job does not mount the static API key")
	}
	for _, s := range c.Services["api"].Secrets {
		if strings.Contains(s.Source, "static-api-key") || strings.Contains(s.Source, "oidc_api_key") {
			t.Errorf("api mounts %s", s.Source)
		}
	}
}

func hasKey(m map[string]any, k string) bool { _, ok := m[k]; return ok }
