//go:build e2e

package render_test

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/render"
)

// stack is a rendered project in a folder, run by docker compose under its own project name.
type stack struct {
	t       *testing.T
	dir     string
	project string
	catalog contract.Catalog
}

func newStack(t *testing.T, catalog contract.Catalog) *stack {
	t.Helper()
	dir := t.TempDir()
	s := &stack{
		t: t, dir: dir, catalog: catalog,
		project: "mp-e2e-" + strings.ToLower(strings.ReplaceAll(filepath.Base(dir), "_", "-")),
	}
	t.Cleanup(func() {
		cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "compose", "-p", s.project, "down", "-v", "-t", "1", "--remove-orphans")
		cmd.Dir = dir
		_ = cmd.Run()
	})
	return s
}

// render plans g, renders it over the files of the folder (keeping secrets/ and data/), and
// returns the rendered Compose file.
func (s *stack) render(g contract.Graph) string {
	s.t.Helper()
	plan := planner.Plan(g, s.catalog)
	if !plan.Ok {
		s.t.Fatalf("plan: %+v", plan.Problems)
	}
	out, err := render.Render(plan, s.catalog)
	if err != nil {
		s.t.Fatal(err)
	}
	compose := ""
	for _, f := range out.Files {
		path := filepath.Join(s.dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			s.t.Fatal(err)
		}
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	return compose
}

// setup runs setup.sh and returns its output and error.
func (s *stack) setup() (string, error) {
	cmd := exec.CommandContext(s.t.Context(), "sh", "setup.sh")
	cmd.Dir = s.dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func (s *stack) compose(args ...string) (string, error) {
	cmd := exec.CommandContext(s.t.Context(), "docker", append([]string{"compose", "-p", s.project}, args...)...)
	cmd.Dir = s.dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// up starts the stack the way the README says after a change, and fails the test unless every
// service is healthy and every job exited 0.
func (s *stack) up() {
	s.t.Helper()
	if out, err := s.compose("up", "-d", "--wait", "--wait-timeout", "180", "--remove-orphans"); err != nil {
		s.t.Fatalf("up: %v\n%s", err, out)
	}
}

func (s *stack) secret(path string) string {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, path))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

func graphOf(name string, modules map[string]contract.GraphModule) contract.Graph {
	return contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: name, Modules: modules}
}

// TestExternalEndToEnd binds one unchanged consumer entry either to a provisioned database or to
// an external server (spec 0011 AC-1, AC-15): both renders give its service the same env names
// with the password only as a file path; setup.sh refuses to go on until the external's
// password file is placed and creates nothing in its place; then the consumer signs in to the
// external server with the delivered values. It needs Docker with the Compose plugin; run it
// with go test -tags e2e.
func TestExternalEndToEnd(t *testing.T) {
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	consumer := contract.GraphModule{Module: "pg-roaming"}
	const alias = "ext-db.example.test"

	internal := newStack(t, catalog)
	internalEnv := envOf(t, internal.render(graphOf("external-e2e", map[string]contract.GraphModule{
		"app": consumer, "postgres": {Module: "postgres"},
	})), "app")

	s := newStack(t, catalog)
	externalEnv := envOf(t, s.render(graphOf("external-e2e", map[string]contract.GraphModule{
		"app": consumer,
		"ext": {External: &contract.ExternalInstance{Provides: map[string]contract.ExternalProvide{"db": {
			Capability: "postgresql", Version: "18.6",
			Fields: map[string]string{
				"host": alias, "port": "5432", "url": "postgres://" + alias + ":5432",
				"database": "ext", "user": "ext", "sslmode": "disable",
			},
		}}}},
	})), "app")

	// AC-1: the same env names in both renders, the password only as a file path
	if a, b := slices.Sorted(maps.Keys(internalEnv)), slices.Sorted(maps.Keys(externalEnv)); !slices.Equal(a, b) {
		t.Errorf("env names differ:\nprovisioned %v\nexternal    %v", a, b)
	}
	for _, env := range []map[string]string{internalEnv, externalEnv} {
		if got := env["DB_PASSWORD_FILE"]; !strings.HasPrefix(got, "/run/secrets/") {
			t.Errorf("DB_PASSWORD_FILE = %q, want a mounted file path", got)
		}
	}

	// AC-15: setup.sh lists the file you place, with its description, and creates nothing there
	out, err := s.setup()
	if err == nil || !strings.Contains(out, "missing secrets/ext/db_password: password of ext, a postgresql service. Password of the login.") {
		t.Fatalf("setup.sh without the placed file: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "secrets/ext/db_password")); err == nil {
		t.Fatal("setup.sh created the file you place")
	}

	// place the file, then start the external server: a postgres outside the project, reachable
	// over the project's egress network
	const password = "external-secret-password"
	if err := os.MkdirAll(filepath.Join(s.dir, "secrets/ext"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "secrets/ext/db_password"), []byte(password), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := s.setup(); err != nil {
		t.Fatalf("setup.sh with the placed file: %v\n%s", err, out)
	}
	if out, err := s.compose("up", "--no-start"); err != nil {
		t.Fatalf("create the project: %v\n%s", err, out)
	}
	server := s.project + "-ext"
	t.Cleanup(func() {
		_ = exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "rm", "-f", "-v", server).Run()
	})
	if out, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--name", server,
		"--network", s.project+"_egress", "--network-alias", alias,
		"-e", "POSTGRES_USER=ext", "-e", "POSTGRES_DB=ext", "-e", "POSTGRES_PASSWORD="+password,
		"--health-cmd", "pg_isready -h 127.0.0.1 -U ext", "--health-interval", "1s", "--health-retries", "60",
		"postgres:18.6").CombinedOutput(); err != nil {
		t.Fatalf("start the external server: %v\n%s", err, out)
	}
	waitHealthy(t, server)

	// AC-1: the consumer is healthy only once it signs in with the delivered values
	s.up()
	if got := s.secret("secrets/ext/db_password"); got != password {
		t.Errorf("setup.sh changed the placed file to %q", got)
	}
}

// envOf reads the environment of service from a rendered Compose file.
func envOf(t *testing.T, compose, service string) map[string]string {
	t.Helper()
	var project struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal([]byte(compose), &project); err != nil {
		t.Fatal(err)
	}
	return project.Services[service].Environment
}

// waitHealthy waits until container reports healthy.
func waitHealthy(t *testing.T, container string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c",
		`for i in $(seq 120); do [ "$(docker inspect -f '{{.State.Health.Status}}' "$1")" = healthy ] && exit 0; sleep 1; done; exit 1`,
		"wait", container)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s did not become healthy: %v\n%s", container, err, out)
	}
}
