package render_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
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
	return c
}

// planFile plans the graph file at path against catalog and fails the test unless it is ok.
func planFile(t *testing.T, path string, catalog contract.Catalog) contract.PlanResult {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		t.Fatal(p.Message)
	}
	plan := planner.Plan(g, catalog)
	if !plan.Ok {
		t.Fatalf("plan of %s: %+v", path, plan.Problems)
	}
	return plan
}

func samplePlan(t *testing.T) (contract.PlanResult, contract.Catalog) {
	t.Helper()
	catalog := loadCatalog(t, "../../catalog")
	return planFile(t, "../../samples/whoami-postgres/graph.yaml", catalog), catalog
}

// TestRenderSampleGolden pins every rendered file of the sample (AC-10 for docker-compose.yml).
// Run with -update after an intended change.
func TestRenderSampleGolden(t *testing.T) {
	plan, catalog := samplePlan(t)
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, filepath.Join("testdata", "whoami-postgres"), out)
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
	plan, catalog := samplePlan(t)
	out, err := render.Render(plan, catalog)
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
	want, _ := contract.MarshalGraph(plan.Graph)
	if !bytes.Equal(files["graph.yaml"].Data, want) || !strings.Contains(string(want), "database: whoami_postgresql") {
		t.Errorf("graph.yaml in the zip is not the resolved canonical graph:\n%s", files["graph.yaml"].Data)
	}
}

// TestZipDeterministic renders the sample twice and checks the zips are byte identical and hold
// exactly the AC-3 files, with modes.
func TestZipDeterministic(t *testing.T) {
	t.Parallel()
	plan, catalog := samplePlan(t)
	var zips [2]bytes.Buffer
	for i := range zips {
		out, err := render.Render(plan, catalog)
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
	if got := strings.Join(names, " "); got != ".env README.md docker-compose.yml graph.yaml setup.sh" {
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
	plan := contract.PlanResult{Ok: true, Graph: contract.Graph{
		Name:    "collide",
		Modules: map[string]contract.GraphModule{"a-b": {Module: "one"}, "a": {Module: "two"}},
	}}
	out, err := render.Render(plan, catalog)
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
	_, err := render.Render(contract.PlanResult{Ok: false}, contract.Catalog{})
	if !errors.Is(err, render.ErrPlanHasErrors) {
		t.Errorf("err = %v, want ErrPlanHasErrors", err)
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
