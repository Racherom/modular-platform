// Package callerkey is `api caller-key generate`, the api entry's setup step (spec 0015): it
// writes the key pair api signs MP-Caller tokens with into the step's output folder. It runs in
// the api image, which has no shell, as you and with no network, before any config exists, so it
// runs before sdk.Run and reads nothing but its arguments.
package callerkey

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/racherom/modular-platform/sdk/caller"
)

// The files the step writes, named as the api entry's secrets.
const (
	SigningFile = "caller-signing-key"
	PublicFile  = "caller-public-key"
)

// Command is the first argument that runs Run instead of the module.
const Command = "caller-key"

// Run carries out `caller-key generate [<folder>]`: a new Ed25519 pair, the signing key into
// <folder>/caller-signing-key (0600) and the public key into <folder>/caller-public-key (0644).
// The folder defaults to /out, where a setup step's output folder is mounted. It never replaces
// a file, so a file left by an earlier run is an error.
func Run(args []string) error {
	if len(args) == 0 || args[0] != "generate" || len(args) > 2 {
		return errors.New("usage: caller-key generate [<folder>]")
	}
	dir := "/out"
	if len(args) == 2 {
		dir = args[1]
	}
	signing, public, err := caller.GenerateKeyPair()
	if err != nil {
		return err
	}
	if err := write(filepath.Join(dir, SigningFile), signing, 0o600); err != nil {
		return err
	}
	return write(filepath.Join(dir, PublicFile), public, 0o644)
}

// write creates path with data and mode, refusing a path that exists.
func write(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // the step's own output folder
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
