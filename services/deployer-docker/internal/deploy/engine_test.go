package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

// fakeCompose records each run and answers from fail: a subcommand found there fails with its
// output. block, when set, holds every run until it is closed.
type fakeCompose struct {
	mu    sync.Mutex
	runs  [][]string
	fail  map[string]string
	block chan struct{}
}

func (c *fakeCompose) Run(_ context.Context, _, project string, args ...string) ([]byte, error) {
	if c.block != nil {
		<-c.block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs = append(c.runs, append([]string{project}, args...))
	if out, ok := c.fail[args[0]]; ok {
		return []byte(out), errors.New("exit status 1")
	}
	return []byte("ok"), nil
}

func (c *fakeCompose) subcommands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, r := range c.runs {
		out = append(out, r[1])
	}
	return out
}

type fakeDocker struct {
	containers []deploy.Container
	err        error
	// streams hands one stream to each Events call; nil gives a stream that stays quiet
	streams chan fakeStream
}

// fakeStream is one event stream; since gets the replay point it was opened from.
type fakeStream struct {
	since  chan time.Time
	events chan deploy.ExitEvent
	errs   chan error
}

func (d fakeDocker) Containers(context.Context, string) ([]deploy.Container, error) {
	return d.containers, d.err
}

func (d fakeDocker) Events(ctx context.Context, since time.Time) (<-chan deploy.ExitEvent, <-chan error) {
	quiet := make(chan error, 1)
	if d.streams == nil {
		return nil, quiet
	}
	select {
	case s := <-d.streams:
		s.since <- since
		return s.events, s.errs
	case <-ctx.Done():
		quiet <- ctx.Err()
		return nil, quiet
	}
}

