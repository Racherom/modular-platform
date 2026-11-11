package render

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DefaultSourceRef is the ref a build without one uses: the default branch, which moves.
const DefaultSourceRef = "main"

// ErrSourceRef is returned for a ref that is neither a full commit nor a branch or tag name.
var ErrSourceRef = errors.New("source ref is neither a full 40 character commit nor a branch or tag name")

var (
	refName     = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	fullCommit  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	shortCommit = regexp.MustCompile(`^[0-9a-f]{4,39}$`)
)

// SourceRef is the git ref of this repository that a platform image builds from (spec 0030): a
// full commit, which addresses its content exactly, or a branch or tag name, which may move. It
// is written into setup.sh in single quotes, so the allowed form holds no quote, space or `#`.
type SourceRef struct {
	ref string
}

// NewSourceRef checks ref and returns it as a SourceRef; an empty ref means DefaultSourceRef. A
// short commit is refused, since a git build context cannot fetch one.
func NewSourceRef(ref string) (SourceRef, error) {
	if ref == "" {
		return SourceRef{ref: DefaultSourceRef}, nil
	}
	if !refName.MatchString(ref) || shortCommit.MatchString(ref) || strings.HasPrefix(ref, "-") {
		return SourceRef{}, fmt.Errorf("%q: %w", ref, ErrSourceRef)
	}
	return SourceRef{ref: ref}, nil
}

// String returns the ref as written; the zero SourceRef gives DefaultSourceRef.
func (r SourceRef) String() string {
	if r.ref == "" {
		return DefaultSourceRef
	}
	return r.ref
}

// Commit reports whether the ref is a full commit; any other ref is a branch or tag that can
// move, so a later build of the same zip may build different code.
func (r SourceRef) Commit() bool {
	return fullCommit.MatchString(r.ref)
}
