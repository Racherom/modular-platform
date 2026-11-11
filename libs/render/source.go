package render

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
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

const (
	// PlatformImages starts the images of the platform's own modules.
	PlatformImages = "ghcr.io/racherom/modular-platform/"
	// UnreleasedTag is the tag the platform's own catalog entries carry until the first release
	// rewrites it; no registry holds an image with it.
	UnreleasedTag = "0.0.0"
)

// Unreleased reports whether image is one of the platform's own images that no registry holds
// yet: it starts with PlatformImages and carries exactly UnreleasedTag. A zip that names one
// builds it on the host instead of pulling it (spec 0030).
func Unreleased(image string) bool {
	name, ok := strings.CutPrefix(image, PlatformImages)
	if !ok {
		return false
	}
	_, tag, _ := strings.Cut(name, ":")
	return tag == UnreleasedTag
}

// options are the settings of one render.
type options struct {
	source      SourceRef
	dockerfiles map[string][]byte // module name to its Dockerfile; nil without WithSource
}

// Option changes one render.
type Option func(*options)

// WithSource lets a render build the unreleased platform images it names (spec 0030): each such
// module's Dockerfile, from dockerfiles by module name, goes into the zip as
// build/<module>/Dockerfile, and setup.sh builds the image from this repository at ref before
// anything else. Without it a render names those images and builds nothing. A placed module whose
// image is unreleased and that has no entry in dockerfiles fails the render.
func WithSource(ref SourceRef, dockerfiles map[string][]byte) Option {
	return func(o *options) {
		o.source = ref
		o.dockerfiles = dockerfiles
	}
}

// imageBuild is one image setup.sh builds before anything else.
type imageBuild struct {
	Module string // the module name, which names its folder under build/
	Image  string // the catalog image, the tag the build gets
}

// Dockerfile is the path of the build's Dockerfile inside the zip.
func (b imageBuild) Dockerfile() string { return "build/" + b.Module + "/Dockerfile" }

// imageBuilds collects, once per module, the unreleased platform images of the placed modules,
// sorted by module name, and the Dockerfile file of each; in the platform flavor each of their
// services gets pull_policy: never. It does nothing without WithSource.
func (r *renderer) imageBuilds(opts []Option) ([]File, error) {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.dockerfiles == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var files []File
	for i, gm := range r.g.Modules {
		m := r.modules[gm.Module]
		if gm.External != nil || !Unreleased(m.Image) {
			continue
		}
		if r.flavor == flavorPlatform {
			svc := r.project.Services[i]
			svc.PullPolicy = types.PullPolicyNever
			r.project.Services[i] = svc
		}
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		data, ok := o.dockerfiles[m.Name]
		if !ok {
			return nil, fmt.Errorf("render: module %s names the unreleased image %s and has no Dockerfile", m.Name, m.Image)
		}
		b := imageBuild{Module: m.Name, Image: m.Image}
		r.data.Builds = append(r.data.Builds, b)
		files = append(files, File{Path: b.Dockerfile(), Mode: 0o644, Data: data})
	}
	slices.SortFunc(r.data.Builds, func(a, b imageBuild) int { return strings.Compare(a.Module, b.Module) })
	if len(r.data.Builds) > 0 {
		r.data.SourceRef = o.source.String()
		r.data.SourceMoves = !o.source.Commit()
	}
	return files, nil
}
