package catalog_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
)

// TestBuiltInCatalog runs the same checks the renderer runs at start (AC-2).
func TestBuiltInCatalog(t *testing.T) {
	t.Parallel()
	c, problems := contract.LoadCatalog(catalog.FS)
	for _, p := range problems {
		t.Error(p)
	}
	if len(c.Modules) == 0 || len(c.Capabilities) == 0 {
		t.Fatal("the built in catalog is empty")
	}
}

// TestPlatformImagesHaveDockerfiles covers the spec 0030 invariant: every module whose image is
// one of the platform's own has the Dockerfile that builds it beside its module.yaml, so a zip
// that names the image before its release can carry it.
func TestPlatformImagesHaveDockerfiles(t *testing.T) {
	t.Parallel()
	c, _ := contract.LoadCatalog(catalog.FS)
	n := 0
	for _, m := range c.Modules {
		if !strings.HasPrefix(m.Image, "ghcr.io/racherom/modular-platform/") {
			continue
		}
		n++
		if _, err := fs.Stat(catalog.FS, "modules/"+m.Name+"/Dockerfile"); err != nil {
			t.Errorf("module %s (image %s) has no Dockerfile: %v", m.Name, m.Image, err)
		}
	}
	if n == 0 {
		t.Fatal("no module names a platform image")
	}
}
