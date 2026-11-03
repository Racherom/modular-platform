package deploy_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

// applied applies the sample and waits for it, then returns the project folder.
func applied(t *testing.T, e *deploy.Engine, dir string) string {
	t.Helper()
	op, err := e.Apply(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, op.ID); done.Status != deploy.StatusSucceeded {
		t.Fatalf("apply: %+v", done)
	}
	return filepath.Join(dir, "mp-sample")
}

func TestRemove(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		deleteData  bool
		wantArgs    []string
		wantSecrets bool
	}{
		"keeps data":   {false, []string{"mp-sample", "down", "--remove-orphans"}, true},
		"deletes data": {true, []string{"mp-sample", "down", "--remove-orphans", "--volumes"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &fakeCompose{}
			e, dir, logs := newEngine(t, c, fakeDocker{})
			project := applied(t, e, dir)
			op, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample", DeleteData: tc.deleteData, Caller: "user-1"})
			if err != nil {
				t.Fatal(err)
			}
			if op.Action != deploy.ActionRemove || op.Status != deploy.StatusRunning {
				t.Errorf("started as %+v", op)
			}
			done := waitDone(t, e, op.ID)
			if done.Status != deploy.StatusSucceeded || done.Step != deploy.StepDone {
				t.Fatalf("finished as %+v", done)
			}
			c.mu.Lock()
			last := c.runs[len(c.runs)-1]
			c.mu.Unlock()
			if !slices.Equal(last, tc.wantArgs) {
				t.Errorf("compose ran %q, want %q", last, tc.wantArgs)
			}
			_, err = os.Lstat(filepath.Join(project, "secrets"))
			if exists := err == nil; exists != tc.wantSecrets {
				t.Errorf("secrets folder exists %v, want %v", exists, tc.wantSecrets)
			}
			// the rendered files stay: they show what last ran
			if _, err := os.Lstat(filepath.Join(project, "docker-compose.yml")); err != nil {
				t.Errorf("the compose file went: %v", err)
			}
			if !strings.Contains(logs.String(), `"action":"remove"`) || !strings.Contains(logs.String(), `"caller":"user-1"`) {
				t.Errorf("the remove is not logged with its caller: %s", logs)
			}
		})
	}
}

func TestRemoveRefusals(t *testing.T) {
	t.Parallel()
	t.Run("nothing there", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
		if _, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample"}); !errors.Is(err, deploy.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("containers without a folder", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{containers: []deploy.Container{{Status: "running"}}})
		op, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample"})
		if err != nil {
			t.Fatal(err)
		}
		if done := waitDone(t, e, op.ID); done.Status != deploy.StatusSucceeded {
			t.Errorf("finished as %+v", done)
		}
	})
	t.Run("docker down", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{err: errors.New("no socket")})
		if _, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample"}); !errors.Is(err, deploy.ErrDocker) {
			t.Errorf("err = %v, want ErrDocker", err)
		}
	})
	t.Run("bad project", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
		_, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "modular-platform"})
		if refused, ok := errors.AsType[*deploy.RefusedError](err); !ok || refused.Problems[0].Code != deploy.CodeInvalidProject {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("busy", func(t *testing.T) {
		t.Parallel()
		c := &fakeCompose{block: make(chan struct{})}
		e, dir, _ := newEngine(t, c, fakeDocker{})
		if err := os.MkdirAll(filepath.Join(dir, "mp-sample"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Apply(sampleRequest()); !errors.Is(err, deploy.ErrBusy) {
			t.Errorf("an apply during a remove: %v, want ErrBusy", err)
		}
		close(c.block)
	})
}

func TestRemoveFailure(t *testing.T) {
	t.Parallel()
	c := &fakeCompose{}
	e, dir, _ := newEngine(t, c, fakeDocker{})
	project := applied(t, e, dir)
	c.mu.Lock()
	c.fail = map[string]string{"down": "network in use"}
	c.mu.Unlock()
	op, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample", DeleteData: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, e, op.ID)
	if done.Status != deploy.StatusFailed || done.Step != deploy.StepDown || !strings.HasPrefix(done.Error, "down_failed") {
		t.Errorf("finished as %+v", done)
	}
	// down failed, so the data stays
	if _, err := os.Lstat(filepath.Join(project, "secrets")); err != nil {
		t.Errorf("the secrets went after a failed down: %v", err)
	}
}

// TestRemoveRefusesALinkedSecretsFolder: deleting data never leaves the project folder.
func TestRemoveRefusesALinkedSecretsFolder(t *testing.T) {
	t.Parallel()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{})
	project := filepath.Join(dir, "mp-sample")
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "secrets")); err != nil {
		t.Fatal(err)
	}
	op, err := e.Remove(t.Context(), deploy.RemoveRequest{Project: "mp-sample", DeleteData: true})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, op.ID); done.Status != deploy.StatusFailed {
		t.Errorf("finished as %+v", done)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a file outside the project went: %v", err)
	}
}

// TestFailureBeforeUpRestoresTheFolder: a failed pull leaves the files of the running version
// and its secrets as they were, and deletes only the secrets this apply created (AC-14).
func TestFailureBeforeUpRestoresTheFolder(t *testing.T) {
	t.Parallel()
	c := &fakeCompose{}
	e, dir, _ := newEngine(t, c, fakeDocker{})
	project := applied(t, e, dir)
	read := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(project, rel))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	compose, appEnv, password := read("docker-compose.yml"), read("config/app.env"), read("secrets/postgres/password")

	next := sampleRequest()
	next.Files[0].Data = []byte(strings.Replace(sampleCompose, "postgres:18.6", "postgres:18.7", 1))
	next.Files = append(next.Files, deploy.File{Path: "config/cache.env", Data: []byte("B=2\n")})
	next.Secrets = append(next.Secrets, deploy.Secret{Path: "secrets/cache/password", Kind: deploy.SecretGenerated})
	c.mu.Lock()
	c.fail = map[string]string{"pull": "manifest unknown"}
	c.mu.Unlock()
	op, err := e.Apply(next)
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, op.ID); done.Status != deploy.StatusFailed || done.Step != deploy.StepPull {
		t.Fatalf("finished as %+v", done)
	}
	if !bytes.Equal(read("docker-compose.yml"), compose) || !bytes.Equal(read("config/app.env"), appEnv) {
		t.Error("the files of the running version were not restored")
	}
	if !bytes.Equal(read("secrets/postgres/password"), password) {
		t.Error("an existing secret changed")
	}
	for _, rel := range []string{"config/cache.env", "secrets/cache/password"} {
		if _, err := os.Lstat(filepath.Join(project, rel)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s of the failed apply is still there: %v", rel, err)
		}
	}
}
