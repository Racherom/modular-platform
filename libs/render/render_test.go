package render_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

func init() {
	// a library the e2e test's compose module links in defines -update too; share it then
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "rewrite the golden files in testdata/")
	}
}

// updating reports whether -update asks to rewrite the golden files.
func updating() bool { return flag.Lookup("update").Value.String() == "true" }

// loadCatalog loads the catalogs under dirs as one, so test modules can name built in
// capabilities.
func loadCatalog(t *testing.T, dirs ...string) contract.Catalog {
	t.Helper()
	c, _ := loadCatalogTemplates(t, dirs...)
	return c
}

// builtinTemplates returns the templates of the built in catalog, loaded once: a module without
// templates never reads them, so every test may pass them.
func builtinTemplates(t testing.TB) contract.Templates {
	t.Helper()
	tmpls, err := builtinTemplatesOnce()
	if err != nil {
		t.Fatal(err)
	}
	return tmpls
}

var builtinTemplatesOnce = sync.OnceValues(func() (contract.Templates, error) {
	fsys := os.DirFS("../../catalog")
	c, problems := contract.LoadCatalog(fsys)
	if len(problems) > 0 {
		return contract.Templates{}, fmt.Errorf("built in catalog: %v", problems)
	}
	return contract.LoadTemplates(fsys, c)
})

// loadCatalogTemplates is loadCatalog plus the templates of its modules.
func loadCatalogTemplates(t *testing.T, dirs ...string) (contract.Catalog, contract.Templates) {
	t.Helper()
	merged := fstest.MapFS{}
	for _, dir := range dirs {
		fsys := os.DirFS(dir)
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
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
	c, problems := contract.LoadCatalog(merged)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	tmpls, err := contract.LoadTemplates(merged, c)
	if err != nil {
		t.Fatal(err)
	}
	return c, tmpls
}

// checkFile checks the graph file at path against catalog and fails the test unless it is ok.
func checkFile(t *testing.T, path string, catalog contract.Catalog) contract.CheckResult {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return resultText(t, string(data), catalog)
}

// resultText checks the graph text and fails the test unless the check result is ok.
func resultText(t *testing.T, text string, catalog contract.Catalog) contract.CheckResult {
	t.Helper()
	path := "graph"
	g, p := contract.DecodeGraph([]byte(text))
	if p != nil {
		t.Fatal(p.Message)
	}
	result := check.Graph(g, catalog)
	if !result.Ok {
		t.Fatalf("result of %s: %+v", path, result.Problems)
	}
	return result
}

func sampleResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "../../catalog")
	return checkFile(t, "../../samples/whoami-postgres/graph.yaml", catalog), catalog
}

// TestRenderSampleGolden pins every rendered file of the sample (AC-10 for docker-compose.yml).
// Run with -update after an intended change.
func TestRenderSampleGolden(t *testing.T) {
	result, catalog := sampleResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "whoami-postgres"), out)
}

// bindingsResult checks the bindings case: an external provide and an external provisioner, both
// and to_provider files, fixed path mounts, an optional field left out, environment
// placeholders, egress and aliases, and every hardening opt out (spec 0011 build tasks 11 to 13).
func bindingsResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "../../catalog", "testdata/bindings/catalog")
	return checkFile(t, "testdata/bindings/graph.yaml", catalog), catalog
}

// TestRenderBindingsGolden pins every rendered file of the bindings case (AC-8, AC-12, AC-15).
func TestRenderBindingsGolden(t *testing.T) {
	result, catalog := bindingsResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "bindings", "golden"), out)
}

// installResult checks the install time case: asked values of every type, an asked secret value,
// a from: ask secret, setup steps with asks, reads, writes and for_each over two clients, and
// clients whose own step reads the provider's step output (spec 0011 build task 14).
func installResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "testdata/install/catalog")
	return checkFile(t, "testdata/install/graph.yaml", catalog), catalog
}

// TestRenderInstallGolden pins every rendered file of the install case (AC-10).
func TestRenderInstallGolden(t *testing.T) {
	result, catalog := installResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "install", "golden"), out)
}

// templatesResult checks the templates case with the templates of its catalog (spec 0011 build
// task 15).
func templatesResult(t *testing.T) (contract.CheckResult, contract.Catalog, contract.Templates) {
	t.Helper()
	catalog, tmpls := loadCatalogTemplates(t, "../../catalog", "testdata/bindings/catalog", "testdata/templates/catalog")
	return checkFile(t, "testdata/templates/graph.yaml", catalog), catalog, tmpls
}

