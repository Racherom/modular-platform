package render_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

const pairDir = "testdata/sdk-pair"

// pairResult checks the graph of the two sdk test modules, a consumer bound to a provider.
func pairResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, filepath.Join(pairDir, "catalog"))
	return checkFile(t, filepath.Join(pairDir, "graph.yaml"), catalog), catalog
}

func renderPair(t *testing.T) map[string]render.File {
	t.Helper()
	result, catalog := pairResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
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
	result, catalog := pairResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
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
	services := from(t, compose, "services:\n")
	split := strings.Index(services, "\n  provider:\n") + 1
	consumer, provider := services[:split], services[split:]
	for _, c := range []struct{ where, in, want string }{
		{"top level", compose, "configs:\n  consumer_env:\n    file: ./config/consumer.env\n  provider_env:\n    file: ./config/provider.env\n"},
		{"top level", compose, "  consumer_uses_greet_token:\n    file: ./secrets/consumer/uses_greet_token\n"},
		{"top level", compose, "  provider_signing-key:\n    file: ./secrets/provider/signing-key\n"},
		{"consumer", consumer, "    configs:\n    - source: consumer_env\n      target: /etc/modular-platform/module.env\n"},
		{"consumer", consumer, "      MP_ENV_FILE: /etc/modular-platform/module.env\n"},
		{"consumer", consumer, "      MP_INSTANCE: consumer\n"},
		{"consumer", consumer, "      MP_DOMAIN: sdk-pair.internal\n"},
		{"consumer", consumer, "    networks:\n      internal:\n        aliases:\n        - consumer.sdk-pair.internal\n"},
		{"consumer", consumer, "      PAIR_GREET_URL: http://provider.sdk-pair.internal:8080\n"},
		{"consumer", consumer, "      PAIR_GREET_TOKEN_FILE: /run/secrets/uses/greet/token\n"},
		{"consumer", consumer, "    - source: consumer_uses_greet_token\n      target: /run/secrets/uses/greet/token\n"},
		{"consumer", consumer, "    user: ${MP_UID}:${MP_GID}\n"},
		{"provider", provider, "      MP_INSTANCE: provider\n"},
		{"provider", provider, "      MP_DOMAIN: sdk-pair.internal\n"},
		{"provider", provider, "    networks:\n      internal:\n        aliases:\n        - provider.sdk-pair.internal\n"},
		{"provider", provider, "      PAIR_CLIENTS_DIR: /run/secrets/clients/greet/token\n"},
		{"provider", provider, "      PAIR_SIGNING_KEY_FILE: /run/secrets/provider_signing-key\n"},
		{"provider", provider, "    - source: consumer_uses_greet_token\n      target: /run/secrets/clients/greet/token/consumer\n"},
		{"provider", provider, "    - source: provider_signing-key\n      target: /run/secrets/provider_signing-key\n"},
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
		"new_secret secrets/provider/signing-key\nnew_secret secrets/consumer/uses_greet_token\n",
		"set_id MP_UID \"$(id -u)\"\nset_id MP_GID \"$(id -g)\"\n",
	} {
		if !strings.Contains(setup, w) {
			t.Errorf("setup.sh lacks %q:\n%s", w, setup)
		}
	}
	readme := string(files["README.md"].Data)
	for _, w := range []string{"- `config/consumer.env`\n- `config/provider.env`\n", "- `secrets/consumer/uses_greet_token`\n"} {
		if !strings.Contains(readme, w) {
			t.Errorf("README.md lacks %q:\n%s", w, readme)
		}
	}
}

// TestRenderExternal covers a binding to an external instance (AC-15): text fields from the
// graph, a secret field as a file you place that setup.sh checks, and no service for it.
func TestRenderExternal(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "../../catalog")
	catalog.Modules = append(catalog.Modules, contract.Module{
		Name: "app", Image: "example.test/app:1.0.0", Runtime: contract.Sdk,
		Uses: map[string]contract.Slot{"db": {Capability: "postgresql", Client: contract.Postgresql, Env: map[string]string{
			"url": "DB_URL", "database": "DB_NAME", "user": "DB_USER", "password": "DB_PASSWORD_FILE", "sslmode": "DB_SSLMODE",
		}}},
	})
	g := contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: "ext", Modules: map[string]contract.GraphModule{
		"app": bound("app", map[string]string{"db": "billing-db"}),
		"billing-db": {External: &contract.ExternalInstance{Provides: map[string]contract.ExternalProvide{"db": {
			Capability: "postgresql", Version: "16.4", Fields: map[string]string{
				"host": "db.example.com", "port": "5432", "url": "postgres://db.example.com:5432",
				"database": "billing", "user": "billing", "sslmode": "verify-full",
			},
		}}}},
	}}
	result := check.Graph(g, catalog)
	if !result.Ok {
		t.Fatalf("result: %+v", result.Problems)
	}
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	compose := files["docker-compose.yml"]
	for _, w := range []string{
		"      DB_URL: postgres://db.example.com:5432\n",
		"      DB_NAME: billing\n",
		"      DB_PASSWORD_FILE: /run/secrets/uses/db/password\n",
		"      DB_SSLMODE: verify-full\n",
		"  app_uses_db_password:\n    file: ./secrets/billing-db/db_password\n",
	} {
		if !strings.Contains(compose, w) {
			t.Errorf("docker-compose.yml lacks %q:\n%s", w, compose)
		}
	}
	if strings.Contains(compose, "  billing-db:\n") || strings.Contains(compose, "depends_on") {
		t.Error("an external instance has no service and nothing waits for it")
	}
	if strings.Contains(files["setup.sh"], "new_secret secrets/billing-db/db_password") ||
		!strings.Contains(files["setup.sh"], "need secrets/billing-db/db_password 'password of billing-db, a postgresql service. Password of the login.'") {
		t.Errorf("setup.sh must check, not create, the placed password:\n%s", files["setup.sh"])
	}
	if !strings.Contains(files["README.md"], "- `secrets/billing-db/db_password`: password of billing-db. Password of the login.\n") {
		t.Errorf("README.md does not list the file you place:\n%s", files["README.md"])
	}
}
