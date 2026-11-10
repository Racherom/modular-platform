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

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
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

// render checks g, renders it over the files of the folder (keeping secrets/ and data/), and
// returns the rendered Compose file.
func (s *stack) render(g contract.Graph) string {
	s.t.Helper()
	result := check.Graph(g, s.catalog)
	if !result.Ok {
		s.t.Fatalf("result: %+v", result.Problems)
	}
	out, err := render.Render(result, s.catalog, builtinTemplates(s.t))
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

// apply renders g like render, then runs setup.sh as a download of a changed graph needs: the
// new download's .env no longer holds the user ids, and a new binding needs its secret files.
func (s *stack) apply(g contract.Graph) string {
	s.t.Helper()
	compose := s.render(g)
	if out, err := s.setup(); err != nil {
		s.t.Fatalf("setup.sh: %v\n%s", err, out)
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
	if out, err := s.tryUp(); err != nil {
		s.t.Fatalf("up: %v\n%s", err, out)
	}
}

// tryUp runs up like up and returns its output and error.
func (s *stack) tryUp() (string, error) {
	return s.compose("up", "-d", "--wait", "--wait-timeout", "180", "--remove-orphans")
}

// state is the state of service's container, as docker compose ps reports it.
func (s *stack) state(service string) string {
	s.t.Helper()
	out, err := s.compose("ps", "-a", "--format", "{{.State}}", service)
	if err != nil {
		s.t.Fatalf("ps %s: %v\n%s", service, err, out)
	}
	return strings.TrimSpace(out)
}

func (s *stack) secret(path string) string {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, path))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

// psql runs one query on the postgres service over TCP as user, and returns its output.
func (s *stack) psql(user, password, database, query string) (string, error) {
	return s.compose("exec", "-T", "-e", "PGPASSWORD="+password, "postgres",
		"psql", "-h", "127.0.0.1", "-U", user, "-d", database, "-v", "ON_ERROR_STOP=1", "-tAc", query)
}

func graphOf(name string, modules map[string]contract.GraphModule) contract.Graph {
	return contract.Graph{ApiVersion: contract.APIVersion, Kind: contract.KindGraph, Name: name, Modules: modules}
}

// TestRevokeEndToEnd runs unbind and rebind for real (spec 0011 AC-6, spec 0014): removing a
// consumer leaves its login unable to sign in while its database and data stay, binding it
// again with the same names restores sign in with the data intact, removing the last consumer
// runs the revoke job with an empty MP_KEEP, and a login the provisioner did not make is never
// changed. It needs Docker with the Compose plugin; run it with go test -tags e2e.
func TestRevokeEndToEnd(t *testing.T) {
	s := newStack(t, loadCatalog(t, "../../catalog", "testdata/provision/catalog"))
	pg := contract.GraphModule{Module: "postgres"}
	client := bound("pg-client", map[string]string{"db": "postgres"})
	both := map[string]contract.GraphModule{"postgres": pg, "a": client, "b": client}
	onlyB := map[string]contract.GraphModule{"postgres": pg, "b": client}
	none := map[string]contract.GraphModule{"postgres": pg}

	s.apply(graphOf("revoke-e2e", both))
	s.up() // the revoke job has no module waiting for it; mp_jobs makes up wait for it (spec 0014)
	if got := s.state("mp_jobs"); got != "running" {
		t.Errorf("mp_jobs is %q after up, want running", got)
	}
	if got := s.state("postgres_revoke_server"); got != "exited" {
		t.Errorf("the revoke job is %q after up, want exited", got)
	}
	admin := s.secret("secrets/postgres/password")
	aPass, bPass := s.secret("secrets/a/uses_db_password"), s.secret("secrets/b/uses_db_password")
	if out, err := s.psql("a_db", aPass, "a_db", "create table notes (x int); insert into notes values (42)"); err != nil {
		t.Fatalf("a writes its data: %v\n%s", err, out)
	}
	if out, err := s.psql("postgres", admin, "postgres", "create role handmade login password 'handmade'"); err != nil {
		t.Fatalf("hand made role: %v\n%s", err, out)
	}
	canLogin := func(role string) string {
		out, err := s.psql("postgres", admin, "postgres", "select rolcanlogin from pg_roles where rolname = '"+role+"'")
		if err != nil {
			t.Fatalf("read %s: %v\n%s", role, err, out)
		}
		return strings.TrimSpace(out)
	}
	notes := func() string {
		out, err := s.psql("postgres", admin, "a_db", "select x from notes")
		if err != nil {
			t.Fatalf("read a's data as admin: %v\n%s", err, out)
		}
		return strings.TrimSpace(out)
	}

	// unbind a: its login is refused, its data stays, b and the hand made role are untouched
	compose := s.apply(graphOf("revoke-e2e", onlyB))
	if !strings.Contains(compose, "MP_KEEP: b_db\n") {
		t.Errorf("revoke job does not keep exactly b_db:\n%s", compose)
	}
	s.up()
	if _, err := s.psql("a_db", aPass, "a_db", "select 1"); err == nil {
		t.Error("a_db still signs in after a was removed")
	}
	if got := canLogin("a_db"); got != "f" {
		t.Errorf("a_db rolcanlogin = %q, want f", got)
	}
	if got := notes(); got != "42" {
		t.Errorf("a's data after unbind = %q, want 42", got)
	}
	if _, err := s.psql("b_db", bPass, "b_db", "select 1"); err != nil {
		t.Errorf("b_db cannot sign in after a was removed: %v", err)
	}
	if got := canLogin("handmade"); got != "t" {
		t.Errorf("the hand made role lost sign in: rolcanlogin = %q", got)
	}

	// bind a again with the same names: sign in comes back with the data intact
	s.apply(graphOf("revoke-e2e", both))
	s.up()
	if out, err := s.psql("a_db", aPass, "a_db", "select x from notes"); err != nil || strings.TrimSpace(out) != "42" {
		t.Errorf("a after rebind: %q, %v; want 42", out, err)
	}

	// remove the last consumers: the revoke job runs with an empty MP_KEEP
	compose = s.apply(graphOf("revoke-e2e", none))
	if !strings.Contains(compose, "MP_KEEP: \"\"\n") {
		t.Errorf("revoke job without bindings has no empty MP_KEEP:\n%s", compose)
	}
	s.up()
	for _, role := range []string{"a_db", "b_db"} {
		if got := canLogin(role); got != "f" {
			t.Errorf("%s rolcanlogin after the last unbind = %q, want f", role, got)
		}
	}
	if got := notes(); got != "42" {
		t.Errorf("a's data after the last unbind = %q, want 42", got)
	}
	if got := canLogin("handmade"); got != "t" {
		t.Errorf("the hand made role lost sign in: rolcanlogin = %q", got)
	}

	// a wrong admin password file: the revoke job fails, and so does up, on this up and the next
	// one, with mp_jobs already running (spec 0014, "Waiting"; AC-6)
	if got := s.state("mp_jobs"); got != "running" {
		t.Fatalf("mp_jobs is %q before the failing up, want running", got)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "secrets/postgres/password"), []byte("not-the-password"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"first", "second"} {
		out, err := s.tryUp()
		if err == nil {
			t.Fatalf("%s up with a failing revoke job exited 0:\n%s", run, out)
		}
		if !strings.Contains(out, "postgres_revoke_server") {
			t.Errorf("%s failing up does not name the revoke job:\n%s", run, out)
		}
		if strings.Contains(out, admin) {
			t.Errorf("%s failing up prints the admin password", run)
		}
	}
}

// TestWaitAnchorImages runs the wait anchor's command, with its hardening and no network, in
// every job image this release pins (spec 0014: a job image needs a sleep that takes
// infinity; no catalog check can see inside an image). It needs Docker; run it with go test
// -tags e2e.
func TestWaitAnchorImages(t *testing.T) {
	images := map[string]bool{}
	// the built in catalog with the paper platform over it, as the platform test of the checks loads it
	for _, k := range loadCatalog(t, "../../catalog").Capabilities {
		if k.Yields != nil && k.Yields.Job.Image != "" {
			images[k.Yields.Job.Image] = true
		}
	}
	if len(images) < 2 {
		t.Fatalf("job images %v, want at least the Debian and the Alpine one", slices.Sorted(maps.Keys(images)))
	}
	for _, image := range slices.Sorted(maps.Keys(images)) {
		t.Run(image, func(t *testing.T) {
			name := "mp-e2e-anchor-" + strings.NewReplacer("/", "-", ":", "-", ".", "-").Replace(image)
			t.Cleanup(func() {
				_ = exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "rm", "-f", name).Run()
			})
			if out, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--name", name,
				"--init", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
				"--user", "65532:65532", "--network", "none", "--entrypoint", "sleep", image, "infinity").CombinedOutput(); err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			// a sleep that does not take infinity exits at once with an error
			cmd := exec.CommandContext(t.Context(), "sh", "-c", "sleep 2; docker inspect -f '{{.State.Status}}' \"$1\"", "inspect", name)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "running" {
				logs, _ := exec.CommandContext(t.Context(), "docker", "logs", name).CombinedOutput()
				t.Errorf("%s does not idle: %s %v\n%s", image, out, err, logs)
			}
		})
	}
}

// TestExternalEndToEnd binds one unchanged consumer entry either to a provisioned database or to
// an external server (spec 0011 AC-1, AC-15): both renders give its service the same env names
// with the password only as a file path; setup.sh refuses to go on until the external's
// password file is placed and creates nothing in its place; then the consumer signs in to the
// external server with the delivered values. It needs Docker with the Compose plugin; run it
// with go test -tags e2e.
func TestExternalEndToEnd(t *testing.T) {
	catalog := loadCatalog(t, "../../catalog", "testdata/provision/catalog")
	const alias = "ext-db.example.test"

	internal := newStack(t, catalog)
	internalEnv := envOf(t, internal.render(graphOf("external-e2e", map[string]contract.GraphModule{
		"app": bound("pg-roaming", map[string]string{"db": "postgres"}), "postgres": {Module: "postgres"},
	})), "app")

	s := newStack(t, catalog)
	externalEnv := envOf(t, s.render(graphOf("external-e2e", map[string]contract.GraphModule{
		"app": bound("pg-roaming", map[string]string{"db": "ext"}),
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
