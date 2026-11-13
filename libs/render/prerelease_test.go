package render_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// prereleaseCommit is the source ref of the prerelease golden.
const prereleaseCommit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"

// prereleaseRender renders testdata/prerelease (two renderer instances and postgres) with the
// built in catalog, its Dockerfiles and ref.
func prereleaseRender(t *testing.T, ref string, opts ...render.Option) render.Output {
	t.Helper()
	catalog := loadCatalog(t, "../../catalog")
	result := checkFile(t, "testdata/prerelease/graph.yaml", catalog)
	if ref != "" {
		source, err := render.NewSourceRef(ref)
		if err != nil {
			t.Fatal(err)
		}
		dockerfiles, err := contract.LoadDockerfiles(os.DirFS("../../catalog"), catalog)
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, render.WithSource(source, dockerfiles))
	}
	out, err := render.Render(result, catalog, builtinTemplates(t), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// file returns the rendered file at path, or nil.
func file(out render.Output, path string) []byte {
	for _, f := range out.Files {
		if f.Path == path {
			return f.Data
		}
	}
	return nil
}

// TestRenderPrereleaseGolden covers spec 0030 AC-1, AC-5 and AC-13: the renderer's Dockerfile,
// once for both instances, the build block first in setup.sh, the README section, and an
// unchanged image line with no build section. The golden holds no copy of the Dockerfile: the
// zip's is compared with the catalog file read now, so a digest update needs no -update. Run
// with -update after an intended change.
func TestRenderPrereleaseGolden(t *testing.T) {
	out := prereleaseRender(t, prereleaseCommit)
	var stored render.Output
	for _, f := range out.Files {
		if !strings.HasPrefix(f.Path, "build/") {
			stored.Files = append(stored.Files, f)
		}
	}
	checkGolden(t, filepath.Join("testdata", "prerelease", "golden"), stored)

	want, err := os.ReadFile("../../catalog/modules/renderer/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if got := file(out, "build/renderer/Dockerfile"); !bytes.Equal(got, want) {
		t.Error("build/renderer/Dockerfile is not the catalog's Dockerfile byte for byte")
	}
	var builds []string
	for _, f := range out.Files {
		if strings.HasPrefix(f.Path, "build/") {
			builds = append(builds, f.Path)
		}
	}
	if len(builds) != 1 {
		t.Errorf("build files %v, want only build/renderer/Dockerfile", builds)
	}
	compose := string(file(out, "docker-compose.yml"))
	if strings.Contains(compose, "build:") || strings.Contains(compose, "pull_policy") {
		t.Errorf("docker-compose.yml has a build section or pull_policy:\n%s", compose)
	}
	if !strings.Contains(compose, "image: ghcr.io/racherom/modular-platform/renderer:0.0.0\n") {
		t.Errorf("docker-compose.yml lost the renderer image line:\n%s", compose)
	}
	// the build block runs before setup.sh writes .env or makes any secret (AC-4)
	setup := string(file(out, "setup.sh"))
	build, write := strings.Index(setup, "\nbuild_image 'ghcr.io/racherom/modular-platform/renderer:0.0.0' 'renderer'\n"), strings.Index(setup, "\nset_id MP_UID")
	if build < 0 || write < 0 || build > write {
		t.Errorf("setup.sh builds at %d and first writes .env at %d, want the build first", build, write)
	}
	if strings.Count(setup, "\nbuild_image ") != 1 {
		t.Error("setup.sh builds the renderer image more than once")
	}
}

// TestRenderPrereleaseLocal covers spec 0030 AC-12 for a zip rendered with the ref local: its
// README and setup.sh (golden-local holds only these two, the files a local ref changes), and a
// setup.sh run without MP_SOURCE_PATH, which stops before the BuildKit check and before it makes
// any file, naming MP_SOURCE_PATH. It runs no Docker. Run with -update after an intended change.
func TestRenderPrereleaseLocal(t *testing.T) {
	out := prereleaseRender(t, render.LocalSourceRef)
	var changed render.Output
	for _, f := range out.Files {
		if f.Path == "README.md" || f.Path == "setup.sh" {
			changed.Files = append(changed.Files, f)
		}
	}
	checkGolden(t, filepath.Join("testdata", "prerelease", "golden-local"), changed)

	readme := string(file(out, "README.md"))
	if !strings.Contains(readme, "source ref `local`") || strings.Contains(readme, "is a branch, not a commit") {
		t.Errorf("README.md lacks the local text, or warns about a branch:\n%s", readme)
	}
	setup := string(file(out, "setup.sh"))
	if stop, buildx := strings.Index(setup, "made by a renderer built from a local checkout"), strings.Index(setup, "docker buildx version"); stop < 0 || stop > buildx {
		t.Errorf("setup.sh stops for local at %d, after the BuildKit check at %d", stop, buildx)
	}

	dir := t.TempDir()
	writeOutput(t, dir, out)
	cmd := exec.CommandContext(t.Context(), "sh", "setup.sh")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MP_SOURCE_PATH=")
	cmd.Stdin = strings.NewReader("")
	stopped, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("setup.sh of a local zip ran without MP_SOURCE_PATH:\n%s", stopped)
	}
	if !strings.Contains(string(stopped), "set MP_SOURCE_PATH=<a checkout of Modular Platform>") {
		t.Errorf("setup.sh stop does not name MP_SOURCE_PATH:\n%s", stopped)
	}
	if env, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil || strings.Contains(string(env), "MP_UID=") {
		t.Errorf("setup.sh wrote .env before it stopped (%v)", err)
	}
	for _, made := range []string{"secrets", "data"} {
		if _, err := os.Stat(filepath.Join(dir, made)); err == nil {
			t.Errorf("setup.sh made %s before it stopped", made)
		}
	}
}