// TestRenderTemplatesGolden pins every rendered file of the templates case (AC-9): a template
// reading a bound and an unbound slot, the provide's clients in consumer order and its port, and
// a template with its own delims whose {{ }} text stays as written; each mounted read only
// through a Compose config. Rendering twice gives the same bytes.
func TestRenderTemplatesGolden(t *testing.T) {
	result, catalog, tmpls := templatesResult(t)
	out, err := render.Render(result, catalog, tmpls)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "templates", "golden"), out)

	again, err := render.Render(result, catalog, tmpls)
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
		t.Error("two renders of one graph differ")
	}

	if _, err := render.Render(result, catalog, builtinTemplates(t)); !errors.Is(err, contract.ErrNoTemplate) {
		t.Errorf("render without its templates: err = %v, want ErrNoTemplate", err)
	}
}

// hostAccessResult checks the host access case: published ports (a number and a config value),
// host mounts (read only and read write, a plain and a config path), and an entry's own
// entrypoint and command (spec 0011 build task 16).
func hostAccessResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "testdata/host-access/catalog")
	return checkFile(t, "testdata/host-access/graph.yaml", catalog), catalog
}

// TestRenderHostAccessGolden pins the static render of host access (spec 0013, Host access;
// AC-11, AC-12): ports, bind mounts and egress for the instance that publishes.
func TestRenderHostAccessGolden(t *testing.T) {
	result, catalog := hostAccessResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "host-access", "golden"), out)
}

// linuxCapResult checks the module of the Linux capability case that adds SYS_ADMIN beside one
// that adds only NET_BIND_SERVICE (spec 0022).
func linuxCapResult(t *testing.T) (contract.CheckResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "testdata/linuxcap/catalog")
	return checkFile(t, "testdata/linuxcap/refused.yaml", catalog), catalog
}

// TestRenderManagedRefuses: a deployed graph gets no host access and has no setup.sh, so the
// managed flavor refuses each such instance with an error naming it, and renders nothing (spec
// 0013, Managed flavor; AC-11).
func TestRenderManagedRefuses(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		result func(*testing.T) (contract.CheckResult, contract.Catalog)
		want   []string // code and path of each refusal
	}{
		"host access": {hostAccessResult, []string{"host_access modules.agent", "host_access modules.edge"}},
		"linux caps":  {linuxCapResult, []string{"host_access modules.admin"}},
		"install time": {installResult, []string{
			"setup_required modules.alpha", "setup_required modules.beta", "setup_required modules.keeper",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, catalog := c.result(t)
			_, err := render.Managed(result, catalog, builtinTemplates(t), "d1", nil)
			var refused *render.RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want a *RefusedError", err)
			}
			var got []string
			for _, p := range refused.Problems {
				if p.Severity != contract.Error || p.Message == "" {
					t.Errorf("%s at %s: severity %s, message %q", p.Code, p.Path, p.Severity, p.Message)
				}
				got = append(got, p.Code+" "+p.Path)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("refusals = %v, want %v", got, c.want)
			}
		})
	}
}

// TestRenderManagedRefusesLinuxCap names every reason in the refusal, and the static render
// still copies each name for the zip you run yourself (spec 0022, AC-11).
func TestRenderManagedRefusesLinuxCap(t *testing.T) {
	t.Parallel()
	result, catalog := linuxCapResult(t)
	_, err := render.Managed(result, catalog, builtinTemplates(t), "d1", nil)
	var refused *render.RefusedError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 {
		t.Fatalf("err = %v, want one refusal", err)
	}
	want := "Instance admin adds the Linux capability SYS_ADMIN, which a deployed graph never gets; download the zip to run it."
	if got := refused.Problems[0].Message; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	if compose := string(fileData(t, out, "docker-compose.yml")); !strings.Contains(compose, "- SYS_ADMIN") {
		t.Errorf("the static render lacks cap_add SYS_ADMIN:\n%s", compose)
	}
}

