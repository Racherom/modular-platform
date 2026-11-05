//go:build e2e

package render_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/render"
)

// installAnswers answers setup.sh of the install case, one per line in the order it asks:
// domain, mode (default), note (left empty), origin (default from the domain), port, timeout,
// the unseal key, the license, then the admins step's user and password.
const installAnswers = "vault.example.test\n\n\n\n8300\n2m\nunseal-1\nlicense-1\nadmin\nhunter2\n"

// renderInstall renders the install case with graph text modules into dir, as a fresh download
// over the folder you keep: it writes only the rendered files and never touches secrets/ or
// data/.
func renderInstall(t *testing.T, dir, modules string) {
	t.Helper()
	catalog := loadCatalog(t, "testdata/install/catalog")
	g, p := contract.DecodeGraph([]byte("apiVersion: modular-platform/v1\nkind: Graph\nname: install\nmodules:\n" + modules))
	if p != nil {
		t.Fatal(p.Message)
	}
	plan := planner.Plan(g, catalog)
	if !plan.Ok {
		t.Fatalf("plan: %+v", plan.Problems)
	}
	out, err := render.Render(plan, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if f.Path == ".env" {
			if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
				continue // keep your answers, as the README says
			}
		}
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
}

// setup runs setup.sh in dir with stdin, so not on a terminal, and returns its output.
func setup(t *testing.T, dir, stdin string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "setup.sh")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// snapshot is every file under dir with its content and mtime.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = info.ModTime().Format(time.RFC3339Nano) + " " + info.Mode().String() + " " + string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func short(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:16]
}

// TestInstallSetupEndToEnd runs the rendered setup.sh with Docker (spec 0013, Install time;
// AC-10): answers piped in, steps in the module's own image, a second run that asks nothing and
// changes no file, a for_each step that runs once per new client, a stale client file that
// stays, and a wrong piped answer that stops setup.sh. Then the project starts with what
// setup.sh made, and no secret value shows in any service's env (AC-8).
func TestInstallSetupEndToEnd(t *testing.T) {
	dir := t.TempDir()
	two := "  alpha: {module: client}\n  beta: {module: client}\n  keeper: {module: keeper}\n"
	renderInstall(t, dir, two)

	out, err := setup(t, dir, installAnswers)
	if err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, out)
	}
	env := readFile(t, filepath.Join(dir, ".env"))
	for _, line := range []string{
		"MP_KEEPER__DOMAIN='vault.example.test'", "MP_KEEPER__MODE='strict'", "MP_KEEPER__NOTE=''",
		"MP_KEEPER__ORIGIN='https://vault.example.test'", "MP_KEEPER__PORT='8300'", "MP_KEEPER__TIMEOUT='2m'",
	} {
		if !strings.Contains(env, line+"\n") {
			t.Errorf(".env lacks %s:\n%s", line, env)
		}
	}
	secret := func(p string) string { return readFile(t, filepath.Join(dir, "secrets", p)) }
	for p, want := range map[string]string{
		"keeper/unseal-key":   "unseal-1",
		"keeper/license":      "license-1",
		"keeper/root":         "root of keeper.install on vault.example.test\n",
		"keeper/admins":       "admin:" + short("hunter2") + "\n",
		"keeper/sealed/alpha": "alpha sealed for alpha " + short(secret("alpha/uses_vault_client_secret")) + "\n",
		"keeper/sealed/beta":  "beta sealed for beta " + short(secret("beta/uses_vault_client_secret")) + "\n",
		"alpha/trusted-root":  "root of keeper.install on vault.example.test\n",
		"keeper/root-key":     secret("keeper/master-key")[:16],
		"beta/trusted-root":   "root of keeper.install on vault.example.test\n",
	} {
		if got := secret(p); got != want {
			t.Errorf("secrets/%s = %q, want %q", p, got, want)
		}
	}
	for p, mode := range map[string]fs.FileMode{"keeper/root": 0o644, "keeper/root-key": 0o600, "keeper/sealed": fs.ModeDir | 0o700, "keeper/sealed/alpha": 0o600} {
		if info, err := os.Stat(filepath.Join(dir, "secrets", p)); err != nil || info.Mode() != mode {
			t.Errorf("secrets/%s: mode %v, %v; want %v", p, info.Mode(), err, mode)
		}
	}

	before := snapshot(t, dir)
	out, err = setup(t, dir, "")
	if err != nil {
		t.Fatalf("second setup.sh: %v\n%s", err, out)
	}
	if strings.Contains(out, "running setup step") || strings.Contains(out, "added ") || strings.Contains(out, "created ") {
		t.Errorf("the second run changed something:\n%s", out)
	}
	if after := snapshot(t, dir); !maps.Equal(before, after) {
		t.Errorf("the second run changed a file's content, mode or mtime")
	}

	renderInstall(t, dir, two+"  gamma: {module: client}\n")
	out, err = setup(t, dir, "")
	if err != nil {
		t.Fatalf("setup.sh with a third client: %v\n%s", err, out)
	}
	if got := strings.Count(out, "running setup step seal of keeper"); got != 1 || !strings.Contains(out, "running setup step seal of keeper for gamma") {
		t.Errorf("want one seal run, for gamma:\n%s", out)
	}
	if !strings.HasPrefix(secret("keeper/sealed/gamma"), "gamma sealed for gamma ") {
		t.Errorf("secrets/keeper/sealed/gamma = %q", secret("keeper/sealed/gamma"))
	}

	renderInstall(t, dir, "  alpha: {module: client}\n  keeper: {module: keeper}\n")
	if out, err = setup(t, dir, ""); err != nil {
		t.Fatalf("setup.sh after unbinding beta: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets", "keeper", "sealed", "beta")); err != nil {
		t.Errorf("the file of the unbound client beta must stay: %v", err)
	}

	up(t, dir)
}

