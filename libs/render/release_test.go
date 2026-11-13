package render_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// The parts of a GitHub Actions workflow the release checks read.
type workflow struct {
	Permissions map[string]string `json:"permissions"`
	Jobs        map[string]job    `json:"jobs"`
}

type job struct {
	Name        string            `json:"name"`
	If          string            `json:"if"`
	Needs       any               `json:"needs"`
	Permissions map[string]string `json:"permissions"`
	Strategy    struct {
		Matrix struct {
			Service []string `json:"service"`
		} `json:"matrix"`
	} `json:"strategy"`
	Steps []step `json:"steps"`
}

type step struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Uses string            `json:"uses"`
	Run  string            `json:"run"`
	Env  map[string]string `json:"env"`
	With map[string]any    `json:"with"`
}

// The four platform images every release pushes and checks.
var releaseImages = []string{"api", "deployer-docker", "renderer", "web"}

func readWorkflow(t *testing.T, name string) (workflow, string) {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var w workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return w, string(data)
}

// stepIndex returns the index of the first step whose uses or run contains s, or -1.
func stepIndex(j job, s string) int {
	return slices.IndexFunc(j.Steps, func(st step) bool {
		return strings.Contains(st.Uses, s) || strings.Contains(st.Run, s)
	})
}

// TestReleaseImagesWorkflow covers spec 0030 AC-9 and the value sourcing of the release pull
// request job without a release: the images are built from the pull request head commit, at the
// version the manifest names there, for both platforms, pushed only from this repository's
// release-please branch, and the required check passes on every other pull request.
func TestReleaseImagesWorkflow(t *testing.T) {
	t.Parallel()
	w, _ := readWorkflow(t, "release-images.yml")
	if w.Permissions["packages"] != "" {
		t.Errorf("workflow permissions grant packages: %v; only the images job may", w.Permissions)
	}

	images, ok := w.Jobs["images"]
	if !ok {
		t.Fatal("no images job")
	}
	for _, want := range []string{
		"startsWith(github.head_ref, 'release-please--')",
		"github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(images.If, want) {
			t.Errorf("images job if %q lacks %q", images.If, want)
		}
	}
	if images.Permissions["packages"] != "write" {
		t.Errorf("images job permissions %v, want packages: write", images.Permissions)
	}
	services := slices.Sorted(slices.Values(images.Strategy.Matrix.Service))
	if !slices.Equal(services, releaseImages) {
		t.Errorf("images matrix %v, want %v", services, releaseImages)
	}

	const head = "${{ github.event.pull_request.head.sha }}"
	checkout := stepIndex(images, "actions/checkout")
	if checkout < 0 || images.Steps[checkout].With["ref"] != head {
		t.Errorf("images job does not check out %s", head)
	}
	version := slices.IndexFunc(images.Steps, func(s step) bool { return s.ID == "version" })
	if version < checkout || !strings.Contains(images.Steps[version].Run, ".release-please-manifest.json") {
		t.Error("the version step must read .release-please-manifest.json after the checkout of the head")
	}
	push := stepIndex(images, "docker/build-push-action")
	if push < 0 {
		t.Fatal("images job has no build-push step")
	}
	released := stepIndex(images, `refs/tags/v$VERSION`)
	if released < version || released > push || images.Steps[released].Env["VERSION"] != "${{ steps.version.outputs.version }}" ||
		!strings.Contains(images.Steps[released].Run, "exit 1") {
		t.Error("between the version and the push, a step must fail when the git tag v<version> already exists")
	}
	with := images.Steps[push].With
	str := func(k string) string { v, _ := with[k].(string); return v }
	args := strings.Split(strings.TrimSpace(str("build-args")), "\n")
	for _, want := range []string{"MP_SOURCE=local", "MP_SOURCE_REF=" + head} {
		if !slices.Contains(args, want) {
			t.Errorf("build-args %q lack %q", str("build-args"), want)
		}
	}
	checks := map[string]string{
		"context":    ".",
		"file":       "catalog/modules/${{ matrix.service }}/Dockerfile",
		"platforms":  "linux/amd64,linux/arm64",
		"provenance": "mode=max",
		"tags":       "ghcr.io/racherom/modular-platform/${{ matrix.service }}:${{ steps.version.outputs.version }}",
	}
	for k, want := range checks {
		if got := str(k); got != want {
			t.Errorf("build-push %s = %q, want %q", k, got, want)
		}
	}
	for _, k := range []string{"push", "sbom"} {
		if with[k] != true {
			t.Errorf("build-push %s = %v, want true", k, with[k])
		}
	}

	gate, ok := w.Jobs["gate"]
	if !ok {
		t.Fatal("no gate job")
	}
	if gate.Name != "Release images to GHCR" || gate.Needs != "images" || gate.If != "always()" {
		t.Errorf("gate job = name %q, needs %v, if %q; want the required check that always reports on images", gate.Name, gate.Needs, gate.If)
	}
	if run := gate.Steps[0].Run; !strings.Contains(run, "success | skipped) ;;") || !strings.Contains(run, "exit 1") {
		t.Errorf("gate step must pass on success or skipped and fail otherwise:\n%s", run)
	}
}

