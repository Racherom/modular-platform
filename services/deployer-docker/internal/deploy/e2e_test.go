//go:build e2e

package deploy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/docker"
)

// e2eCatalog is a stack whose every image comes from the local registry REG: app binds a store
// that store's provisioner makes, so a provision job, a revoke job and mp_jobs run the job image
// REG/mp-e2e/job:v1 (spec 0031, build task 7). app is healthy only when /ok exists in its image.
var e2eCatalog = map[string]string{
	"capabilities/e2e-store.yaml": `apiVersion: modular-platform/v1
kind: Capability
name: e2e-store
description: A store and a login for one consumer, for the frozen images end to end test.
scheme: store
fields:
  host: {type: string, description: Host name.}
  name: {type: string, pattern: "[a-z_]+", description: Store name.}
  password: {type: secret, description: Password.}
`,
	"capabilities/e2e-store-admin.yaml": `apiVersion: modular-platform/v1
kind: Capability
name: e2e-store-admin
description: Admin access that makes a store per consumer, for the frozen images end to end test.
scheme: store
fields:
  host: {type: string, description: Host name.}
  admin_password: {type: secret, description: Admin password.}
yields:
  capability: e2e-store
  named: [name]
  login: name
  fields:
    host: {admin: host}
    name: named
    password: generate
  job:
    image: REG/mp-e2e/job:v1
    provision: "true"
    revoke: "true"
`,
	"modules/e2e-store/module.yaml": `apiVersion: modular-platform/v1
kind: Module
name: e2e-store
description: Provides the store admin, for the frozen images end to end test.
image: REG/mp-e2e/app:v1
entrypoint: ["sleep"]
command: ["infinity"]
provides:
  admin:
    capability: e2e-store-admin
    version: "1.0"
    port: 8080
    fields:
      host: {platform: host}
      admin_password: {secret: admin}
secrets:
  - name: admin
    env: ADMIN_FILE
    description: The admin password.
`,
	"modules/e2e-app/module.yaml": `apiVersion: modular-platform/v1
kind: Module
name: e2e-app
description: Healthy only when its image has /ok, for the frozen images end to end test.
image: REG/mp-e2e/app:v1
entrypoint: ["sleep"]
command: ["infinity"]
uses:
  db:
    capability: e2e-store
    env:
      host: DB_HOST
      name: DB_NAME
      password: DB_PASSWORD_FILE
healthcheck:
  test: ["CMD", "test", "-f", "/ok"]
  interval: 1s
  timeout: 2s
  retries: 3
`,
}

const e2eGraph = `apiVersion: modular-platform/v1
kind: Graph
name: frozen-e2e
modules:
  app:
    module: e2e-app
    uses:
      db: store
  store:
    module: e2e-store
`