// TestRefuse: the refused result is not ok, has no digest, and keeps one problem per code and path,
// the error, sorted by path then code.
func TestRefuse(t *testing.T) {
	t.Parallel()
	result, _ := hostAccessResult(t)
	if !result.Ok || result.Digest == "" || len(result.Problems) != 2 {
		t.Fatalf("result: ok %v, digest %q, problems %+v", result.Ok, result.Digest, result.Problems)
	}
	refused := render.Refuse(result, []contract.Problem{
		{Severity: contract.Error, Code: render.CodeSetupRequired, Path: "modules.edge", Message: "m"},
		{Severity: contract.Error, Code: render.CodeHostAccess, Path: "modules.edge", Message: "m"},
	})
	if refused.Ok || refused.Digest != "" {
		t.Errorf("refused: ok %v, digest %q", refused.Ok, refused.Digest)
	}
	var got []string
	for _, p := range refused.Problems {
		got = append(got, string(p.Severity)+" "+p.Code+" "+p.Path)
	}
	want := []string{"warning host_access modules.agent", "error host_access modules.edge", "error setup_required modules.edge"}
	if !slices.Equal(got, want) {
		t.Errorf("problems = %v, want %v", got, want)
	}
	if len(result.Problems) != 2 || result.Problems[1].Severity != contract.Warning {
		t.Error("Refuse changed the check result it was given")
	}
}

// checkGolden compares each rendered file with its copy under dir, or rewrites it with -update.
func checkGolden(t *testing.T, dir string, out render.Output) {
	t.Helper()
	for _, f := range out.Files {
		golden := filepath.Join(dir, f.Path)
		if updating() {
			if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, f.Data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (run go test -update)", f.Path, err)
		}
		if !bytes.Equal(f.Data, want) {
			t.Errorf("%s differs from %s\n--- got\n%s", f.Path, golden, f.Data)
		}
	}
}

func TestRenderSampleRules(t *testing.T) {
	t.Parallel()
	result, catalog := sampleResult(t)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]render.File{}
	for _, f := range out.Files {
		files[f.Path] = f
	}
	compose := string(files["docker-compose.yml"].Data)
	for _, want := range []string{
		"name: whoami-postgres\n",
		"      DEMO_POSTGRESQL_HOST: postgres.whoami-postgres.internal\n",
		"      DEMO_POSTGRESQL_PORT: \"5432\"\n",
		"      POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password\n",
		"  postgres_password:\n    file: ./secrets/postgres/password\n",
		"networks:\n  internal:\n    internal: true\n",
		"      internal:\n        aliases:\n        - whoami.whoami-postgres.internal\n",
		"    - source: ./data/postgres/data\n      target: /var/lib/postgresql\n      type: bind\n",
		"    tmpfs:\n    - /var/run/postgresql\n    - /tmp\n",
		"  postgres_provision_whoami_postgresql:\n",
		"      MP_DATABASE: whoami_postgresql\n",
		"      MP_ADMIN_ADMIN_PASSWORD_FILE: /run/secrets/admin/admin_password\n",
		"      MP_PASSWORD_FILE: /run/secrets/bind/password\n",
		"    restart: \"no\"\n",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("docker-compose.yml lacks %q\n%s", want, compose)
		}
	}
	whoami := from(t, compose, "  whoami:\n")
	for _, want := range []string{
		"      postgres:\n        condition: service_healthy\n",
		"      postgres_provision_whoami_postgresql:\n        condition: service_completed_successfully\n",
		"    read_only: true\n", "    cap_drop:\n    - ALL\n", "    - no-new-privileges:true\n",
		"    user: ${MP_UID}:${MP_GID}\n",
	} {
		if !strings.Contains(whoami, want) {
			t.Errorf("whoami lacks %q\n%s", want, whoami)
		}
	}
	if strings.Contains(whoami, "secrets:") {
		t.Error("whoami maps no file field, so it must get no secret")
	}
	if strings.Contains(compose, ": null") || strings.Contains(compose, "- null") || strings.Contains(compose, "ports:") {
		t.Error("docker-compose.yml must hold no null and publish no port")
	}
	if !strings.Contains(compose, "$$MP_ADMIN_HOST") || strings.Contains(compose, " $MP_ADMIN_HOST") {
		t.Error("the job script must escape $ so Compose leaves it to the shell")
	}
	if got := files["setup.sh"].Mode; got != 0o755 {
		t.Errorf("setup.sh mode = %v", got)
	}
	setup := string(files["setup.sh"].Data)
	for _, want := range []string{
		"new_secret secrets/postgres/password\n", "new_secret secrets/whoami/uses_postgresql_password\n",
		"new_folder data/postgres/data\n", "set_id MP_UID",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup.sh lacks %q", want)
		}
	}
	want, _ := contract.MarshalGraph(result.Graph)
	if !bytes.Equal(files["graph.yaml"].Data, want) || !strings.Contains(string(want), "database: whoami_postgresql") {
		t.Errorf("graph.yaml in the zip is not the resolved canonical graph:\n%s", files["graph.yaml"].Data)
	}
}

