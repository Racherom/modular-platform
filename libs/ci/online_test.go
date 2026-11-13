package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/render"
)

// TestOnlinePrereleaseOverlay covers spec 0030 AC-8: deploy/online/compose.prerelease.yaml
// builds exactly the services of deploy/online/compose.yaml that run a platform image, each from
// its own catalog folder at MP_SOURCE_REF, and versions.env sets MP_SOURCE_REF.
func TestOnlinePrereleaseOverlay(t *testing.T) {
	t.Parallel()
	type project struct {
		Services map[string]struct {
			Image string `json:"image"`
			Build struct {
				Context string            `json:"context"`
				Args    map[string]string `json:"args"`
			} `json:"build"`
		} `json:"services"`
	}
	read := func(path string) project {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var p project
		if err := yaml.Unmarshal(data, &p); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return p
	}
	base, overlay := read("../../deploy/online/compose.yaml"), read("../../deploy/online/compose.prerelease.yaml")
	var platform, built []string
	for name, svc := range base.Services {
		if strings.HasPrefix(svc.Image, render.PlatformImages) {
			platform = append(platform, name)
		}
	}
	for name, svc := range overlay.Services {
		built = append(built, name)
		want := "https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF:?set in versions.env}:catalog/modules/" + name
		if svc.Build.Context != want {
			t.Errorf("%s builds from %q, want %q", name, svc.Build.Context, want)
		}
		if svc.Build.Args["MP_SOURCE"] != "git" || svc.Build.Args["MP_SOURCE_REF"] != "${MP_SOURCE_REF:?set in versions.env}" {
			t.Errorf("%s has build args %v", name, svc.Build.Args)
		}
		if _, err := os.Stat("../../catalog/modules/" + name + "/Dockerfile"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	slices.Sort(platform)
	slices.Sort(built)
	if !slices.Equal(platform, built) || len(platform) == 0 {
		t.Errorf("the overlay builds %v, compose.yaml runs the platform images of %v", built, platform)
	}
	versions, err := os.ReadFile("../../deploy/online/versions.env")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(versions), "\nMP_SOURCE_REF=") {
		t.Error("versions.env does not set MP_SOURCE_REF")
	}
}

// git runs git in dir with a fixed identity and fails the test on an error; it returns stdout.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestOnlineSourceRef covers spec 0030 AC-8 for scripts/online-source-ref.sh against a local bare
// repository: a full commit passes with no network call, a branch resolves to its newest commit
// (a branch before a tag of the same name), an annotated tag to the commit it points to, a
// lightweight tag to its commit, and local, a short commit, a bad form, an unknown ref and an
// unreachable remote stop with a message that names the ref.
func TestOnlineSourceRef(t *testing.T) {
	t.Parallel()
	work, bare := t.TempDir(), filepath.Join(t.TempDir(), "mp.git")
	git(t, work, "init", "-q", "-b", "main")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "one")
	first := git(t, work, "rev-parse", "HEAD")
	git(t, work, "tag", "-a", "-m", "v1", "v1.0.0")
	git(t, work, "tag", "light")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "two")
	second := git(t, work, "rev-parse", "HEAD")
	git(t, work, "branch", "both", first)
	git(t, work, "tag", "both", second)   // a branch and a tag of one name: the branch wins
	git(t, work, "branch", "CAFE", first) // uppercase hex is a name, as in render.NewSourceRef
	git(t, work, "clone", "-q", "--bare", work, bare)

	script, err := filepath.Abs("../../scripts/online-source-ref.sh")
	if err != nil {
		t.Fatal(err)
	}
	const commit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	for name, tc := range map[string]struct {
		ref, remote string
		want        string // the commit on stdout
		stop        string // a part of the message on stderr; empty for success
	}{
		"full commit, no network": {ref: commit, remote: "/nonexistent/mp.git", want: commit},
		"branch":                  {ref: "main", want: second},
		"annotated tag":           {ref: "v1.0.0", want: first},
		"lightweight tag":         {ref: "light", want: first},
		"branch before tag":       {ref: "both", want: first},
		"local":                   {ref: "local", stop: "MP_SOURCE_REF=local builds from a checkout"},
		"short commit":            {ref: first[:7], stop: "MP_SOURCE_REF=" + first[:7] + " is neither a full commit nor a branch or tag name"},
		"uppercase hex branch":    {ref: "CAFE", want: first},
		"uppercase commit":        {ref: strings.ToUpper(commit), stop: strings.ToUpper(commit) + " is no branch or tag of " + bare},
		"lowercase hex name":      {ref: "cafe", stop: "MP_SOURCE_REF=cafe is neither a full commit"},
		"bad form":                {ref: "a b", stop: "MP_SOURCE_REF=a b is neither a full commit"},
		"unknown":                 {ref: "nope", stop: "nope is no branch or tag of " + bare},
		"unreachable":             {ref: "main", remote: "/nonexistent/mp.git", stop: "could not reach /nonexistent/mp.git to resolve main"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			remote := tc.remote
			if remote == "" {
				remote = bare
			}
			cmd := exec.CommandContext(t.Context(), "sh", script)
			cmd.Dir = "../.."
			cmd.Env = append(os.Environ(), "MP_SOURCE_REF="+tc.ref, "MP_SOURCE_REMOTE="+remote)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			got := strings.TrimSpace(string(out))
			if tc.stop != "" {
				if err == nil || got != "" || !strings.Contains(stderr.String(), tc.stop) {
					t.Errorf("err %v, stdout %q, stderr %q; want a stop with %q and nothing on stdout", err, got, stderr.String(), tc.stop)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("stdout %q (err %v, stderr %q), want %s", got, err, stderr.String(), tc.want)
			}
		})
	}

	// with no MP_SOURCE_REF in the shell, the versions.env line is the input: a fixture in the
	// same form as deploy/online/versions.env, so the check runs whatever the real file names
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "deploy/online"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := "MP_VERSION=0.0.0\n# the ref the images build from\nMP_SOURCE_REF=both # a comment\n"
	if err := os.WriteFile(filepath.Join(dir, "deploy/online/versions.env"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", script)
	cmd.Dir = dir
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(e string) bool { return strings.HasPrefix(e, "MP_SOURCE_REF=") }), "MP_SOURCE_REMOTE="+bare)
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != first {
		t.Errorf("with versions.env naming both: stdout %q (err %v), want %s", out, err, first)
	}
}
