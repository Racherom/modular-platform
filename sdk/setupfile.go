package sdk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// setupFiles is the folder of a module's lifetime: setup files (MP_SETUP_DIR) and their names.
type setupFiles struct {
	dir   string
	names []string
}

// SetupFile reads the lifetime: setup secret name (spec 0033) from the setup folder. The generated
// getters call it. The file is read at every call, because the keeper replaces it with a new code
// and deletes it when the setup is done; a missing or empty file is absent (false), never an
// error. Other read errors are errors, and no error text carries the file's content.
func SetupFile(ctx context.Context, v *Values, name string) (Secret, bool, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, false, err
	}
	if !slices.Contains(v.setup.names, name) {
		return Secret{}, false, fmt.Errorf("sdk: no setup file %s is declared", name)
	}
	//nolint:gosec // name is one of the entry's declared setup files, checked above; dir is the render's MP_SETUP_DIR
	data, err := os.ReadFile(filepath.Join(v.setup.dir, name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Secret{}, false, nil
	case err != nil:
		return Secret{}, false, fmt.Errorf("sdk: reading setup file %s: %w", name, err)
	}
	text := strings.TrimRight(string(data), " \t\r\n")
	if text == "" {
		return Secret{}, false, nil
	}
	return NewSecret(text), true, nil
}

// SetupFolder is a module's lifetime: setup folder (MP_SETUP_DIR) for the files the module keeps
// itself: the used code record, the done marker, and deleting the secrets once the setup is done
// (spec 0037, the interim setup holder). Only sdk touches the files; the generated Config hands
// one out as SetupFolder. A name is a plain file name, never a path.
type SetupFolder struct{ v *Values }

// NewSetupFolder returns the folder of v. The generated Config calls it.
func NewSetupFolder(v *Values) SetupFolder { return SetupFolder{v: v} }

// path joins a plain file name onto the folder.
func (f SetupFolder) path(name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("sdk: %q is not a plain setup file name", name)
	}
	return filepath.Join(f.v.setup.dir, name), nil
}

// Create writes name with mode 0600 and never replaces a file: when name exists the error wraps
// fs.ErrExist. The content goes to a temp file first and is linked into place, so a crash leaves
// no empty or half written name.
func (f SetupFolder) Create(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst, err := f.path(name)
	if err != nil {
		return err
	}
	tmp, err := writeTemp(filepath.Dir(dst), data)
	if err != nil {
		return fmt.Errorf("sdk: creating setup file %s: %w", name, err)
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, dst); err != nil {
		return fmt.Errorf("sdk: creating setup file %s: %w", name, err)
	}
	return nil
}

// Write replaces name (or makes it) with mode 0600 in one step: readers see the old or the new
// content, never a part.
func (f SetupFolder) Write(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst, err := f.path(name)
	if err != nil {
		return err
	}
	tmp, err := writeTemp(filepath.Dir(dst), data)
	if err != nil {
		return fmt.Errorf("sdk: writing setup file %s: %w", name, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sdk: writing setup file %s: %w", name, err)
	}
	return nil
}

// Remove deletes name; a missing file is not an error, so it is safe to call again.
func (f SetupFolder) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst, err := f.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("sdk: removing setup file %s: %w", name, err)
	}
	return nil
}

// Exists reports whether name is in the folder.
func (f SetupFolder) Exists(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	dst, err := f.path(name)
	if err != nil {
		return false, err
	}
	switch _, err := os.Lstat(dst); {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("sdk: checking setup file %s: %w", name, err)
	}
	return true, nil
}

// writeTemp writes data to a new 0600 temp file in dir and returns its path.
func writeTemp(dir string, data []byte) (string, error) {
	t, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return "", err
	}
	_, werr := t.Write(data)
	if cerr := t.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(t.Name())
		return "", werr
	}
	return t.Name(), nil
}
