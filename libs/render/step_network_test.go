package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestStepNetworkInternalNeedsKeeper checks that a setup step with network: internal is refused
// in the static render, whose setup.sh runs before any network exists (spec 0037, AC-9).
func TestStepNetworkInternalNeedsKeeper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/install/catalog")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "modules", "client", "module.yaml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "  - name: trust\n", "  - name: trust\n    network: internal\n", 1)
	if edited == string(data) {
		t.Fatal("the client entry has no trust step to edit")
	}
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := loadCatalog(t, dir)
	result := checkFile(t, "testdata/install/graph.yaml", catalog)
	_, err = render.Render(result, catalog, builtinTemplates(t))
	if err == nil || !strings.Contains(err.Error(), "network internal") {
		t.Fatalf("render error = %v, want a refusal naming network internal", err)
	}
}