func newEngine(t *testing.T, c *fakeCompose, d fakeDocker) (*deploy.Engine, string, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var logs bytes.Buffer
	e := deploy.New(t.Context(), deploy.Config{
		Dir: dir, UID: os.Getuid(), GID: os.Getgid(), PullTimeout: time.Minute, WaitTimeout: 30 * time.Second,
		ReservedPorts: []int{80, 443},
	}, c, d, slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(e.Wait)
	return e, dir, &logs
}

// sampleCompose is a project in the shape the managed render emits.
const sampleCompose = `name: mp-sample
secrets:
  app_uses_postgresql_password:
    file: ./secrets/app/uses_postgresql_password
  postgres_password:
    file: ./secrets/postgres/password
services:
  app:
    image: traefik/whoami:v1.12.0
    labels:
      io.github.racherom.modular-platform.stack: 01960000-0000-7000-8000-000000000001
      io.github.racherom.modular-platform.graph: sample
      io.github.racherom.modular-platform.instance: app
      io.github.racherom.modular-platform.module: whoami
    cap_drop: [ALL]
    restart: unless-stopped
    security_opt: ["no-new-privileges:true"]
    secrets:
    - source: app_uses_postgresql_password
    user: ${MP_UID}:${MP_GID}
  postgres:
    image: postgres:18.6
    labels:
      io.github.racherom.modular-platform.stack: 01960000-0000-7000-8000-000000000001
      io.github.racherom.modular-platform.graph: sample
      io.github.racherom.modular-platform.instance: postgres
      io.github.racherom.modular-platform.module: postgres
    cap_drop: [ALL]
    restart: unless-stopped
    security_opt: ["no-new-privileges:true"]
    secrets:
    - source: postgres_password
    volumes:
    - source: ./data/postgres/data
      target: /var/lib/postgresql
      type: bind
`

func sampleRequest() deploy.ApplyRequest {
	return deploy.ApplyRequest{
		Project: "mp-sample", Stack: "01960000-0000-7000-8000-000000000001", Caller: "user-1",
		Files: []deploy.File{
			{Path: "docker-compose.yml", Data: []byte(sampleCompose)},
			{Path: ".env", Data: []byte("# env\n")},
			{Path: "config/app.env", Data: []byte("A=1\n")},
			{Path: "config/app/routes.conf", Data: []byte("route /\n")},
		},
		Secrets: []deploy.Secret{
			{Path: "secrets/postgres/password", Kind: deploy.SecretGenerated},
			{Path: "secrets/app/uses_postgresql_password", Kind: deploy.SecretPlaced},
		},
		Placed: []deploy.Placed{{Path: "secrets/app/uses_postgresql_password", Data: []byte("hunter2-placed")}},
	}
}

// waitDone polls until the operation finishes.
func waitDone(t *testing.T, e *deploy.Engine, id string) deploy.Operation {
	t.Helper()
	e.Wait()
	op, ok := e.Operation(id)
	if !ok {
		t.Fatalf("operation %s is gone", id)
	}
	return op
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestApplySucceeds(t *testing.T) {
	t.Parallel()
	c := &fakeCompose{}
	e, dir, logs := newEngine(t, c, fakeDocker{})
	op, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != deploy.StatusRunning || op.Step != deploy.StepPrepare || op.Action != deploy.ActionApply {
		t.Errorf("started as %+v", op)
	}
	done := waitDone(t, e, op.ID)
	if done.Status != deploy.StatusSucceeded || done.Step != deploy.StepDone || done.FinishedAt.IsZero() {
		t.Fatalf("finished as %+v", done)
	}
	if got := c.subcommands(); !slices.Equal(got, []string{"pull", "up"}) {
		t.Errorf("compose runs %q, want pull then up", got)
	}
	project := filepath.Join(dir, "mp-sample")
	for path, mode := range map[string]fs.FileMode{
		"docker-compose.yml":                   0o644,
		"config/app.env":                       0o644,
		"config/app":                           0o755 | fs.ModeDir,
		"config/app/routes.conf":               0o644,
		"deploy.env":                           0o644,
		"secrets":                              0o700 | fs.ModeDir,
		"secrets/postgres":                     0o700 | fs.ModeDir,
		"secrets/postgres/password":            0o600,
		"secrets/app/uses_postgresql_password": 0o600,
		"data":                                 0o700 | fs.ModeDir,
		"data/postgres/data":                   0o700 | fs.ModeDir,
	} {
		info, err := os.Lstat(filepath.Join(project, path))
		if err != nil {
			t.Error(err)
			continue
		}
		if info.Mode() != mode {
			t.Errorf("%s: mode %v, want %v", path, info.Mode(), mode)
		}
	}
	generated, _ := os.ReadFile(filepath.Join(project, "secrets/postgres/password"))
	if !hex64.Match(generated) {
		t.Errorf("generated secret %q is not 32 bytes as hex", generated)
	}
	placed, _ := os.ReadFile(filepath.Join(project, "secrets/app/uses_postgresql_password"))
	if string(placed) != "hunter2-placed" {
		t.Errorf("placed secret = %q", placed)
	}
	deployEnv, _ := os.ReadFile(filepath.Join(project, "deploy.env"))
	if !strings.Contains(string(deployEnv), "MP_UID=") || !strings.Contains(string(deployEnv), "MP_GID=") {
		t.Errorf("deploy.env = %q", deployEnv)
	}
	for _, secret := range [][]byte{generated, placed} {
		if bytes.Contains(logs.Bytes(), secret) {
			t.Errorf("a secret value reached the log")
		}
	}
	if !strings.Contains(logs.String(), `"caller":"user-1"`) {
		t.Errorf("the caller is not logged: %s", logs)
	}
}

func TestApplyNeverOverwritesSecrets(t *testing.T) {
	t.Parallel()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	first, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, e, first.ID)
	path := filepath.Join(dir, "mp-sample", "secrets/postgres/password")
	before, _ := os.ReadFile(path)

	again := sampleRequest()
	again.Placed = []deploy.Placed{{Path: "secrets/app/uses_postgresql_password", Data: []byte("other")}}
	second, err := e.Apply(again)
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, second.ID); done.Status != deploy.StatusSucceeded {
		t.Fatalf("second apply: %+v", done)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a generated secret was overwritten")
	}
	placed, _ := os.ReadFile(filepath.Join(dir, "mp-sample", "secrets/app/uses_postgresql_password"))
	if string(placed) != "hunter2-placed" {
		t.Errorf("a placed secret was overwritten without replace: %q", placed)
	}

	replace := sampleRequest()
	replace.Placed = []deploy.Placed{{Path: "secrets/app/uses_postgresql_password", Data: []byte("new-value"), Replace: true}}
	third, err := e.Apply(replace)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, e, third.ID)
	placed, _ = os.ReadFile(filepath.Join(dir, "mp-sample", "secrets/app/uses_postgresql_password"))
	if string(placed) != "new-value" {
		t.Errorf("replace did not replace: %q", placed)
	}
}

