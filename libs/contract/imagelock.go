package contract

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
)

// LockFormatVersion is the only formatVersion of an image lock this release reads and writes
// (spec 0031).
const LockFormatVersion = 1

// CodeInvalidLock refuses a lock given to the managed render, or a lock file that breaks a rule
// (spec 0031).
const CodeInvalidLock = "invalid_lock"

// maxReference is the longest image reference a lock or a resolve call takes.
const maxReference = 255

// ImageSource tells where a locked image came from.
type ImageSource string

// The image sources.
const (
	// ImageFromRegistry is an image with a registry digest.
	ImageFromRegistry ImageSource = "registry"
	// ImageBuilt is an image with no repo digest, such as one built on the host; it is frozen
	// by its image ID only.
	ImageBuilt ImageSource = "built"
)

// LockedImage is one entry of an image lock: a reference frozen to a digest and the image ID
// Docker ran for it.
type LockedImage struct {
	Reference string      `json:"reference"`
	Source    ImageSource `json:"source"`
	Digest    string      `json:"digest,omitempty"`
	ImageID   string      `json:"imageId"`
}

// ImageLock is the list of image references a stack runs, each frozen to a digest and an image
// ID (spec 0031). Build it with NewImageLock or DecodeImageLock; it is then treated as immutable.
type ImageLock struct {
	FormatVersion int           `json:"formatVersion"`
	Stack         string        `json:"stack"`
	Operation     string        `json:"operation"`
	WrittenAt     time.Time     `json:"writtenAt"`
	Images        []LockedImage `json:"images"`
}

// ErrInvalidLock is wrapped by every error about an image lock that breaks a rule.
var ErrInvalidLock = errors.New("invalid image lock")

// NewImageLock builds an image lock for stack, written by operation at writtenAt (kept in UTC),
// with images sorted by reference. It checks every rule of the lock format.
func NewImageLock(stack, operation string, writtenAt time.Time, images []LockedImage) (ImageLock, error) {
	l := ImageLock{
		FormatVersion: LockFormatVersion,
		Stack:         stack,
		Operation:     operation,
		WrittenAt:     writtenAt.UTC(),
		Images:        slices.Clone(images),
	}
	slices.SortFunc(l.Images, func(a, b LockedImage) int { return strings.Compare(a.Reference, b.Reference) })
	if err := l.validate(); err != nil {
		return ImageLock{}, err
	}
	return l, nil
}

// DecodeImageLock reads a lock file strictly: unknown fields, a wrong formatVersion, unsorted or
// repeated references and any other broken rule are an error wrapping ErrInvalidLock.
func DecodeImageLock(data []byte) (ImageLock, error) {
	var l ImageLock
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return ImageLock{}, fmt.Errorf("%w: %w", ErrInvalidLock, err)
	}
	if dec.More() {
		return ImageLock{}, fmt.Errorf("%w: data after the lock", ErrInvalidLock)
	}
	if err := l.validate(); err != nil {
		return ImageLock{}, err
	}
	return l, nil
}

// Encode is the lock file content: indented JSON and a final newline.
func (l ImageLock) Encode() ([]byte, error) {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode image lock: %w", err)
	}
	return append(data, '\n'), nil
}

func (l ImageLock) validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidLock, fmt.Sprintf(format, args...))
	}
	switch {
	case l.FormatVersion != LockFormatVersion:
		return bad("formatVersion %d, want %d", l.FormatVersion, LockFormatVersion)
	case !ValidStackID(l.Stack):
		return bad("stack %q is not a uuid", l.Stack)
	case l.Operation == "":
		return bad("operation is required")
	case l.WrittenAt.IsZero():
		return bad("writtenAt is required")
	case len(l.Images) == 0:
		return bad("images needs at least one entry")
	}
	for n, e := range l.Images {
		if n > 0 && l.Images[n-1].Reference >= e.Reference {
			return bad("images must be sorted by reference, each once: %q", e.Reference)
		}
		if err := CheckLockReference(e.Reference); err != nil {
			return bad("images[%d]: %v", n, err)
		}
		if !ValidImageDigest(e.ImageID) {
			return bad("images[%d]: imageId %q is not sha256 and 64 hex", n, e.ImageID)
		}
		switch e.Source {
		case ImageFromRegistry:
			if !ValidImageDigest(e.Digest) {
				return bad("images[%d]: digest %q is not sha256 and 64 hex", n, e.Digest)
			}
		case ImageBuilt:
			if e.Digest != "" {
				return bad("images[%d]: a built image has no digest", n)
			}
		default:
			return bad("images[%d]: source %q is not registry or built", n, e.Source)
		}
	}
	return nil
}

// Find is the entry of reference ref, if the lock has one.
func (l ImageLock) Find(ref string) (LockedImage, bool) {
	n, ok := slices.BinarySearchFunc(l.Images, ref, func(e LockedImage, ref string) int { return strings.Compare(e.Reference, ref) })
	if !ok {
		return LockedImage{}, false
	}
	return l.Images[n], true
}

// SameImages reports whether l and o freeze the same images: equal references, sources, digests
// and image IDs. Who wrote them and when does not count.
func (l ImageLock) SameImages(o ImageLock) bool {
	return slices.Equal(l.Images, o.Images)
}

// ValidImageDigest reports whether s is `sha256:` and 64 lowercase hex, the form of a registry
// digest and of an image ID.
func ValidImageDigest(s string) bool {
	h, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil && strings.ToLower(h) == h
}

// ValidStackID reports whether s is a uuid in its canonical lowercase form.
func ValidStackID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for n, r := range s {
		switch n {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
	}
	return true
}