// TestReleaseWorkflow covers spec 0030 AC-11 and the managed zip part of AC-9 without a release:
// release-please runs only with RELEASES_ENABLED, only with RELEASE_PLEASE_TOKEN (checked first),
// never with github.token; the managed zip job is skipped with it and checks the images first.
func TestReleaseWorkflow(t *testing.T) {
	t.Parallel()
	w, _ := readWorkflow(t, "release.yml")

	rp, ok := w.Jobs["release-please"]
	if !ok {
		t.Fatal("no release-please job")
	}
	if rp.If != "vars.RELEASES_ENABLED == 'true'" {
		t.Errorf("release-please if = %q, want vars.RELEASES_ENABLED == 'true'", rp.If)
	}
	if len(rp.Permissions) != 1 || rp.Permissions["contents"] != "read" {
		t.Errorf("release-please permissions = %v, want only contents: read", rp.Permissions)
	}
	if len(rp.Steps) < 2 {
		t.Fatal("release-please job needs the token check and the action")
	}
	gate := rp.Steps[0]
	if gate.Name != "Release token is set" || gate.Env["TOKEN"] != "${{ secrets.RELEASE_PLEASE_TOKEN }}" ||
		!strings.Contains(gate.Run, `[ -n "$TOKEN" ]`) || !strings.Contains(gate.Run, "RELEASE_PLEASE_TOKEN is not set") {
		t.Errorf("first step must check RELEASE_PLEASE_TOKEN and name it: %+v", gate)
	}
	action := stepIndex(rp, "googleapis/release-please-action")
	if action < 1 || rp.Steps[action].With["token"] != "${{ secrets.RELEASE_PLEASE_TOKEN }}" {
		t.Error("release-please must run after the token check with token: ${{ secrets.RELEASE_PLEASE_TOKEN }}")
	}
	for _, s := range rp.Steps {
		for _, v := range append(slices.Collect(mapValues(s.With)), slices.Collect(mapValues(s.Env))...) {
			if strings.Contains(v, "github.token") || strings.Contains(v, "GITHUB_TOKEN") {
				t.Errorf("release-please step %q uses %q; it must never use github.token", s.Name+s.Uses, v)
			}
		}
	}

	mz, ok := w.Jobs["managed-zip"]
	if !ok {
		t.Fatal("no managed-zip job")
	}
	if mz.Needs != "release-please" || mz.If != "needs.release-please.outputs.created == 'true'" {
		t.Errorf("managed-zip needs %v, if %q; it must be skipped with release-please (no always())", mz.Needs, mz.If)
	}
	check, zip := stepIndex(mz, "scripts/release-images-check.sh"), stepIndex(mz, "task managed:zip")
	if check < 0 || zip < check || stepIndex(mz, "actions/checkout") > check {
		t.Error("managed-zip must check out, run scripts/release-images-check.sh, then task managed:zip")
	}
}