func TestApplyFailures(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fail     map[string]string
		wantStep deploy.Step
		wantCode string
	}{
		"pull": {map[string]string{"pull": "no such image"}, deploy.StepPull, "pull_failed"},
		"up":   {map[string]string{"up": "container mp-sample-app-1 is unhealthy"}, deploy.StepUp, "up_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, _, _ := newEngine(t, &fakeCompose{fail: tc.fail}, fakeDocker{})
			op, err := e.Apply(sampleRequest())
			if err != nil {
				t.Fatal(err)
			}
			done := waitDone(t, e, op.ID)
			if done.Status != deploy.StatusFailed || done.Step != tc.wantStep || !strings.HasPrefix(done.Error, tc.wantCode) {
				t.Errorf("finished as %+v", done)
			}
			for _, out := range tc.fail {
				if !strings.Contains(done.Error, out) {
					t.Errorf("error %q lacks the compose output %q", done.Error, out)
				}
			}
		})
	}
}

func TestApplyKeepsOnlyTheTailOfTheOutput(t *testing.T) {
	t.Parallel()
	out := strings.Repeat("a", 20_000) + "THE END"
	e, _, _ := newEngine(t, &fakeCompose{fail: map[string]string{"up": out}}, fakeDocker{})
	op, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, e, op.ID)
	if len(done.Error) > 16*1024+200 || !strings.HasSuffix(done.Error, "THE END") {
		t.Errorf("error is %d bytes, ends %q", len(done.Error), done.Error[len(done.Error)-10:])
	}
}

func TestApplyRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		change func(*deploy.ApplyRequest)
		want   string
	}{
		"project without mp-":     {func(r *deploy.ApplyRequest) { r.Project = "sample" }, deploy.CodeInvalidProject},
		"the platform's project":  {func(r *deploy.ApplyRequest) { r.Project = "modular-platform" }, deploy.CodeInvalidProject},
		"file outside the layout": {func(r *deploy.ApplyRequest) { r.Files[0].Path = "../escape.yml" }, deploy.CodeInvalidPath},
		"config two deep":         {func(r *deploy.ApplyRequest) { r.Files[3].Path = "config/a/b/c" }, deploy.CodeInvalidPath},
		"config dot name":         {func(r *deploy.ApplyRequest) { r.Files[3].Path = "config/a/.." }, deploy.CodeInvalidPath},
		"config of no instance":   {func(r *deploy.ApplyRequest) { r.Files[3].Path = "config/A/x" }, deploy.CodeInvalidPath},
		"secret outside secrets/": {func(r *deploy.ApplyRequest) { r.Secrets[0].Path = "secrets/../x" }, deploy.CodeInvalidPath},
		"empty placed":            {func(r *deploy.ApplyRequest) { r.Placed[0].Data = nil }, deploy.CodePlacedInvalid},
		"placed with NUL":         {func(r *deploy.ApplyRequest) { r.Placed[0].Data = []byte("a\x00b") }, deploy.CodePlacedInvalid},
		"placed too long":         {func(r *deploy.ApplyRequest) { r.Placed[0].Data = bytes.Repeat([]byte("a"), 4097) }, deploy.CodePlacedInvalid},
		"placed missing":          {func(r *deploy.ApplyRequest) { r.Placed = nil }, deploy.CodePlacedMissing},
		"placed over a generated": {func(r *deploy.ApplyRequest) { r.Placed[0].Path = "secrets/postgres/password" }, deploy.CodeInvalidPath},
		"no compose file":         {func(r *deploy.ApplyRequest) { r.Files = r.Files[1:] }, deploy.CodeInvalidProject},
		"a broken compose file":   {func(r *deploy.ApplyRequest) { r.Files[0].Data = []byte("services: [") }, deploy.CodeInvalidProject},
		"another project's name":  {func(r *deploy.ApplyRequest) { r.Project = "mp-other" }, deploy.CodeInvalidProject},
		"another stack":           {func(r *deploy.ApplyRequest) { r.Stack = "01960000-0000-7000-8000-000000000002" }, deploy.CodeInvalidProject},
		"privileged":              {composeEdit("    restart: unless-stopped\n", "    restart: unless-stopped\n    privileged: true\n"), deploy.CodeHostAccess},
		"the docker socket":       {composeEdit("    restart: unless-stopped\n", "    restart: unless-stopped\n    volumes: [/var/run/docker.sock:/var/run/docker.sock]\n"), deploy.CodeHostAccess},
		"a reserved port":         {composeEdit("    restart: unless-stopped\n", "    restart: unless-stopped\n    ports: [\"443:8080\"]\n"), deploy.CodeHostAccess},
		"a Linux capability":      {composeEdit("    restart: unless-stopped\n", "    restart: unless-stopped\n    cap_add: [SYS_ADMIN]\n"), deploy.CodeHostAccess},
		"no cap_drop":             {composeEdit("    cap_drop: [ALL]\n", ""), deploy.CodeHostAccess},
		"the platform's network": {func(r *deploy.ApplyRequest) {
			r.Files[0].Data = append(r.Files[0].Data, "networks:\n  default:\n    external: true\n    name: modular-platform_internal\n"...)
		}, deploy.CodeHostAccess},
		"another engine in .env": {func(r *deploy.ApplyRequest) { r.Files[1].Data = []byte("DOCKER_HOST=tcp://elsewhere:2375\n") }, deploy.CodeHostAccess},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &fakeCompose{}
			e, dir, _ := newEngine(t, c, fakeDocker{})
			req := sampleRequest()
			tc.change(&req)
			_, err := e.Apply(req)
			refused, ok := errors.AsType[*deploy.RefusedError](err)
			if !ok {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if !slices.ContainsFunc(refused.Problems, func(p deploy.Problem) bool { return p.Code == tc.want }) {
				t.Errorf("problems %+v lack %s", refused.Problems, tc.want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 || len(c.subcommands()) != 0 {
				t.Error("a refused apply touched the folder or ran compose")
			}
		})
	}
}

// composeEdit changes the sample's docker-compose.yml.
func composeEdit(old, replacement string) func(*deploy.ApplyRequest) {
	return func(r *deploy.ApplyRequest) {
		r.Files[0].Data = []byte(strings.Replace(string(r.Files[0].Data), old, replacement, 1))
	}
}

func TestApplyRefusesASymlinkedSecretFolder(t *testing.T) {
	t.Parallel()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	outside := t.TempDir()
	project := filepath.Join(dir, "mp-sample")
	if err := os.MkdirAll(filepath.Join(project, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "secrets", "postgres")); err != nil {
		t.Fatal(err)
	}
	op, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, op.ID); done.Status != deploy.StatusFailed || done.Step != deploy.StepPrepare {
		t.Errorf("finished as %+v", done)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Error("a secret was written through the link")
	}
}

func TestApplyBusy(t *testing.T) {
	t.Parallel()
	c := &fakeCompose{block: make(chan struct{})}
	e, _, _ := newEngine(t, c, fakeDocker{})
	first, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(sampleRequest()); !errors.Is(err, deploy.ErrBusy) {
		t.Errorf("second apply of the project: %v, want ErrBusy", err)
	}
	other := sampleRequest()
	other.Project = "mp-other"
	other.Files[0].Data = []byte(strings.NewReplacer("name: mp-sample", "name: mp-other", "graph: sample", "graph: other").Replace(sampleCompose))
	if _, err := e.Apply(other); err != nil {
		t.Errorf("another project must run in parallel: %v", err)
	}
	close(c.block)
	waitDone(t, e, first.ID)
	if _, err := e.Apply(sampleRequest()); err != nil {
		t.Errorf("after the first finished: %v", err)
	}
}

