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

// The lines this test looks for in act's plain output. The stub prints the first; release.yml's
// token step prints the second; act v0.2.92 prints the third when the release-please job ends
// well. A newer act may word its own line differently, so a bump of ACT_VERSION checks it.
const (
	stubRan        = "release-please stub ran"
	stubRanToken   = "release-please stub ran (token set: yes)"
	tokenMissing   = "RELEASE_PLEASE_TOKEN is not set"
	tokenStep      = "Release token is set"
	releaseJobDone = "[Release/release-please] 🏁  Job succeeded"
)

// TestReleaseGateAct covers spec 0030 AC-11 without GitHub: it runs the real release.yml with
// act in local Docker, release-please replaced by the stub in testdata. With RELEASES_ENABLED
// unset every job is skipped and the run passes; with it true and no RELEASE_PLEASE_TOKEN the
// token step fails before the stub; with both the stub runs with a token and reports no release,
// so the images and managed zip jobs stay skipped.
func TestReleaseGateAct(t *testing.T) {
	root := repoRoot(t)
	needTools(t, "docker", "go")
	actVersion := taskfileVar(t, root, "ACT_VERSION")
	runner := taskfileVar(t, root, "ACT_RUNNER_IMAGE")

	// the one SHA pinned release-please uses: line, mapped to the stub
	workflow, err := os.ReadFile(filepath.Join(root, ".github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	shas := regexp.MustCompile(`release-please-action@([0-9a-f]{40})`).FindAllSubmatch(workflow, -1)
	if len(shas) != 1 {
		t.Fatalf("release.yml names release-please-action at a full SHA %d times, want 1", len(shas))
	}
	stub := filepath.Join(root, "libs/ci/testdata/release-please-stub")
	mapping := "googleapis/release-please-action@" + string(shas[0][1]) + "=" + stub

	act := buildAct(t, root, actVersion)
	run(t, root, nil, "docker", "pull", "--quiet", runner)
	socket := dockerSocket(t, root)

	// act's own home: an .actrc with the runner, so it asks no first run question
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".actrc"), []byte("-P ubuntu-24.04="+runner+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{"ref":"refs/heads/main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerConfig := os.Getenv("DOCKER_CONFIG")
	if dockerConfig == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		dockerConfig = filepath.Join(userHome, ".docker")
	}

	tests := []struct {
		name     string
		args     []string
		wantFail bool
		want     []string
		notWant  []string
	}{
		{
			name:    "variable unset skips every job",
			notWant: []string{tokenStep, stubRan},
		},
		{
			name:     "variable true without the secret fails before release-please",
			args:     []string{"--var", "RELEASES_ENABLED=true"},
			wantFail: true,
			want:     []string{tokenMissing},
			notWant:  []string{stubRan},
		},
		{
			name: "variable true with the secret runs release-please only",
			args: []string{"--var", "RELEASES_ENABLED=true", "-s", "RELEASE_PLEASE_TOKEN=dummy"},
			want: []string{stubRanToken, releaseJobDone},
			// release_created is empty, so neither later job starts
			notWant: []string{"[Release/Release image", "[Release/Managed zip on the release"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()
			args := append([]string{
				"push", "-W", ".github/workflows/release.yml", "-e", event,
				"-P", "ubuntu-24.04=" + runner, "--pull=false", "--rm",
				"--container-architecture", "linux/amd64",
				"--container-daemon-socket", socket,
				"--local-repository", mapping,
			}, tt.args...)
			cmd := exec.CommandContext(ctx, act, args...)
			cmd.Dir = root
			cmd.Env = actEnv(home, dockerConfig)
			out, err := cmd.CombinedOutput()
			output := string(out)
			switch {
			case tt.wantFail && err == nil:
				t.Fatalf("act passed, want a failure\n%s", output)
			case !tt.wantFail && err != nil:
				t.Fatalf("act: %v\n%s", err, output)
			}
			for _, s := range tt.want {
				if !strings.Contains(output, s) {
					t.Errorf("output lacks %q\n%s", s, output)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(output, s) {
					t.Errorf("output holds %q\n%s", s, output)
				}
			}
		})
	}
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
