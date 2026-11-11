package render_test

import (
	"os"
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