// TestReleaseImagesCheckScript covers the image list of the check the managed zip job runs: it
// names the same four images the release pull request pushes.
func TestReleaseImagesCheckScript(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../scripts/release-images-check.sh")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "for service in "); ok {
			got := slices.Sorted(slices.Values(strings.Fields(strings.TrimSuffix(rest, "; do"))))
			if !slices.Equal(got, releaseImages) {
				t.Errorf("check script images %v, want %v", got, releaseImages)
			}
			return
		}
	}
	t.Error("check script has no image loop")
}

// TestCIImagesSourceRef covers the value sourcing of the other CI image builds: every CI job
// that builds images through task checks out the event's own commit (no ref override) and lets
// the Taskfile pass git rev-parse HEAD, so the images carry github.sha.
func TestCIImagesSourceRef(t *testing.T) {
	t.Parallel()
	w, _ := readWorkflow(t, "ci.yml")
	builds := 0
	for name, j := range w.Jobs {
		build := slices.IndexFunc(j.Steps, func(s step) bool {
			return strings.Contains(s.Run, "task images") || strings.Contains(s.Run, "task build") || strings.Contains(s.Run, "task smoke")
		})
		if build < 0 {
			continue
		}
		builds++
		if strings.Contains(j.Steps[build].Run, "SOURCE_REF") {
			t.Errorf("job %s overrides SOURCE_REF: %q", name, j.Steps[build].Run)
		}
		checkout := stepIndex(j, "actions/checkout")
		if checkout < 0 || checkout > build {
			t.Errorf("job %s builds images before any checkout", name)
			continue
		}
		if ref, ok := j.Steps[checkout].With["ref"]; ok {
			t.Errorf("job %s checks out ref %v, so its images would not carry github.sha", name, ref)
		}
	}
	if builds == 0 {
		t.Error("no CI job builds images; the test reads the wrong file")
	}

	taskfile, err := os.ReadFile("../../Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	var tf struct {
		Vars  map[string]any `json:"vars"`
		Tasks map[string]struct {
			Cmds []any `json:"cmds"`
		} `json:"tasks"`
	}
	if err := yaml.Unmarshal(taskfile, &tf); err != nil {
		t.Fatal(err)
	}
	ref, _ := tf.Vars["SOURCE_REF"].(map[string]any)
	if ref["sh"] != "git rev-parse HEAD" {
		t.Errorf("Taskfile SOURCE_REF = %v, want sh: git rev-parse HEAD", tf.Vars["SOURCE_REF"])
	}
	cmds := tf.Tasks["images"].Cmds
	if len(cmds) != len(releaseImages) {
		t.Errorf("task images has %d commands, want one per image", len(cmds))
	}
	for _, c := range cmds {
		if s, _ := c.(string); !strings.Contains(s, "--build-arg MP_SOURCE_REF={{.SOURCE_REF}}") {
			t.Errorf("task images command %q does not pass MP_SOURCE_REF={{.SOURCE_REF}}", s)
		}
	}
}

// TestReleaseRunSteps checks that no run step of the release workflows writes an expression
// into its script; values go through env, as the GitHub hardening guide asks.
func TestReleaseRunSteps(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"release.yml", "release-images.yml"} {
		w, _ := readWorkflow(t, name)
		for jn, j := range w.Jobs {
			for _, st := range j.Steps {
				if strings.Contains(st.Run, "${{") {
					t.Errorf("%s job %s step %q interpolates an expression into run; pass it through env", name, jn, st.Name+st.Run)
				}
			}
		}
	}
}

func mapValues[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, v := range m {
			s, _ := any(v).(string)
			if !yield(s) {
				return
			}
		}
	}
}
