package render_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func samplePlan(t *testing.T) (contract.PlanResult, []contract.CatalogEntry) {
	t.Helper()
	catalog, problems := contract.LoadCatalog(os.DirFS("../../catalog"))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	data, err := os.ReadFile("../../samples/whoami-postgres/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		t.Fatal(p.Message)
	}
	return planner.Plan(g, catalog), catalog
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
		"    depends_on:\n      postgres:\n        condition: service_healthy\n",
		"      DEMO_POSTGRESQL_HOST: postgres\n",
		"      DEMO_POSTGRESQL_PORT: \"5432\"\n",
		"      POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password\n",
		"  postgres_password:\n    file: ./secrets/postgres/password\n",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("docker-compose.yml lacks %q\n%s", want, compose)
		}
	}
	whoami := compose[strings.Index(compose, "  whoami:\n"):]
	if strings.Contains(whoami, "secrets:") {
		t.Error("whoami must get no secret")
	}
	if strings.Contains(compose, "null") || strings.Contains(compose, "ports:") {
		t.Error("docker-compose.yml must hold no null and publish no port")
	}
	if got := files["setup.sh"].Mode; got != 0o755 {
		t.Errorf("setup.sh mode = %v", got)
	}
	if !strings.Contains(string(files["setup.sh"].Data), "new_secret secrets/postgres/password\n") {
		t.Error("setup.sh does not create the postgres password")
	}
	want, _ := os.ReadFile("../../samples/whoami-postgres/graph.yaml")
	if !bytes.Equal(files["graph.yaml"].Data, want) {
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
	catalog := []contract.CatalogEntry{
		{Name: "one", Image: "example.test/one:1.0.0", Secrets: []contract.Secret{{Name: "c", Env: "C_FILE"}}},
		{Name: "two", Image: "example.test/two:1.0.0", Secrets: []contract.Secret{{Name: "b-c", Env: "BC_FILE"}}},
	}
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
	top := compose[strings.Index(compose, "secrets:\n"):strings.Index(compose, "services:\n")]
	if want := "secrets:\n  a-b_c:\n    file: ./secrets/a-b/c\n  a_b-c:\n    file: ./secrets/a/b-c\n"; top != want {
		t.Errorf("top level secrets =\n%s\nwant\n%s", top, want)
	}
	services := compose[strings.Index(compose, "services:\n"):]
	a, ab := services[strings.Index(services, "  a:\n"):strings.Index(services, "  a-b:\n")], services[strings.Index(services, "  a-b:\n"):]
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
	_, err := render.Render(contract.PlanResult{Ok: false}, nil)
	if !errors.Is(err, render.ErrPlanHasErrors) {
		t.Errorf("err = %v, want ErrPlanHasErrors", err)
	}
}
