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