// up starts the project with what setup.sh made, waits for it, and checks that no service's env
// holds a secret value; it removes the project afterwards.
func up(t *testing.T, dir string) {
	t.Helper()
	project := "mp-e2e-install-" + strings.ToLower(filepath.Base(dir))
	compose := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-p", project}, args...)...)
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		if out, err := compose(context.WithoutCancel(t.Context()), "down", "--remove-orphans"); err != nil {
			t.Logf("down: %v\n%s", err, out)
		}
	})
	if out, err := compose(t.Context(), "up", "-d", "--wait", "--wait-timeout", "60"); err != nil {
		t.Fatalf("up: %v\n%s", err, out)
	}
	out, err := compose(t.Context(), "ps", "-q")
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := exec.CommandContext(t.Context(), "docker", append([]string{"inspect", "--format", "{{json .Config.Env}}"}, strings.Fields(string(out))...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"keeper/unseal-key", "keeper/license", "keeper/master-key", "alpha/uses_vault_client_secret"} {
		if value := readFile(t, filepath.Join(dir, "secrets", p)); bytes.Contains(inspect, []byte(value)) {
			t.Errorf("the value of secrets/%s shows in a service's env", p)
		}
	}
	if !bytes.Contains(inspect, []byte("KEEPER_ORIGIN=https://vault.example.test")) {
		t.Errorf("keeper lacks its asked origin in its env: %s", inspect)
	}
}

// TestInstallSetupStopsOnWrongPipedAnswer: with answers piped in, an answer breaking the value's
// rules stops setup.sh with a message naming the value, and creates nothing for it (AC-10).
func TestInstallSetupStopsOnWrongPipedAnswer(t *testing.T) {
	dir := t.TempDir()
	renderInstall(t, dir, "  keeper: {module: keeper}\n")
	out, err := setup(t, dir, "Not A Domain!\n")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !strings.Contains(out, "MP_KEEPER__DOMAIN") {
		t.Fatalf("want setup.sh to stop naming MP_KEEPER__DOMAIN; err %v\n%s", err, out)
	}
	if strings.Contains(readFile(t, filepath.Join(dir, ".env")), "MP_KEEPER__DOMAIN") {
		t.Error("a refused answer reached .env")
	}

	// a port out of range, piped, stops too
	out, err = setup(t, dir, "vault.example.test\n\n\n\n70000\n")
	if !errors.As(err, &exit) || !strings.Contains(out, "MP_KEEPER__PORT: the answer is above the maximum 65535") {
		t.Fatalf("want setup.sh to stop at the port; err %v\n%s", err, out)
	}
}

// TestInstallSetupWithoutClients: a for_each step over a provide with no binding never runs, and
// its folder still exists, empty, so the module's mount of it works (AC-10).
func TestInstallSetupWithoutClients(t *testing.T) {
	dir := t.TempDir()
	renderInstall(t, dir, "  keeper: {module: keeper}\n")
	out, err := setup(t, dir, installAnswers)
	if err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, out)
	}
	if strings.Contains(out, "seal of keeper") {
		t.Errorf("the seal step ran with no client:\n%s", out)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "secrets", "keeper", "sealed"))
	if err != nil || len(entries) != 0 {
		t.Errorf("secrets/keeper/sealed: %v, %v; want an empty folder", entries, err)
	}
}