// CheckLockReference returns why s is not a lock key, an image reference with a tag, no digest
// and at most 255 characters, or nil.
func CheckLockReference(s string) error {
	if len(s) > maxReference {
		return fmt.Errorf("reference is longer than %d characters", maxReference)
	}
	ref, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return fmt.Errorf("%q is not an image reference: %w", s, err)
	}
	if _, ok := ref.(reference.Digested); ok {
		return fmt.Errorf("%q carries a digest", s)
	}
	if _, ok := ref.(reference.Tagged); !ok {
		return fmt.Errorf("%q has no tag", s)
	}
	return nil
}

// ImagePinProblems lists why pins are not a lock the managed render takes, one invalid_lock
// error at lock[<n>] per bad entry: a reference that is not a lock key, a digest that is not
// sha256 and 64 hex, or a reference listed twice.
func ImagePinProblems(pins []ImagePin) []Problem {
	var out []Problem
	seen := map[string]bool{}
	for n, p := range pins {
		var msg string
		switch err := CheckLockReference(p.Reference); {
		case err != nil:
			msg = err.Error()
		case !ValidImageDigest(p.Digest):
			msg = fmt.Sprintf("digest %q of %s is not sha256 and 64 hex", p.Digest, p.Reference)
		case seen[p.Reference]:
			msg = p.Reference + " is in the lock twice"
		}
		seen[p.Reference] = true
		if msg != "" {
			out = append(out, Problem{Severity: Error, Code: CodeInvalidLock, Path: fmt.Sprintf("lock[%d]", n), Message: msg})
		}
	}
	return out
}

// ImageChange tells how a frozen image's digest relates to the stack's current lock.
type ImageChange string

// The image changes.
const (
	// ChangeFrozen keeps the digest of the current lock.
	ChangeFrozen ImageChange = "frozen"
	// ChangeNew is a reference the current lock has no digest for.
	ChangeNew ImageChange = "new"
	// ChangeUpdated replaces the digest of the current lock with the registry's.
	ChangeUpdated ImageChange = "updated"
)

// FrozenImage is one reference with the digest an apply runs. PreviousDigest is set for
// ChangeUpdated only.
type FrozenImage struct {
	Reference      string
	Digest         string
	Change         ImageChange
	PreviousDigest string
}

// UnresolvedError lists the references Freeze needed a registry digest for and got none.
type UnresolvedError struct {
	References []string // sorted
}

func (e *UnresolvedError) Error() string {
	return "freeze: no digest for " + strings.Join(e.References, ", ")
}

// frozenDigest is the digest of ref in current when it can stay frozen: a registry entry with a
// digest, and ref not in update.
func frozenDigest(ref string, current []LockedImage, update []string) (string, bool) {
	if slices.Contains(update, ref) {
		return "", false
	}
	for _, e := range current {
		if e.Reference == ref && e.Source == ImageFromRegistry && e.Digest != "" {
			return e.Digest, true
		}
	}
	return "", false
}

// ToResolve lists, sorted and once each, the references of references that Freeze takes from the
// registry (rule 2 of spec 0031): new to current, in update, built, or with no digest.
func ToResolve(references []string, current []LockedImage, update []string) []string {
	var out []string
	for _, ref := range references {
		if _, ok := frozenDigest(ref, current, update); !ok {
			out = append(out, ref)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Freeze gives each of references (spec 0031, "the freeze rule") its digest: the current lock's
// when the reference is there with a digest and not in update (frozen), else resolved's (new, or
// updated when current had a digest for it). It is sorted by reference, each once. A reference
// that needs resolved and is missing from it is an *UnresolvedError.
func Freeze(references []string, current []LockedImage, resolved map[string]string, update []string) ([]FrozenImage, error) {
	refs := slices.Compact(slices.Sorted(slices.Values(references)))
	out := make([]FrozenImage, 0, len(refs))
	var missing []string
	for _, ref := range refs {
		if d, ok := frozenDigest(ref, current, update); ok {
			out = append(out, FrozenImage{Reference: ref, Digest: d, Change: ChangeFrozen})
			continue
		}
		d, ok := resolved[ref]
		if !ok {
			missing = append(missing, ref)
			continue
		}
		f := FrozenImage{Reference: ref, Digest: d, Change: ChangeNew}
		if prev, had := frozenDigest(ref, current, nil); had {
			f.Change, f.PreviousDigest = ChangeUpdated, prev
		}
		out = append(out, f)
	}
	if len(missing) > 0 {
		return nil, &UnresolvedError{References: missing}
	}
	return out, nil
}

// Pins is the lock the managed render takes for frozen, in its order.
func Pins(frozen []FrozenImage) []ImagePin {
	out := make([]ImagePin, 0, len(frozen))
	for _, f := range frozen {
		out = append(out, ImagePin{Reference: f.Reference, Digest: f.Digest})
	}
	return out
}

// ReviewDigest is `sha256:<hex>` over checkDigest, a newline, and the canonical JSON of pins
// sorted by reference (spec 0031): the review and the apply of one graph and one frozen lock
// give the same value.
func ReviewDigest(checkDigest string, pins []ImagePin) string {
	sorted := slices.SortedFunc(slices.Values(pins), func(a, b ImagePin) int {
		return cmp.Compare(a.Reference, b.Reference)
	})
	type pin struct {
		Reference string `json:"reference"`
		Digest    string `json:"digest"`
	}
	canon := make([]pin, 0, len(sorted))
	for _, p := range sorted {
		canon = append(canon, pin{Reference: p.Reference, Digest: p.Digest})
	}
	data, err := json.Marshal(canon)
	if err != nil {
		panic(err) // a slice of string pairs always marshals
	}
	h := sha256.New()
	h.Write([]byte(checkDigest))
	h.Write([]byte{'\n'})
	h.Write(data)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
