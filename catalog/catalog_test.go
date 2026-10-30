package catalog_test

import (
	"testing"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
)

// TestBuiltInCatalog runs the same checks the planner runs at start (AC-11).
func TestBuiltInCatalog(t *testing.T) {
	entries, problems := contract.LoadCatalog(catalog.FS)
	for _, p := range problems {
		t.Error(p)
	}
	if len(entries) == 0 {
		t.Fatal("the built in catalog is empty")
	}
}