// TestRenderPrereleaseRules covers the edges of spec 0030: no WithSource or a released image
// builds nothing (the render is the one of today), a branch ref warns in the README, and an
// unreleased module without a Dockerfile fails the render.
func TestRenderPrereleaseRules(t *testing.T) {
	t.Run("without WithSource", func(t *testing.T) {
		for _, f := range prereleaseRender(t, "").Files {
			if strings.HasPrefix(f.Path, "build/") {
				t.Errorf("rendered %s without WithSource", f.Path)
			}
		}
	})
	t.Run("same bytes twice", func(t *testing.T) {
		a, b := prereleaseRender(t, "main"), prereleaseRender(t, "main")
		for i := range a.Files {
			if a.Files[i].Path != b.Files[i].Path || !bytes.Equal(a.Files[i].Data, b.Files[i].Data) {
				t.Errorf("%s differs between two renders", a.Files[i].Path)
			}
		}
	})
	t.Run("branch ref", func(t *testing.T) {
		out := prereleaseRender(t, "main")
		if readme := string(file(out, "README.md")); !strings.Contains(readme, "`main` is a branch, not a commit") {
			t.Errorf("README.md does not warn about the branch:\n%s", readme)
		}
		if setup := string(file(out, "setup.sh")); !strings.Contains(setup, "source_ref='main'\n") {
			t.Error("setup.sh does not build main")
		}
	})
	t.Run("released image", func(t *testing.T) {
		catalog := loadCatalog(t, "../../catalog")
		for i, m := range catalog.Modules {
			if m.Name == "renderer" {
				catalog.Modules[i].Image = render.PlatformImages + "renderer:1.0.0"
			}
		}
		result := checkFile(t, "testdata/prerelease/graph.yaml", catalog)
		ref, _ := render.NewSourceRef(prereleaseCommit)
		out, err := render.Render(result, catalog, builtinTemplates(t), render.WithSource(ref, map[string][]byte{"renderer": []byte("FROM scratch\n")}))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out.Files {
			if strings.HasPrefix(f.Path, "build/") || bytes.Contains(f.Data, []byte("build_image")) {
				t.Errorf("%s builds a released image", f.Path)
			}
		}
	})
	t.Run("no Dockerfile", func(t *testing.T) {
		catalog := loadCatalog(t, "../../catalog")
		result := checkFile(t, "testdata/prerelease/graph.yaml", catalog)
		ref, _ := render.NewSourceRef(prereleaseCommit)
		_, err := render.Render(result, catalog, builtinTemplates(t), render.WithSource(ref, map[string][]byte{}))
		if err == nil || !strings.Contains(err.Error(), "renderer") {
			t.Errorf("err = %v, want one naming the renderer", err)
		}
	})
}

// TestRenderMissingDockerfileStable checks that with several unreleased modules lacking a
// Dockerfile, the render error names the same module every time (instances walked in name order).
func TestRenderMissingDockerfileStable(t *testing.T) {
	t.Parallel()
	catalog, templates := loadCatalogTemplates(t, "../../catalog")
	result := checkFile(t, "../../deploy/managed/graph.yaml", catalog)
	ref, _ := render.NewSourceRef(prereleaseCommit)
	var first string
	for range 20 {
		_, err := render.Platform(result, catalog, templates, render.WithSource(ref, map[string][]byte{}))
		if err == nil {
			t.Fatal("render without Dockerfiles succeeded")
		}
		if first == "" {
			first = err.Error()
		} else if err.Error() != first {
			t.Fatalf("render errors differ between runs: %q and %q", first, err)
		}
	}
}

