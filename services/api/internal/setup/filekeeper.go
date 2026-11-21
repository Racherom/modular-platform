package setup

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
)

// This file is the interim Keeper of spec 0037: before the keeper of #32 exists, the setup holder is
// a folder (MP_SETUP_DIR) that setup.sh fills and api reads and writes. It goes away with #32,
// whose client replaces FileKeeper behind the same Keeper port. It never logs the code, a hash or
// a file's content.

// Names of the files FileKeeper keeps beside the code and the session key.
const (
	codeUsedFile  = "code-used"
	setupDoneFile = ".setup-done"
)

// Folder is the sdk's setup folder as FileKeeper needs it (sdk.SetupFolder is the adapter).
type Folder interface {
	// Create writes name and never replaces it: an existing file wraps fs.ErrExist.
	Create(ctx context.Context, name string, data []byte) error
	// Write replaces name in one step.
	Write(ctx context.Context, name string, data []byte) error
	// Remove deletes name; a missing file is not an error.
	Remove(ctx context.Context, name string) error
	// Exists reports whether name is there.
	Exists(ctx context.Context, name string) (bool, error)
}

// FileKeeper is the file based Keeper. The identity module has no enroll block here: the first
// admin makes the account on the provider's own page.
type FileKeeper struct {
	folder Folder
	files  Files
}

var _ Keeper = (*FileKeeper)(nil)

// NewFileKeeper returns a Keeper that keeps its records in folder and compares the code it is
// given with the one files reads.
func NewFileKeeper(folder Folder, files Files) *FileKeeper {
	return &FileKeeper{folder: folder, files: files}
}

// Enroll answers nil: no enroll questions.
func (*FileKeeper) Enroll(context.Context) ([]Ask, error) { return nil, nil }

// RunEnroll answers ErrNoEnroll.
func (*FileKeeper) RunEnroll(context.Context, string, map[string]string) error { return ErrNoEnroll }

// CodeUsed records the code as used. A hash that is not the current code's answers ErrCodeChanged;
// a code that was recorded before answers ErrCodeUsed. The record is written to a temp file and
// linked into place, so a crash leaves no empty record, and a restart of api keeps the code refused.
func (k *FileKeeper) CodeUsed(ctx context.Context, codeSHA256 string) error {
	if done, err := k.folder.Exists(ctx, setupDoneFile); err != nil {
		return fmt.Errorf("checking the done marker: %w", err)
	} else if done {
		return ErrDone
	}
	code, ok, err := k.files.Code(ctx)
	if err != nil {
		return fmt.Errorf("reading the setup code: %w", err)
	}
	current := sha256.Sum256([]byte(code))
	if !ok || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(current[:])), []byte(codeSHA256)) != 1 {
		return ErrCodeChanged
	}
	switch err := k.folder.Create(ctx, codeUsedFile, []byte(codeSHA256)); {
	case errors.Is(err, fs.ErrExist):
		return ErrCodeUsed
	case err != nil:
		return fmt.Errorf("recording the used code: %w", err)
	}
	return nil
}

// Done writes the done marker, then deletes the session key, the used record and the code. It is
// safe to call again. The code goes last: while any setup file is left, the code file is still
// there, so Run calls Done again after a restart and the marker is never missing.
func (k *FileKeeper) Done(ctx context.Context) error {
	if err := k.folder.Write(ctx, setupDoneFile, nil); err != nil {
		return fmt.Errorf("writing the done marker: %w", err)
	}
	for _, name := range []string{"session-key", codeUsedFile, "code"} {
		if err := k.folder.Remove(ctx, name); err != nil {
			return fmt.Errorf("deleting the setup files: %w", err)
		}
	}
	return nil
}
