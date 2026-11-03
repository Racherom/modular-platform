package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// errExists reports a secret file that is already there; secrets are never overwritten unless
// a placed value asks to replace one.
var errExists = errors.New("deploy: the secret exists")

// newSecret is 32 random bytes as hex, like setup.sh makes them.
func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	return hex.EncodeToString(b)
}

// ensureDir makes dir with mode and the platform owner, below parent, refusing a symbolic link
// or a non folder in its place.
func (e *Engine) ensureDir(parent, dir string, mode fs.FileMode) error {
	if !strings.HasPrefix(dir, parent+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside %s", dir, parent)
	}
	info, err := os.Lstat(dir)
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s: %w", dir, errSymlink)
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s is not a folder", dir)
	case err == nil:
		return nil
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := os.Mkdir(dir, mode); err != nil {
		return err
	}
	if err := os.Chmod(dir, mode); err != nil { // the umask may have narrowed it
		return err
	}
	return os.Lchown(dir, e.cfg.UID, e.cfg.GID)
}

var errSymlink = errors.New("a symbolic link is refused")

// writeFile writes rel below the project folder dir with mode and the platform owner. Every
// folder on the way is checked, and the file itself is opened without following a link. With
// excl it never replaces an existing file (errExists).
func (e *Engine) writeFile(dir, rel string, data []byte, mode fs.FileMode, excl bool) error {
	parent := dir
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		next := filepath.Join(parent, p)
		dirMode := fs.FileMode(0o755)
		if p == "secrets" || parent != dir && filepath.Base(parent) == "secrets" {
			dirMode = 0o700
		}
		if err := e.ensureDir(parent, next, dirMode); err != nil {
			return err
		}
		parent = next
	}
	flags := os.O_WRONLY | os.O_CREATE | syscall.O_NOFOLLOW
	if excl {
		flags |= os.O_EXCL
	} else {
		flags |= os.O_TRUNC
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	f, err := os.OpenFile(path, flags, mode) //nolint:gosec // rel matched the project's file or secret patterns, every folder on the way was checked, and O_NOFOLLOW refuses a link
	if errors.Is(err, fs.ErrExist) {
		return errExists
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return fmt.Errorf("%s: %w", rel, errSymlink)
		}
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return os.Lchown(path, e.cfg.UID, e.cfg.GID)
}

// writeSecret writes a secret file, mode 0600, never replacing one unless replace is set.
func (e *Engine) writeSecret(dir, rel string, data []byte, replace bool) error {
	if !secretPattern.MatchString(rel) {
		return fmt.Errorf("%s: %w", rel, errNotSecretDir)
	}
	return e.writeFile(dir, rel, data, 0o600, !replace)
}

// fileExists reports whether the regular file rel exists below dir, refusing a link on the way.
func fileExists(dir, rel string) (bool, error) {
	path := dir
	for p := range strings.SplitSeq(rel, "/") {
		path = filepath.Join(path, p)
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return false, errSymlink
		}
	}
	return true, nil
}

// readFile reads the file rel below dir, refusing a link on the way; nil when it does not exist.
func readFile(dir, rel string) ([]byte, error) {
	exists, err := fileExists(dir, rel)
	if err != nil || !exists {
		return nil, err
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) //nolint:gosec // every part of rel was checked for links, and O_NOFOLLOW refuses the last
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if data == nil {
		data = []byte{} // an empty file exists: not nil
	}
	return data, nil
}

// removeFile deletes the file rel below dir, refusing a link on the way; a missing one is fine.
func removeFile(dir, rel string) error {
	if _, err := fileExists(dir, rel); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Secrets lists the secret files of project that exist, as secrets/<instance>/<name>, sorted.
// Links are skipped; content is never read.
func (e *Engine) Secrets(project string) ([]string, error) {
	root := filepath.Join(e.projectDir(project), "secrets")
	instances, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	present := []string{}
	for _, i := range instances {
		if i.Type()&fs.ModeSymlink != 0 || !i.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, i.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if rel := "secrets/" + i.Name() + "/" + f.Name(); f.Type().IsRegular() && secretPattern.MatchString(rel) {
				present = append(present, rel)
			}
		}
	}
	slices.Sort(present)
	return present, nil
}
