package render_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// keeperUnreleased is the keeper fixture with every platform image at 0.0.0 again, as the catalog
// holds them before the first release; keeperResult tags them 0.0.0-test.
func keeperUnreleased(t *testing.T) (contract.CheckResult, contract.Catalog, contract.Templates) {
	t.Helper()
	catalog, templates := loadCatalogTemplates(t, "../../catalog", keeperDir+"/catalog")
	return checkFile(t, keeperDir+"/graph.yaml", catalog), catalog, templates
}

// TestRenderJobImage covers spec 0030 AC-14a to c: a stack with a provisioner binding at 0.0.0
// carries build/oidc-job/Dockerfile once, keeps the image line of every job service, builds it
// from its own folder with no MP_SOURCE, and labels it with the zip's ref.
func TestRenderJobImage(t *testing.T) {
	t.Parallel()
	result, catalog, templates := keeperUnreleased(t)
	dockerfiles, err := contract.LoadDockerfiles(os.DirFS("../../catalog"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../catalog/images/oidc-job/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]string{"commit": prereleaseCommit, "local": render.LocalSourceRef} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := render.NewSourceRef(ref)
			if err != nil {
				t.Fatal(err)
			}
			out, err := render.Platform(result, catalog, templates, render.WithSource(source, dockerfiles))
			if err != nil {
				t.Fatal(err)
			}
			if got := file(out, "build/oidc-job/Dockerfile"); !bytes.Equal(got, want) {
				t.Errorf("build/oidc-job/Dockerfile differs from the catalog file")
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
			jobs := 0
			for n, svc := range project.Services {
				if svc.Image != "ghcr.io/racherom/modular-platform/oidc-job:0.0.0" {
					continue
				}
				jobs++
				if svc.PullPolicy != "never" {
					t.Errorf("job service %s has pull_policy %q, want never", n, svc.PullPolicy)
				}
			}
			if jobs == 0 {
				t.Fatal("no service runs the job image")
			}
			setup := string(file(out, "setup.sh"))
			if n := strings.Count(setup, "build_job_image 'ghcr.io/racherom/modular-platform/oidc-job:0.0.0' 'oidc-job'"); n != 1 {
				t.Errorf("setup.sh builds the job image %d times, want 1", n)
			}
			if !strings.Contains(setup, `--build-arg MP_SOURCE_REF="$source_ref" \`+"\n\t\t-f \"build/$2/Dockerfile\" \"build/$2\"") {
				t.Error("setup.sh builds the job image with another context or a MP_SOURCE argument")
			}
		})
	}

	t.Run("dev tag", func(t *testing.T) {
		t.Parallel()
		retagged, rc, templates := keeperResult(t)
		source, _ := render.NewSourceRef(prereleaseCommit)
		out, err := render.Platform(retagged, rc, templates, render.WithSource(source, dockerfiles))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out.Files {
			if strings.HasPrefix(f.Path, "build/") {
				t.Errorf("a retagged render holds %s", f.Path)
			}
		}
	})
}

// TestRenderJobImageOnly covers spec 0030 AC-14: a zip whose only build is a job image runs
// setup.sh without MP_SOURCE_PATH, even for a local ref, and its README names only that image.
func TestRenderJobImageOnly(t *testing.T) {
	t.Parallel()
	result, catalog, templates := keeperUnreleased(t)
	for i, m := range catalog.Modules {
		if strings.HasPrefix(m.Image, render.PlatformImages) && m.Image != "" {
			catalog.Modules[i].Image = render.PlatformImages + strings.SplitN(strings.TrimPrefix(m.Image, render.PlatformImages), ":", 2)[0] + ":0.0.0-test"
		}
	}
	dockerfiles, err := contract.LoadDockerfiles(os.DirFS("../../catalog"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := render.NewSourceRef(render.LocalSourceRef)
	out, err := render.Platform(result, catalog, templates, render.WithSource(local, dockerfiles))
	if err != nil {
		t.Fatal(err)
	}
	setup := string(file(out, "setup.sh"))
	if strings.Contains(setup, "set MP_SOURCE_PATH=<a checkout") || strings.Contains(setup, "build_image()") {
		t.Error("a job image only zip stops on MP_SOURCE_PATH or builds a module image")
	}
	if !strings.Contains(setup, "build_job_image ") {
		t.Error("setup.sh does not build the job image")
	}
	readme := string(file(out, "README.md"))
	if strings.Contains(readme, "source ref `local`") || !strings.Contains(readme, "Job images are built from the Dockerfile in this zip alone") {
		t.Errorf("README.md of a job image only zip:\n%s", readme)
	}
}
