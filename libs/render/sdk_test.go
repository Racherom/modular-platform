package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/render"
)

const pairDir = "testdata/sdk-pair"

// pairPlan plans the graph of the two sdk test modules, a consumer bound to a provider.
func pairPlan(t *testing.T) (contract.PlanResult, []contract.CatalogEntry) {
	t.Helper()
	catalog, problems := contract.LoadCatalog(os.DirFS(filepath.Join(pairDir, "catalog")))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	data, err := os.ReadFile(filepath.Join(pairDir, "graph.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		t.Fatal(p.Message)
	}
	plan := planner.Plan(g, catalog)
	if !plan.Ok {
		t.Fatalf("plan: %+v", plan.Problems)
	}
	return plan, catalog
}

func renderPair(t *testing.T) map[string]render.File {
	t.Helper()
	plan, catalog := pairPlan(t)
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]render.File{}
	for _, f := range out.Files {
		files[f.Path] = f
	}
	return files
}

// TestRenderSDKGolden pins every rendered file of the sdk pair (AC-12). Run with -update after an
// intended change.
func TestRenderSDKGolden(t *testing.T) {
	plan, catalog := pairPlan(t)
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join(pairDir, "golden"), out)
}

// TestRenderSDKRules covers the render additions of spec 0005 one rule at a time.
func TestRenderSDKRules(t *testing.T) {
	t.Parallel()
	files := renderPair(t)
	compose := string(files["docker-compose.yml"].Data)
	services := compose[strings.Index(compose, "services:\n"):]
	split := strings.Index(services, "\n  provider:\n") + 1
	consumer, provider := services[:split], services[split:]
	for _, c := range []struct{ where, in, want string }{
		{"top level", compose, "configs:\n  consumer_env:\n    file: ./config/consumer.env\n  provider_env:\n    file: ./config/provider.env\n"},
		{"top level", compose, "  consumer_uses_greet:\n    file: ./secrets/consumer/uses_greet\n"},
		{"top level", compose, "  provider_signing-key:\n    file: ./secrets/provider/signing-key\n"},
		{"consumer", consumer, "    configs:\n    - source: consumer_env\n      target: /etc/modular-platform/module.env\n"},
		{"consumer", consumer, "      MP_ENV_FILE: /etc/modular-platform/module.env\n"},
		{"consumer", consumer, "      MP_INSTANCE: consumer\n"},
		{"consumer", consumer, "      MP_DOMAIN: sdk-pair.internal\n"},
		{"consumer", consumer, "    networks:\n      default:\n        aliases:\n        - consumer.sdk-pair.internal\n"},
		{"consumer", consumer, "      PAIR_GREET_URL: http://provider.sdk-pair.internal:8080\n"},
		{"consumer", consumer, "      PAIR_GREET_TOKEN_FILE: /run/secrets/uses/greet/token\n"},
		{"consumer", consumer, "    - source: consumer_uses_greet\n      target: /run/secrets/uses/greet/token\n"},
		{"consumer", consumer, "    user: ${MP_UID}:${MP_GID}\n"},
		{"provider", provider, "      MP_INSTANCE: provider\n"},
		{"provider", provider, "      MP_DOMAIN: sdk-pair.internal\n"},
		{"provider", provider, "    networks:\n      default:\n        aliases:\n        - provider.sdk-pair.internal\n"},
		{"provider", provider, "      PAIR_CLIENTS_DIR: /run/secrets/clients/greet\n"},
		{"provider", provider, "      PAIR_SIGNING_KEY_FILE: /run/secrets/provider_signing-key\n"},
		{"provider", provider, "    - source: consumer_uses_greet\n      target: /run/secrets/clients/greet/consumer\n"},
		{"provider", provider, "    - source: provider_signing-key\n"},
		{"provider", provider, "    user: ${MP_UID}:${MP_GID}\n"},
	} {
		if !strings.Contains(c.in, c.want) {
			t.Errorf("%s lacks %q:\n%s", c.where, c.want, c.in)
		}
	}

	want := map[string]string{
		"config/provider.env": `# Settings for provider (module pair-provider). Edit and restart the module to apply.
# Lines starting with # are defaults or optional values; remove the # to set one.

# The word the provider greets with.
# required
PAIR_GREETING=
`,
		"config/consumer.env": `# Settings for consumer (module pair-consumer). Edit and restart the module to apply.
# Lines starting with # are defaults or optional values; remove the # to set one.

# Free text the consumer never reads.
# optional
# PAIR_NOTE=

# Whether the consumer upper cases the answer.
# PAIR_SHOUT=false
`,
	}
	for path, text := range want {
		f := files[path]
		if string(f.Data) != text || f.Mode != 0o644 {
			t.Errorf("%s (mode %v) =\n%s\nwant\n%s", path, f.Mode, f.Data, text)
		}
	}

	setup := string(files["setup.sh"].Data)
	for _, w := range []string{
		"new_secret secrets/consumer/uses_greet\nnew_secret secrets/provider/signing-key\n",
		"set_id MP_UID \"$(id -u)\"\nset_id MP_GID \"$(id -g)\"\n",
	} {
		if !strings.Contains(setup, w) {
			t.Errorf("setup.sh lacks %q:\n%s", w, setup)
		}
	}
	readme := string(files["README.md"].Data)
	for _, w := range []string{"- `config/consumer.env`\n- `config/provider.env`\n", "- `secrets/consumer/uses_greet`\n"} {
		if !strings.Contains(readme, w) {
			t.Errorf("README.md lacks %q:\n%s", w, readme)
		}
	}
}