// TestUnreleased covers the one predicate of spec 0030: the platform prefix and exactly 0.0.0.
func TestUnreleased(t *testing.T) {
	t.Parallel()
	for image, want := range map[string]bool{
		"ghcr.io/racherom/modular-platform/renderer:0.0.0":  true,
		"ghcr.io/racherom/modular-platform/renderer:0.0.01": false,
		"ghcr.io/racherom/modular-platform/renderer:dev":    false,
		"ghcr.io/racherom/modular-platform/renderer:1.0.0":  false,
		"ghcr.io/racherom/modular-platform/renderer":        false,
		"ghcr.io/other/renderer:0.0.0":                      false,
		"postgres:0.0.0":                                    false,
	} {
		if got := render.Unreleased(image); got != want {
			t.Errorf("Unreleased(%q) = %v, want %v", image, got, want)
		}
	}
}

// TestRenderPlatformPrerelease covers spec 0030 AC-6: the platform folder rendered while the
// platform images carry 0.0.0 holds the Dockerfile of api, renderer, web and deployer-docker,
// gives exactly their services pull_policy: never, and builds them first in setup.sh; a retagged
// render (dev, ci) builds nothing.
func TestRenderPlatformPrerelease(t *testing.T) {
	catalog, templates := loadCatalogTemplates(t, "../../catalog")
	result := checkFile(t, "../../deploy/managed/graph.yaml", catalog)
	ref, err := render.NewSourceRef(prereleaseCommit)
	if err != nil {
		t.Fatal(err)
	}
	dockerfiles, err := contract.LoadDockerfiles(os.DirFS("../../catalog"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	out, err := render.Platform(result, catalog, templates, render.WithSource(ref, dockerfiles))
	if err != nil {
		t.Fatal(err)
	}
	var builds []string
	for _, f := range out.Files {
		if strings.HasPrefix(f.Path, "build/") {
			builds = append(builds, f.Path)
		}
	}
	want := []string{"build/api/Dockerfile", "build/deployer-docker/Dockerfile", "build/renderer/Dockerfile", "build/web/Dockerfile"}
	if strings.Join(builds, " ") != strings.Join(want, " ") {
		t.Errorf("build files %v, want %v", builds, want)
	}
	var project struct {
		Services map[string]struct {
			Image      string `json:"image"`
			PullPolicy string `json:"pull_policy"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal(file(out, "docker-compose.yml"), &project); err != nil {
		t.Fatal(err)
	}
	never := 0
	for name, svc := range project.Services {
		if wantNever := render.Unreleased(svc.Image); (svc.PullPolicy == "never") != wantNever {
			t.Errorf("service %s (%s) has pull_policy %q", name, svc.Image, svc.PullPolicy)
		}
		if svc.PullPolicy == "never" {
			never++
		}
	}
	if never < 4 {
		t.Errorf("%d services with pull_policy never, want at least the four platform services", never)
	}
	setup := string(file(out, "setup.sh"))
	if b, d := strings.Index(setup, "\nbuild_image "), strings.Index(setup, "\nset_id MP_DIR"); b < 0 || d < b {
		t.Errorf("setup.sh builds at %d, after it writes MP_DIR at %d", b, d)
	}
	if !strings.Contains(string(file(out, "README.md")), "### Images built on this host") {
		t.Error("README.md has no section on the images built on this host")
	}
	// rendered from a checkout with changes, the README says so and setup.sh needs MP_SOURCE_PATH
	local, _ := render.NewSourceRef(render.LocalSourceRef)
	localOut, err := render.Platform(result, catalog, templates, render.WithSource(local, dockerfiles))
	if err != nil {
		t.Fatal(err)
	}
	if readme := string(file(localOut, "README.md")); !strings.Contains(readme, "source ref `local`") {
		t.Errorf("README.md of a local render lacks the local text:\n%s", readme)
	}
	if setup := string(file(localOut, "setup.sh")); !strings.Contains(setup, "set MP_SOURCE_PATH=<a checkout of Modular Platform>") {
		t.Error("setup.sh of a local render does not stop without MP_SOURCE_PATH")
	}

	// a render retagged dev or ci names no unreleased image, so nothing changes
	retagged, _, _ := platformResult(t)
	plain, err := render.Platform(retagged, platformCatalog(t), templates)
	if err != nil {
		t.Fatal(err)
	}
	with, err := render.Platform(retagged, platformCatalog(t), templates, render.WithSource(ref, dockerfiles))
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Files) != len(with.Files) {
		t.Fatalf("a retagged render with WithSource has %d files, want %d", len(with.Files), len(plain.Files))
	}
	for i := range plain.Files {
		if !bytes.Equal(plain.Files[i].Data, with.Files[i].Data) {
			t.Errorf("a retagged render with WithSource changes %s", plain.Files[i].Path)
		}
	}
}

// platformCatalog is the catalog of platformResult, its platform images retagged.
func platformCatalog(t *testing.T) contract.Catalog {
	t.Helper()
	_, catalog, _ := platformResult(t)
	return catalog
}

// fakeBuildDocker writes a docker program into a new folder and returns that folder, for a PATH that
// puts it first. buildx says whether `docker buildx version` succeeds, version is what
// `docker version` prints (empty: it fails, as when the daemon does not answer). Every other call
// is appended to the calls file and fails, so a build never reaches a real Docker.
func fakeBuildDocker(t *testing.T, buildx bool, version, calls string) string {
	t.Helper()
	bin := t.TempDir()
	buildxExit := "1"
	if buildx {
		buildxExit = "0"
	}
	versionCmd := "exit 1"
	if version != "" {
		versionCmd = "echo '" + version + "'; exit 0"
	}
	fake := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = 'buildx version' ]; then exit " + buildxExit + "; fi\n" +
		"if [ \"$1\" = version ]; then " + versionCmd + "; fi\n" +
		"echo \"$*\" >> '" + calls + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestPrereleaseSetupDockerChecks covers spec 0030 AC-4 without a real Docker: on a host without
// buildx, with a daemon that does not answer, or with Docker older than 23, setup.sh of a zip
// that builds stops before it writes .env or makes secrets/ or data/, names the image, the ref
// and MP_SOURCE_PATH, and never starts a build. Docker 23 or newer passes the checks and builds
// from GitHub at the zip's ref.
func TestPrereleaseSetupDockerChecks(t *testing.T) {
	t.Parallel()
	out := prereleaseRender(t, prereleaseCommit)
	const image = "ghcr.io/racherom/modular-platform/renderer:0.0.0"
	for name, tc := range map[string]struct {
		buildx  bool
		version string
		stop    string
		builds  bool
	}{
		"no buildx": {
			buildx: false, version: "27.3.1",
			stop: "docker buildx is not available; install Docker 23 or newer with the buildx plugin",
		},
		"daemon does not answer": {
			buildx: true, version: "",
			stop: "Docker does not answer; start it and run setup.sh again",
		},
		"Docker 20": {
			buildx: true, version: "20.10.24",
			stop: "the platform images need Docker 23 or newer, this host runs 20.10.24",
		},
		"Docker 23 passes and builds from GitHub": {
			buildx: true, version: "23.0.0", builds: true,
			stop: "could not build " + image + " at " + prereleaseCommit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeOutput(t, dir, out)
			calls := filepath.Join(t.TempDir(), "docker-calls")
			cmd := exec.CommandContext(t.Context(), "sh", "setup.sh")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "MP_SOURCE_PATH=", "PATH="+fakeBuildDocker(t, tc.buildx, tc.version, calls)+string(os.PathListSeparator)+os.Getenv("PATH"))
			cmd.Stdin = strings.NewReader("")
			got, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("setup.sh went on:\n%s", got)
			}
			for _, want := range []string{tc.stop, image, prereleaseCommit, "MP_SOURCE_PATH="} {
				if !strings.Contains(string(got), want) {
					t.Errorf("setup.sh output lacks %q:\n%s", want, got)
				}
			}
			build, _ := os.ReadFile(calls)
			if tc.builds != strings.Contains(string(build), "buildx build") {
				t.Errorf("docker calls %q, want a build: %v", build, tc.builds)
			}
			if tc.builds && !strings.Contains(string(build), "MP_SOURCE=git --build-arg MP_SOURCE_REF="+prereleaseCommit) {
				t.Errorf("the build is not from GitHub at %s: %q", prereleaseCommit, build)
			}
			if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
				if env, _ := os.ReadFile(filepath.Join(dir, ".env")); strings.Contains(string(env), "MP_UID=") {
					t.Error("setup.sh wrote MP_UID to .env before it stopped")
				}
			}
			for _, made := range []string{"secrets", "data"} {
				if _, err := os.Stat(filepath.Join(dir, made)); err == nil {
					t.Errorf("setup.sh made %s before it stopped", made)
				}
			}
		})
	}
}
