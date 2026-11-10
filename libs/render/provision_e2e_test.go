//go:build e2e

package render_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestProvisionEndToEnd runs the provision thread for real (spec 0011 AC-1, AC-4, AC-5, AC-8):
// two clients and a module with two slots of postgresql bound to one postgres instance each get
// their own database and login, sign in with the delivered values, cannot open another's
// database or postgres, a second up changes no role or database, no service env holds a secret,
// and a failing provision job keeps its consumers from starting. It needs Docker with the
// Compose plugin; run it with go test -tags e2e.
func TestProvisionEndToEnd(t *testing.T) {
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	result := checkFile(t, "testdata/provision/graph.yaml", catalog)
	out, err := render.Render(result, catalog, builtinTemplates(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range out.Files {
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "sh", "setup.sh")

	project := "mp-e2e-" + strings.ToLower(strings.ReplaceAll(filepath.Base(dir), "_", "-"))
	compose := func(args ...string) (string, error) {
		cmd := exec.CommandContext(t.Context(), "docker", append([]string{"compose", "-p", project}, args...)...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	t.Cleanup(func() {
		cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "compose", "-p", project, "down", "-v", "-t", "1")
		cmd.Dir = dir
		_ = cmd.Run()
	})
	up := func() (string, error) { return compose("up", "-d", "--wait", "--wait-timeout", "180") }

	// AC-1, AC-5: each client is healthy only once it signs in with its delivered values
	if out, err := up(); err != nil {
		t.Fatalf("up: %v\n%s", err, out)
	}
	secret := func(path string) string {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	signIn := func(service, user, password, database string) error {
		_, err := compose("exec", "-T", "-e", "PGPASSWORD="+password, service,
			"psql", "-h", "postgres.provision-e2e.internal", "-U", user, "-d", database, "-tAc", "select 1")
		return err
	}
	aPass := secret("secrets/a/uses_db_password")
	if err := signIn("a", "a_db", aPass, "a_db"); err != nil {
		t.Errorf("a cannot sign in to its own database: %v", err)
	}
	// AC-4: a login opens only its own database
	for _, db := range []string{"b_db", "reports_main_db", "postgres"} {
		if err := signIn("a", "a_db", aPass, db); err == nil {
			t.Errorf("a signed in to %s", db)
		}
	}
	if err := signIn("reports", "reports_audit_db", secret("secrets/reports/uses_audit-db_password"), "reports_audit_db"); err != nil {
		t.Errorf("reports cannot sign in through its second slot: %v", err)
	}

	// AC-5: a second up adds or drops no role or database, and the same password still signs in
	admin := secret("secrets/postgres/password")
	count := func() string {
		out, err := compose("exec", "-T", "-e", "PGPASSWORD="+admin, "postgres",
			"psql", "-h", "127.0.0.1", "-U", "postgres", "-tAc",
			"select string_agg(rolname, ',' order by rolname) from pg_roles; select string_agg(datname, ',' order by datname) from pg_database")
		if err != nil {
			t.Fatalf("count: %v\n%s", err, out)
		}
		return out
	}
	before := count()
	if out, err := up(); err != nil {
		t.Fatalf("second up: %v\n%s", err, out)
	}
	if after := count(); after != before {
		t.Errorf("a second up changed roles or databases:\n%s\n%s", before, after)
	}
	if err := signIn("a", "a_db", aPass, "a_db"); err != nil {
		t.Errorf("a cannot sign in after the second up: %v", err)
	}

	// AC-8: no service env holds a secret value
	ids, err := compose("ps", "-aq")
	if err != nil {
		t.Fatal(err)
	}
	secrets, _ := filepath.Glob(filepath.Join(dir, "secrets", "*", "*"))
	for id := range strings.FieldsSeq(ids) {
		b, err := exec.CommandContext(t.Context(), "docker", "inspect", "--format", "{{json .Config.Env}}", id).Output()
		if err != nil {
			t.Fatal(err)
		}
		var env []string
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatal(err)
		}
		for _, path := range secrets {
			value, _ := os.ReadFile(path)
			for _, e := range env {
				if len(value) > 0 && strings.Contains(e, string(value)) {
					t.Errorf("container %s has the value of %s in its env", id, path)
				}
			}
		}
	}

	// AC-5: a failing job (wrong admin password) keeps its consumers from starting
	if out, err := compose("down", "-t", "1"); err != nil {
		t.Fatalf("down: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets/postgres/password"), []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := up(); err == nil {
		t.Fatalf("up with a wrong admin password succeeded:\n%s", out)
	}
	states, _ := compose("ps", "-a", "--format", "{{.Service}} {{.State}}")
	for line := range strings.SplitSeq(strings.TrimSpace(states), "\n") {
		service, state, _ := strings.Cut(line, " ")
		if (service == "a" || service == "b" || service == "reports") && state == "running" {
			t.Errorf("%s started although its provision job failed", service)
		}
	}
}