func TestOperationUnknown(t *testing.T) {
	t.Parallel()
	e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	if _, ok := e.Operation("nope"); ok {
		t.Error("an unknown id was found")
	}
	if len(e.BootID()) != 22 {
		t.Errorf("boot id %q is not 16 bytes in base64url", e.BootID())
	}
}

func TestState(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	labels := func(i string) map[string]string {
		return map[string]string{
			"io.github.racherom.modular-platform.instance": i,
			"io.github.racherom.modular-platform.module":   "m-" + i,
		}
	}
	d := fakeDocker{containers: []deploy.Container{
		{Labels: labels("g"), Status: "exited", ExitCode: 137, FinishedAt: started.Add(time.Minute)},
		{Labels: labels("a"), Status: "created"},
		{Labels: labels("c"), Status: "running", Health: "healthy", RestartCount: 2, StartedAt: started},
		{Labels: labels("b"), Status: "running", Health: "starting"},
		{Labels: labels("d"), Status: "running"},
		{Labels: labels("e"), Status: "running", Health: "unhealthy"},
		{Labels: labels("f"), Status: "restarting"},
		{Labels: labels("h"), Status: "paused"},
		{Labels: map[string]string{"io.github.racherom.modular-platform.job": "wait"}, Status: "running"},
	}}
	e, _, _ := newEngine(t, &fakeCompose{}, d)
	present, services, err := e.State(t.Context(), "mp-sample")
	if err != nil || !present {
		t.Fatalf("present %v, err %v", present, err)
	}
	want := map[string]deploy.ModuleState{
		"a": deploy.StatePending, "b": deploy.StateStarting, "c": deploy.StateRunning, "d": deploy.StateRunning,
		"e": deploy.StateUnhealthy, "f": deploy.StateRestarting, "g": deploy.StateExited, "h": deploy.StatePaused,
	}
	var order []string
	for _, s := range services {
		order = append(order, s.Instance)
		if s.State != want[s.Instance] {
			t.Errorf("%s: %s, want %s", s.Instance, s.State, want[s.Instance])
		}
		if s.Module != "m-"+s.Instance {
			t.Errorf("%s: module %q", s.Instance, s.Module)
		}
		switch s.Instance {
		case "g":
			if s.ExitCode == nil || *s.ExitCode != 137 || !s.Since.Equal(started.Add(time.Minute)) {
				t.Errorf("exited: code %v since %v", s.ExitCode, s.Since)
			}
		case "c":
			if s.RestartCount != 2 || !s.Since.Equal(started) || s.ExitCode != nil {
				t.Errorf("running: %+v", s)
			}
		}
	}
	if !slices.IsSorted(order) {
		t.Errorf("not sorted by instance: %q", order)
	}
	if len(services) != len(want) {
		t.Errorf("%d rows, want %d: the wait anchor mp_jobs is no row", len(services), len(want))
	}
	anchorOnly, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{containers: []deploy.Container{
		{Labels: map[string]string{"io.github.racherom.modular-platform.job": "wait"}, Status: "running"},
	}})
	if present, rows, _ := anchorOnly.State(t.Context(), "mp-anchor"); !present || len(rows) != 0 {
		t.Errorf("only mp_jobs left: present %v, rows %+v; want present with no row", present, rows)
	}

	empty, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	if present, _, _ := empty.State(t.Context(), "mp-none"); present {
		t.Error("a project with no containers and no folder is present")
	}
	broken, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{err: io.ErrUnexpectedEOF})
	if _, _, err := broken.State(t.Context(), "mp-x"); !errors.Is(err, deploy.ErrDocker) {
		t.Errorf("err = %v, want ErrDocker", err)
	}
}

func TestSecretsListsPathsOnly(t *testing.T) {
	t.Parallel()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	if got, err := e.Secrets("mp-sample"); err != nil || len(got) != 0 {
		t.Fatalf("before any apply: %q, %v", got, err)
	}
	op, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, e, op.ID)
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "mp-sample", "secrets", "app", "link")); err != nil {
		t.Fatal(err)
	}
	got, err := e.Secrets("mp-sample")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"secrets/app/uses_postgresql_password", "secrets/postgres/password"}
	if !slices.Equal(got, want) {
		t.Errorf("secrets = %q, want %q", got, want)
	}
}
