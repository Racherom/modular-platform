package callerkey_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/racherom/modular-platform/sdk/caller"
	"github.com/racherom/modular-platform/services/api/internal/callerkey"
)

// TestGenerate covers the api setup step (spec 0015): it writes a pair the sdk reads, the
// signing key 0600 and the public key 0644, and never replaces a file.
func TestGenerate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := callerkey.Run([]string{"generate", dir}); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{callerkey.SigningFile: 0o600, callerkey.PublicFile: 0o644} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&^mode != 0 {
			t.Errorf("%s has mode %v, want at most %v", name, info.Mode().Perm(), mode)
		}
	}
	signing, _ := os.ReadFile(filepath.Join(dir, callerkey.SigningFile))
	public, _ := os.ReadFile(filepath.Join(dir, callerkey.PublicFile))
	if _, err := caller.ParseSigningKey(signing); err != nil {
		t.Errorf("signing key: %v", err)
	}
	if _, err := caller.KeysOf("api", public); err != nil {
		t.Errorf("public key: %v", err)
	}
	if err := callerkey.Run([]string{"generate", dir}); err == nil {
		t.Error("a second run replaced the pair")
	}
	for _, args := range [][]string{nil, {"rotate"}, {"generate", dir, "extra"}} {
		if err := callerkey.Run(args); err == nil {
			t.Errorf("Run(%q) passed", args)
		}
	}
}