// TestZipDeterministic renders the sample twice and checks the zips are byte identical and hold
// exactly the AC-3 files, with modes.
func TestZipDeterministic(t *testing.T) {
	t.Parallel()
	result, catalog := sampleResult(t)
	var zips [2]bytes.Buffer
	for i := range zips {
		out, err := render.Render(result, catalog, builtinTemplates(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := out.WriteZip(&zips[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(zips[0].Bytes(), zips[1].Bytes()) {
		t.Fatal("two renders of the same graph give different zips")
	}

	zr, err := zip.NewReader(bytes.NewReader(zips[0].Bytes()), int64(zips[0].Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "setup.sh" && f.Mode() != 0o755 {
			t.Errorf("setup.sh mode in zip = %v", f.Mode())
		}
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if bytes.Contains(body, []byte("POSTGRES_PASSWORD=")) {
			t.Errorf("%s holds a password value", f.Name)
		}
	}
	if got := strings.Join(names, " "); got != ".env README.md config/postgres/pg_hba.conf config/postgres/postgresql.conf docker-compose.yml graph.yaml setup.sh" {
		t.Errorf("zip entries = %s", got)
	}
}

// TestSecretComposeNamesNeverCollide renders two pairs that a `-` join would merge into `a-b-c`
// (spec 0002, Secrets in Compose).
func TestSecretComposeNamesNeverCollide(t *testing.T) {
	t.Parallel()
	catalog := contract.Catalog{Modules: []contract.Module{
		{Name: "one", Image: "example.test/one:1.0.0", Secrets: []contract.Secret{{Name: "c", Env: "C_FILE"}}},
		{Name: "two", Image: "example.test/two:1.0.0", Secrets: []contract.Secret{{Name: "b-c", Env: "BC_FILE"}}},
	}}
	result := contract.CheckResult{Ok: true, Graph: contract.Graph{
		Name:    "collide",
		Modules: map[string]contract.GraphModule{"a-b": {Module: "one"}, "a": {Module: "two"}},
	}}
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	var compose string
	for _, f := range out.Files {
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	top, _, _ := strings.Cut(from(t, compose, "secrets:\n"), "services:\n")
	if want := "secrets:\n  a-b_c:\n    file: ./secrets/a-b/c\n  a_b-c:\n    file: ./secrets/a/b-c\n"; top != want {
		t.Errorf("top level secrets =\n%s\nwant\n%s", top, want)
	}
	services := from(t, compose, "services:\n")
	ab := from(t, services, "  a-b:\n")
	a, _, _ := strings.Cut(from(t, services, "  a:\n"), "  a-b:\n")
	for _, c := range []struct{ svc, want, not string }{
		{a, "      BC_FILE: /run/secrets/a_b-c\n", "a-b_c"},
		{a, "    - source: a_b-c\n", "a-b_c"},
		{ab, "      C_FILE: /run/secrets/a-b_c\n", "a_b-c"},
		{ab, "    - source: a-b_c\n", "a_b-c"},
	} {
		if !strings.Contains(c.svc, c.want) || strings.Contains(c.svc, c.not) {
			t.Errorf("service lacks %q or holds %q:\n%s", c.want, c.not, c.svc)
		}
	}
}

func TestRenderRefusesErrors(t *testing.T) {
	t.Parallel()
	_, err := render.Render(contract.CheckResult{Ok: false}, contract.Catalog{}, builtinTemplates(t))
	if !errors.Is(err, render.ErrGraphHasErrors) {
		t.Errorf("err = %v, want ErrGraphHasErrors", err)
	}
}

// from returns s from the first marker on, and fails the test when s lacks the marker.
func from(t *testing.T, s, marker string) string {
	t.Helper()
	i := strings.Index(s, marker)
	if i < 0 {
		t.Fatalf("%q is missing from\n%s", marker, s)
	}
	return s[i:]
}
