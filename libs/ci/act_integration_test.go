//go:build integration

package ci_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// actRunner runs one workflow of this repository with act in local Docker, at the version and
// runner image the Taskfile pins. Actions that would reach GitHub are replaced by the stubs in
// testdata through act's --local-repository mapping.
type actRunner struct {
	root, bin, runner, socket, home, dockerConfig string
}

// newActRunner builds act, pulls the runner image and prepares act's own home with an .actrc
// that names the runner, so act asks no first run question.
func newActRunner(t *testing.T, root string) *actRunner {
	t.Helper()
	needTools(t, "docker", "go")
	a := &actRunner{
		root:   root,
		runner: taskfileVar(t, root, "ACT_RUNNER_IMAGE"),
		home:   t.TempDir(),
	}
	a.bin = buildAct(t, root, taskfileVar(t, root, "ACT_VERSION"))
	run(t, root, nil, "docker", "pull", "--quiet", a.runner)
	a.socket = dockerSocket(t, root)
	if err := os.WriteFile(filepath.Join(a.home, ".actrc"), []byte("-P ubuntu-24.04="+a.runner+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.dockerConfig = os.Getenv("DOCKER_CONFIG")
	if a.dockerConfig == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		a.dockerConfig = filepath.Join(userHome, ".docker")
	}
	return a
}

// event writes an event payload to a temporary file and returns its path.
func (a *actRunner) event(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubFor returns the --local-repository mapping that sends every `uses: repo@<sha>` line of the
// workflow to the folder in testdata. It fails unless the workflow names the action at exactly
// one full SHA.
func (a *actRunner) stubFor(t *testing.T, workflow, repo, stubDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.root, ".github/workflows", workflow))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(regexp.QuoteMeta(repo) + `@([0-9a-f]{40})`)
	found := map[string]bool{}
	for _, m := range re.FindAllSubmatch(data, -1) {
		found[string(m[1])] = true
	}
	if len(found) != 1 {
		t.Fatalf("%s names %s at %d different full SHAs, want 1", workflow, repo, len(found))
	}
	for sha := range found {
		return repo + "@" + sha + "=" + filepath.Join(a.root, "libs/ci/testdata", stubDir)
	}
	return ""
}

// run runs act for the event and workflow (a path from the repository root) and returns its
// combined output and the error of the run, so a test can expect a failure.
func (a *actRunner) run(t *testing.T, eventName, workflow, event string, mappings []string, extra ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	args := []string{
		eventName, "-W", workflow, "-e", event,
		"-P", "ubuntu-24.04=" + a.runner, "--pull=false", "--rm",
		"--container-architecture", "linux/amd64",
		"--container-daemon-socket", a.socket,
	}
	for _, m := range mappings {
		args = append(args, "--local-repository", m)
	}
	cmd := exec.CommandContext(ctx, a.bin, append(args, extra...)...)
	cmd.Dir = a.root
	cmd.Env = actEnv(a.home, a.dockerConfig)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// buildAct builds act at version into a temporary folder with go install, in the real
// environment so the Go build and module caches are reused, and returns the binary's path.
func buildAct(t *testing.T, root, version string) string {
	t.Helper()
	bin := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "install", "github.com/nektos/act@"+version)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOBIN="+bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install act@%s: %v\n%s", version, err, out)
	}
	return filepath.Join(bin, "act")
}

// dockerSocket returns the socket path of the current Docker context, so rootless Docker and
// Docker Desktop work too.
func dockerSocket(t *testing.T, root string) string {
	t.Helper()
	host := strings.TrimSpace(run(t, root, nil, "docker", "context", "inspect",
		"--format", "{{.Endpoints.docker.Host}}"))
	socket, ok := strings.CutPrefix(host, "unix://")
	if !ok {
		t.Fatalf("the Docker context's endpoint %q is no unix socket", host)
	}
	return socket
}

// actEnv is the environment act runs in: its own HOME, the real Docker settings, and no check
// for a newer act (that check would reach GitHub).
func actEnv(home, dockerConfig string) []string {
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"DOCKER_CONFIG=" + dockerConfig,
		"ACT_DISABLE_VERSION_CHECK=1",
	}
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		env = append(env, "DOCKER_HOST="+host)
	}
	return env
}
