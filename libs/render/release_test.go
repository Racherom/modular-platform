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
	Env         map[string]string `json:"env"`
	Strategy    struct {
		FailFast *bool `json:"fail-fast"`
		Matrix   struct {
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

// TestReleaseImagesJob covers spec 0030 AC-9 and the value sourcing of the images job without a
// release: it runs only when release-please created a release, builds the four images from the
// release commit (github.sha) at the version release-please names (through env) for both
// platforms, checks GHCR right before the push and stops when the tag is there, and holds the
// only packages: write. The old release pull request workflow is gone.
func TestReleaseImagesJob(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("../../.github/workflows/release-images.yml"); !os.IsNotExist(err) {
		t.Errorf("release-images.yml still exists (%v); the images are pushed after the merge", err)
	}
	w, _ := readWorkflow(t, "release.yml")
	if w.Permissions["packages"] != "" {
		t.Errorf("workflow permissions grant packages: %v; only the images job may", w.Permissions)
	}
	for name, j := range w.Jobs {
		if name != "images" && j.Permissions["packages"] == "write" {
			t.Errorf("job %s has packages: write; only the images job may", name)
		}
	}

	images, ok := w.Jobs["images"]
	if !ok {
		t.Fatal("no images job")
	}
	if images.Needs != "release-please" || images.If != "needs.release-please.outputs.created == 'true'" {
		t.Errorf("images job needs %v, if %q; want release-please and its created output", images.Needs, images.If)
	}
	if images.Permissions["packages"] != "write" || images.Permissions["contents"] != "read" {
		t.Errorf("images job permissions %v, want contents: read, packages: write", images.Permissions)
	}
	if images.Strategy.FailFast == nil || *images.Strategy.FailFast {
		t.Error("images matrix must set fail-fast: false, so one failed image does not cancel the others")
	}
	services := slices.Sorted(slices.Values(images.Strategy.Matrix.Service))
	if !slices.Equal(services, releaseImages) {
		t.Errorf("images matrix %v, want %v", services, releaseImages)
	}
	if images.Env["VERSION"] != "${{ needs.release-please.outputs.version }}" {
		t.Errorf("images job env VERSION = %q, want the version release-please names", images.Env["VERSION"])
	}

	const sha = "${{ github.sha }}"
	checkout := stepIndex(images, "actions/checkout")
	if checkout < 0 || images.Steps[checkout].With["ref"] != sha {
		t.Errorf("images job does not check out %s", sha)
	}
	push := stepIndex(images, "docker/build-push-action")
	if push != len(images.Steps)-1 {
		t.Fatalf("the build and push is step %d of %d; it must be the last", push, len(images.Steps))
	}
	login := stepIndex(images, "docker/login-action")
	guard := stepIndex(images, "docker buildx imagetools inspect")
	if guard < 0 || guard < login || guard != push-1 {
		t.Fatal("right before the push, after the GHCR login, a step must check GHCR for the tag")
	}
	run := images.Steps[guard].Run
	for _, want := range []string{
		`ghcr.io/racherom/modular-platform/$SERVICE:$VERSION`,
		"is already in GHCR; a released image is never pushed again",
		`*"not found"*)`,
		"could not check whether",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("GHCR check lacks %q:\n%s", want, run)
		}
	}
	if strings.Count(run, "exit 1") != 2 {
		t.Errorf("GHCR check must stop when the tag exists and on any other failure:\n%s", run)
	}

	with := images.Steps[push].With
	str := func(k string) string { v, _ := with[k].(string); return v }
	args := strings.Split(strings.TrimSpace(str("build-args")), "\n")
	for _, want := range []string{"MP_SOURCE=local", "MP_SOURCE_REF=" + sha} {
		if !slices.Contains(args, want) {
			t.Errorf("build-args %q lack %q", str("build-args"), want)
		}
	}
	checks := map[string]string{
		"context":    ".",
		"file":       "catalog/modules/${{ matrix.service }}/Dockerfile",
		"platforms":  "linux/amd64,linux/arm64",
		"provenance": "mode=max",
		"tags":       "ghcr.io/racherom/modular-platform/${{ matrix.service }}:${{ env.VERSION }}",
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
}

// TestReleaseWorkflow covers spec 0030 AC-11 and the managed zip part of AC-9 without a release:
// release-please runs only with RELEASES_ENABLED, only with RELEASE_PLEASE_TOKEN (checked first),
// never with github.token; the managed zip job waits for the images job, is skipped with
// release-please, and checks the images first.
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
	needs, _ := mz.Needs.([]any)
	if !slices.Equal(needs, []any{"release-please", "images"}) || mz.If != "needs.release-please.outputs.created == 'true'" {
		t.Errorf("managed-zip needs %v, if %q; it must wait for the images and be skipped with release-please (no always())", mz.Needs, mz.If)
	}
	check, zip := stepIndex(mz, "scripts/release-images-check.sh"), stepIndex(mz, "task managed:zip")
	if check < 0 || zip < check || stepIndex(mz, "actions/checkout") > check {
		t.Error("managed-zip must check out, run scripts/release-images-check.sh, then task managed:zip")
	}
}

// TestReleaseImagesCheckScript covers the image list of the check the managed zip job runs: it
// names the same four images the images job pushes.
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
	// the commit on a clean tree, local with changes (spec 0030, AC-12)
	if want := `if [ -z "$(git status --porcelain)" ]; then git rev-parse HEAD; else echo local; fi`; ref["sh"] != want {
		t.Errorf("Taskfile SOURCE_REF = %v, want sh: %s", tf.Vars["SOURCE_REF"], want)
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
	for _, name := range []string{"release.yml"} {
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
