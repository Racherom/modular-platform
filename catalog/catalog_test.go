package catalog_test

import (
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