// TestRenderSDKCredentials covers a postgresql and an s3 binding: the url scheme by client kind,
// the credential files you place, sslmode, and the env file lines you fill.
func TestRenderSDKCredentials(t *testing.T) {
	t.Parallel()
	catalog := []contract.CatalogEntry{
		{Name: "pg", Image: "example.test/pg:1.0.0", Provides: map[string]contract.Provide{"postgresql": {Port: 5432}}},
		{Name: "minio", Image: "example.test/minio:1.0.0", Provides: map[string]contract.Provide{"s3": {Port: 9000}}},
		{Name: "app", Image: "example.test/app:1.0.0", Runtime: contract.Sdk, Uses: map[string]contract.Use{
			"postgresql": {Client: contract.Postgresql, Env: contract.UseEnv{
				Url: "DB_URL", Database: "DB_NAME", User: "DB_USER", PasswordFile: "DB_PASSWORD_FILE", Sslmode: "DB_SSLMODE",
			}},
			"s3": {Client: contract.S3, Env: contract.UseEnv{
				Url: "S3_URL", Bucket: "S3_BUCKET", Region: "S3_REGION", AccessKeyId: "S3_KEY_ID", SecretAccessKeyFile: "S3_SECRET_FILE",
			}},
		}},
	}
	plan := contract.PlanResult{
		Ok: true,
		Graph: contract.Graph{Name: "creds", Modules: map[string]contract.GraphModule{
			"app": {Module: "app"}, "db": {Module: "pg"}, "store": {Module: "minio"},
		}},
		Bindings: []contract.Binding{
			{Consumer: "app", Capability: "postgresql", Provider: "db"},
			{Consumer: "app", Capability: "s3", Provider: "store"},
		},
	}
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	for _, w := range []string{
		"      DB_URL: postgres://db:5432\n",
		"      DB_PASSWORD_FILE: /run/secrets/uses/postgresql/password\n",
		"      DB_SSLMODE: disable\n",
		"      S3_URL: http://store:9000\n",
		"      S3_SECRET_FILE: /run/secrets/uses/s3/secret_key\n",
		"  app_uses_postgresql_password:\n    file: ./secrets/app/uses_postgresql_password\n",
		"  app_uses_s3_secret_key:\n    file: ./secrets/app/uses_s3_secret_key\n",
	} {
		if !strings.Contains(files["docker-compose.yml"], w) {
			t.Errorf("docker-compose.yml lacks %q:\n%s", w, files["docker-compose.yml"])
		}
	}
	if strings.Contains(files["setup.sh"], "uses_postgresql_password") || strings.Contains(files["setup.sh"], "uses_s3_secret_key") {
		t.Error("setup.sh creates a credential file you place")
	}
	if !strings.Contains(files["README.md"], "- `secrets/app/uses_postgresql_password`\n- `secrets/app/uses_s3_secret_key`\n") {
		t.Errorf("README.md does not list the files you place:\n%s", files["README.md"])
	}
	wantEnv := `# Settings for app (module app). Edit and restart the module to apply.
# Lines starting with # are defaults or optional values; remove the # to set one.

# Binding uses.postgresql to db: required, from your db setup
DB_NAME=
DB_USER=

# Binding uses.s3 to store: required, from your store setup
S3_BUCKET=
S3_REGION=
S3_KEY_ID=
`
	if files["config/app.env"] != wantEnv {
		t.Errorf("config/app.env =\n%s\nwant\n%s", files["config/app.env"], wantEnv)
	}
}