func sh(t *testing.T, stdin, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestFrozenImagesEndToEnd runs spec 0031 on a real host with a local registry:3: an apply
// freezes every image, jobs and mp_jobs included (AC-1, AC-2, AC-14); the same graph applies
// again with the registry stopped (AC-3); and after a moved tag breaks an apply, the rollback to
// the current lock runs offline with the old image IDs (AC-4). It needs Docker with the Compose
// plugin; run it with go test -tags e2e.
func TestFrozenImagesEndToEnd(t *testing.T) {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	id := hex.EncodeToString(suffix)
	registry := "mp-e2e-registry-" + id
	// a fixed port: docker start would pick a new random one after the registry stops
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reg := l.Addr().String()
	_ = l.Close()
	sh(t, "", "docker", "run", "-d", "--name", registry, "-p", reg+":5000", "registry:3")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", registry).Run() })
	app, job := reg+"/mp-e2e/app:v1", reg+"/mp-e2e/job:v1"
	push := func(ref, dockerfile string) {
		sh(t, dockerfile, "docker", "build", "-q", "-t", ref, "-")
		for range 20 { // the registry may still be starting
			if err := exec.Command("docker", "push", "-q", ref).Run(); err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		sh(t, "", "docker", "push", "-q", ref)
	}
	push(app, "FROM busybox:1.37.0\nRUN touch /ok\n")
	push(job, "FROM busybox:1.37.0\nLABEL job=1\n")

	files := fstest.MapFS{}
	for path, text := range e2eCatalog {
		files[path] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(text, "REG", reg))}
	}
	catalog, problems := contract.LoadCatalog(files)
	if len(problems) > 0 {
		t.Fatalf("catalog: %v", problems)
	}
	templates, err := contract.LoadTemplates(files, catalog)
	if err != nil {
		t.Fatal(err)
	}
	g, problem := contract.DecodeGraph([]byte(e2eGraph))
	if problem != nil {
		t.Fatalf("graph: %+v", problem)
	}
	result := check.Graph(g, catalog)
	if !result.Ok {
		t.Fatalf("check: %+v", result.Problems)
	}
	const stack = "01960000-0000-7000-8000-0000000000e2"

	dir := t.TempDir()
	dockerEngine, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dockerEngine.Close() })
	// the engine outlives t.Context, which ends before the cleanups run; registered first, this
	// cancel runs after every other cleanup
	ectx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := deploy.New(ectx, deploy.Config{
		Dir: dir, UID: os.Getuid(), GID: os.Getgid(), PullTimeout: 2 * time.Minute, WaitTimeout: 20 * time.Second,
		ResolveTimeout: 30 * time.Second, ReservedPorts: []int{80, 443},
	}, hostCompose{}, dockerEngine, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	project := "mp-frozen-e2e"
	t.Cleanup(func() {
		if _, err := e.Remove(ectx, deploy.RemoveRequest{Project: project, DeleteData: true}); err == nil {
			e.Wait()
		}
		// the pulled images by ID: the remove frees only the keep tags
		out, _ := exec.Command("docker", "image", "ls", "-q", "--filter", "reference="+reg+"/mp-e2e/*").Output()
		for img := range strings.FieldsSeq(string(out)) {
			_ = exec.Command("docker", "rmi", "-f", img).Run()
		}
	})

	// apply plays api's part: render, freeze against the current lock, render with the lock, apply
	apply := func(update []string) (deploy.Operation, []contract.FrozenImage) {
		t.Helper()
		plain, err := render.Managed(result, catalog, templates, stack, nil)
		if err != nil {
			t.Fatal(err)
		}
		var refs []string
		for _, r := range plain.References {
			refs = append(refs, r.Reference)
		}
		imgs, err := e.Images(t.Context(), project)
		if err != nil {
			t.Fatal(err)
		}
		var current []contract.LockedImage
		if imgs.Current != nil {
			current = imgs.Current.Lock.Images
		}
		resolved := map[string]string{}
		if need := contract.ToResolve(refs, current, update); len(need) > 0 {
			res, err := e.Resolve(t.Context(), need)
			if err != nil || len(res.Unresolved) > 0 {
				t.Fatalf("resolve %v: %+v, %v", need, res.Unresolved, err)
			}
			for _, p := range res.Images {
				resolved[p.Reference] = p.Digest
			}
		}
		frozen, err := contract.Freeze(refs, current, resolved, update)
		if err != nil {
			t.Fatal(err)
		}
		m, err := render.Managed(result, catalog, templates, stack, contract.Pins(frozen))
		if err != nil {
			t.Fatal(err)
		}
		req := deploy.ApplyRequest{Project: project, Stack: stack, Caller: "e2e"}
		for _, f := range m.Files {
			req.Files = append(req.Files, deploy.File{Path: f.Path, Data: f.Data})
		}
		for _, s := range m.Secrets {
			req.Secrets = append(req.Secrets, deploy.Secret{Path: s.Path, Kind: deploy.SecretKind(s.Kind)})
		}
		op, err := e.Apply(req)
		if err != nil {
			t.Fatal(err)
		}
		return waitDone(t, e, op.ID), frozen
	}
	runningIDs := func() map[string]string {
		t.Helper()
		out := sh(t, "", "docker", "ps", "-a", "--filter", "label=com.docker.compose.project="+project,
			"--format", `{{.Label "com.docker.compose.service"}} {{.ID}}`)
		ids := map[string]string{}
		for line := range strings.SplitSeq(out, "\n") {
			svc, cid, _ := strings.Cut(line, " ")
			ids[svc] = sh(t, "", "docker", "inspect", "-f", "{{.Image}}", cid)
		}
		return ids
	}
	stackDir := filepath.Join(dir, project)
	readLock := func(name string) (contract.ImageLock, []byte) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(stackDir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		l, err := contract.DecodeImageLock(data)
		if err != nil {
			t.Fatal(err)
		}
		return l, data
	}

	// AC-1, AC-2, AC-14: every image frozen, jobs and mp_jobs included, image IDs match
	if op, _ := apply(nil); op.Status != deploy.StatusSucceeded {
		t.Fatalf("first apply: %+v", op)
	}
	compose, _ := os.ReadFile(filepath.Join(stackDir, "docker-compose.yml"))
	for line := range strings.Lines(string(compose)) {
		if strings.HasPrefix(strings.TrimSpace(line), "image:") && !strings.Contains(line, "@sha256:") {
			t.Errorf("not frozen: %s", line)
		}
	}
	lock, _ := readLock("images.lock")
	if got := len(lock.Images); got != 2 {
		t.Fatalf("lock has %d entries, want app and job: %+v", got, lock.Images)
	}
	ids := runningIDs()
	for svc, imgID := range ids {
		ref := app
		if strings.Contains(svc, "_provision_") || strings.Contains(svc, "_revoke_") || svc == "mp_jobs" {
			ref = job
		}
		if entry, ok := lock.Find(ref); !ok || entry.ImageID != imgID {
			t.Errorf("service %s runs %s, lock has %+v", svc, imgID, entry)
		}
	}
	if _, ok := ids["mp_jobs"]; !ok {
		t.Errorf("no mp_jobs among %v", ids)
	}
	keep := sh(t, "", "docker", "image", "ls", "-q", "localhost/mp-keep/"+project)
	if len(strings.Fields(keep)) != 2 {
		t.Errorf("keep tags: %q", keep)
	}

	// AC-3: the same graph again with the registry stopped
	sh(t, "", "docker", "stop", registry)
	if op, frozen := apply(nil); op.Status != deploy.StatusSucceeded || !allFrozen(frozen) {
		t.Fatalf("offline apply: %+v %+v", op, frozen)
	}
	before := runningIDs()
	_, lockBefore := readLock("images.lock")

	// AC-4: the tag moves to a broken image, you take it, up fails, the rollback runs offline
	sh(t, "", "docker", "start", registry)
	push(app, "FROM busybox:1.37.0\nLABEL broken=1\n")
	op, frozen := apply([]string{app})
	if op.Status != deploy.StatusFailed || op.Step != deploy.StepUp {
		t.Fatalf("broken apply: %+v", op)
	}
	if i := slices.IndexFunc(frozen, func(f contract.FrozenImage) bool { return f.Reference == app }); frozen[i].Change != contract.ChangeUpdated {
		t.Errorf("broken apply froze %+v", frozen[i])
	}
	if _, after := readLock("images.lock"); string(after) != string(lockBefore) {
		t.Error("the failed apply changed images.lock")
	}
	sh(t, "", "docker", "stop", registry)
	if op, frozen := apply(nil); op.Status != deploy.StatusSucceeded || !allFrozen(frozen) {
		t.Fatalf("offline rollback: %+v %+v", op, frozen)
	}
	after := runningIDs()
	for svc, imgID := range before {
		if after[svc] != imgID {
			t.Errorf("after the rollback %s runs %s, want %s", svc, after[svc], imgID)
		}
	}
}

func allFrozen(frozen []contract.FrozenImage) bool {
	return !slices.ContainsFunc(frozen, func(f contract.FrozenImage) bool { return f.Change != contract.ChangeFrozen })
}

// hostCompose is docker.Compose with the host's own env: the adapter pins HOME=/root, which
// only holds the compose plugin inside the deployer image.
type hostCompose struct{}

func (hostCompose) Run(ctx context.Context, dir, project string, args ...string) ([]byte, error) {
	base := []string{
		"compose", "--project-name", project, "--progress", "plain",
		"--project-directory", dir, "--file", filepath.Join(dir, "docker-compose.yml"),
		"--env-file", filepath.Join(dir, ".env"), "--env-file", filepath.Join(dir, "deploy.env"),
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		base = base[:5]
	}
	cmd := exec.CommandContext(ctx, "docker", append(base, args...)...)
	return cmd.CombinedOutput()
}
