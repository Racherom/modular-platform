package setup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/racherom/modular-platform/services/api/internal/setup"
)

// dirFolder is the sdk's folder rules over a temp dir, enough for the keeper's cases.
type dirFolder struct{ dir string }

func (d dirFolder) Create(_ context.Context, name string, data []byte) error {
	tmp := filepath.Join(d.dir, ".tmp-"+name)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Link(tmp, filepath.Join(d.dir, name))
}

func (d dirFolder) Write(_ context.Context, name string, data []byte) error {
	return os.WriteFile(filepath.Join(d.dir, name), data, 0o600)
}

func (d dirFolder) Remove(_ context.Context, name string) error {
	if err := os.Remove(filepath.Join(d.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (d dirFolder) Exists(_ context.Context, name string) (bool, error) {
	_, err := os.Lstat(filepath.Join(d.dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newFileKeeper(t *testing.T, code string) (*setup.FileKeeper, *fakeFiles, string) {
	t.Helper()
	dir := t.TempDir()
	files := &fakeFiles{}
	files.set(code, "key")
	return setup.NewFileKeeper(dirFolder{dir}, files), files, dir
}

// TestFileKeeperCodeUsed covers AC-15: a code is accepted once, a changed code is refused, and a
// restart (a new keeper over the same folder) keeps the code refused.
func TestFileKeeperCodeUsed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	k, files, dir := newFileKeeper(t, "code-1")

	if err := k.CodeUsed(ctx, sum("other")); !errors.Is(err, setup.ErrCodeChanged) {
		t.Errorf("a hash of another code = %v, want ErrCodeChanged", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "code-used")); err == nil {
		t.Error("a refused hash left a code-used record")
	}
	if err := k.CodeUsed(ctx, sum("code-1")); err != nil {
		t.Fatalf("first use = %v, want nil", err)
	}
	if err := k.CodeUsed(ctx, sum("code-1")); !errors.Is(err, setup.ErrCodeUsed) {
		t.Errorf("second use = %v, want ErrCodeUsed", err)
	}
	restarted := setup.NewFileKeeper(dirFolder{dir}, files)
	if err := restarted.CodeUsed(ctx, sum("code-1")); !errors.Is(err, setup.ErrCodeUsed) {
		t.Errorf("use after a restart = %v, want ErrCodeUsed", err)
	}
	files.set("", "key")
	if err := k.CodeUsed(ctx, sum("code-1")); !errors.Is(err, setup.ErrCodeChanged) {
		t.Errorf("a deleted code = %v, want ErrCodeChanged", err)
	}
}

// TestFileKeeperDone covers AC-15: done deletes the setup files, writes the marker, can be called
// again, and later code use answers ErrDone.
func TestFileKeeperDone(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	k, _, dir := newFileKeeper(t, "code-1")
	for _, name := range []string{"code", "session-key", "code-used"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := k.Done(ctx); err != nil {
			t.Fatalf("Done = %v", err)
		}
	}
	for _, name := range []string{"code", "session-key", "code-used"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still there after Done: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".setup-done")); err != nil {
		t.Errorf(".setup-done missing: %v", err)
	}
	if err := k.CodeUsed(ctx, sum("code-1")); !errors.Is(err, setup.ErrDone) {
		t.Errorf("CodeUsed after Done = %v, want ErrDone", err)
	}
}

// failingRemove is a folder whose Remove of one name fails, like a disk error halfway through Done.
type failingRemove struct {
	dirFolder
	name string
}

func (f failingRemove) Remove(ctx context.Context, name string) error {
	if name == f.name {
		return errors.New("disk error")
	}
	return f.dirFolder.Remove(ctx, name)
}

// TestFileKeeperDoneCutShort: whichever removal fails, the marker is already written and the code
// file is still there, so Run calls Done again after a restart.
func TestFileKeeperDoneCutShort(t *testing.T) {
	t.Parallel()
	for _, failing := range []string{"session-key", "code-used", "code"} {
		t.Run(failing, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			dir := t.TempDir()
			for _, name := range []string{"code", "session-key", "code-used"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			files := &fakeFiles{}
			files.set("code-1", "key")
			if err := setup.NewFileKeeper(failingRemove{dirFolder{dir}, failing}, files).Done(ctx); err == nil {
				t.Fatal("Done with a failing remove = nil, want an error")
			}
			if _, err := os.Stat(filepath.Join(dir, ".setup-done")); err != nil {
				t.Errorf(".setup-done missing after a Done cut short: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "code")); err != nil {
				t.Errorf("code gone after a Done cut short, so Run would not retry: %v", err)
			}
			if err := setup.NewFileKeeper(dirFolder{dir}, files).Done(ctx); err != nil {
				t.Fatalf("Done again = %v", err)
			}
			for _, name := range []string{"code", "session-key", "code-used"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s still there after the retry: %v", name, err)
				}
			}
		})
	}
}

// TestFileKeeperNoEnroll covers AC-15: the file keeper has no enroll questions and refuses a run.
func TestFileKeeperNoEnroll(t *testing.T) {
	t.Parallel()
	k, _, _ := newFileKeeper(t, "c")
	if asks, err := k.Enroll(t.Context()); asks != nil || err != nil {
		t.Errorf("Enroll = %v, %v; want nil, nil", asks, err)
	}
	if err := k.RunEnroll(t.Context(), "s", nil); !errors.Is(err, setup.ErrNoEnroll) {
		t.Errorf("RunEnroll = %v, want ErrNoEnroll", err)
	}
}

// TestIssuerIdentity covers AC-16: the origin is the issuer's host, the path comes from the
// field, and an empty or absent path means no first admin page.
func TestIssuerIdentity(t *testing.T) {
	t.Parallel()
	issuer, _ := url.Parse("https://auth.platform.localhost:8443")
	path, empty := "/setup", ""
	for name, tc := range map[string]struct {
		path   *string
		wantOK bool
	}{"path": {&path, true}, "empty": {&empty, false}, "absent": {nil, false}} {
		fa, ok, err := setup.NewIssuerIdentity(issuer, tc.path).FirstAdmin(t.Context())
		if err != nil || ok != tc.wantOK {
			t.Errorf("%s: ok = %v, err = %v; want ok %v", name, ok, err, tc.wantOK)
		}
		if ok && (fa.Origin != "https://auth.platform.localhost:8443" || fa.Path != "/setup") {
			t.Errorf("%s: %+v", name, fa)
		}
	}
}
