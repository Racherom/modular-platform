package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/distribution/reference"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// The lock files in a project's folder (spec 0031).
const (
	lockFile         = "images.lock"
	previousLockFile = "images.previous.lock"
)

// keepRepo starts every keep tag: localhost/mp-keep/<project>:<image ID hex>. A keep tag holds
// a locked image on the host so Docker never counts it as dangling.
const keepRepo = "localhost/mp-keep/"

// labelService names a container's Compose service; Compose sets it.
const labelService = "com.docker.compose.service"

// adoptedOperation is the operation an adopted lock names: no apply wrote it.
const adoptedOperation = "adopted"

// maxResolving is how many registry lookups one resolve call runs at once.
const maxResolving = 4

// ErrImageNotFound reports an image that is not on the host.
var ErrImageNotFound = errors.New("deploy: the image is not on this host")

// Image is an image on the host: its ID and the repo digests Docker recorded for it.
type Image struct {
	ID          string
	RepoDigests []string // repository@sha256:<hex>
}

// LockInvalidError reports a lock file that does not decode or breaks a rule.
type LockInvalidError struct {
	File string
	Err  error
}

func (e *LockInvalidError) Error() string { return fmt.Sprintf("deploy: %s: %v", e.File, e.Err) }

func (e *LockInvalidError) Unwrap() error { return e.Err }

// LockView is a lock as the images read answers it: each entry with whether its image is on the
// host under the name Compose looks up.
type LockView struct {
	Lock    contract.ImageLock
	Present []bool // Present[n] belongs to Lock.Images[n]
}

// StackImages is the images read of one project.
type StackImages struct {
	Adopted           bool
	Current, Previous *LockView
}

// keepTag is the keep tag of imageID in project.
func keepTag(project, imageID string) string {
	return keepRepo + project + ":" + strings.TrimPrefix(imageID, "sha256:")
}

// splitDigest splits a Compose image value into its reference and a trailing @sha256 digest.
func splitDigest(image string) (ref, digest string) {
	if i := strings.LastIndex(image, "@"); i >= 0 && contract.ValidImageDigest(image[i+1:]) {
		return image[:i], image[i+1:]
	}
	return image, ""
}

// repoDigest is the digest of the repoDigests entry whose repository is ref's, or "".
func repoDigest(ref string, repoDigests []string) string {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return ""
	}
	for _, rd := range repoDigests {
		other, err := reference.ParseNormalizedNamed(rd)
		if err != nil {
			continue
		}
		if c, ok := other.(reference.Canonical); ok && other.Name() == named.Name() {
			return c.Digest().String()
		}
	}
	return ""
}

// reserve holds project's operation lock while a read of its lock files runs, or returns
// ErrBusy; the returned func frees it.
func (e *Engine) reserve(project string) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy[project] {
		return nil, ErrBusy
	}
	e.busy[project] = true
	return func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		delete(e.busy, project)
	}, nil
}

// readLock decodes the lock file name of dir; nil when it does not exist.
func readLock(dir, name string) (*contract.ImageLock, error) {
	data, err := readFile(dir, name)
	if err != nil {
		return nil, &LockInvalidError{File: name, Err: err}
	}
	if data == nil {
		return nil, nil
	}
	l, err := contract.DecodeImageLock(data)
	if err != nil {
		return nil, &LockInvalidError{File: name, Err: err}
	}
	return &l, nil
}

// Images reads project's current and previous lock (spec 0031), each entry with whether its
// image is on the host. Without images.lock it adopts a lock from the project's containers. It
// returns ErrBusy while an operation of project runs, a *LockInvalidError for a broken file and
// ErrDocker when Docker does not answer.
func (e *Engine) Images(ctx context.Context, project string) (StackImages, error) {
	release, err := e.reserve(project)
	if err != nil {
		return StackImages{}, err
	}
	defer release()
	dir := e.projectDir(project)
	current, err := readLock(dir, lockFile)
	if err != nil {
		return StackImages{}, err
	}
	previous, err := readLock(dir, previousLockFile)
	if err != nil {
		return StackImages{}, err
	}
	var out StackImages
	if current == nil {
		adopted, err := e.adopt(ctx, project)
		if err != nil {
			return StackImages{}, err
		}
		if adopted != nil {
			present := make([]bool, len(adopted.Images))
			for n := range present {
				present[n] = true // adopted from running containers, so on the host
			}
			out.Adopted, out.Current = true, &LockView{Lock: *adopted, Present: present}
		}
	} else if out.Current, err = e.view(ctx, *current); err != nil {
		return StackImages{}, err
	}
	if previous != nil {
		if out.Previous, err = e.view(ctx, *previous); err != nil {
			return StackImages{}, err
		}
	}
	return out, nil
}

// view adds to l whether each image is on the host: reference@digest for a registry entry, the
// image ID for a built one, the names Compose looks up.
func (e *Engine) view(ctx context.Context, l contract.ImageLock) (*LockView, error) {
	v := &LockView{Lock: l, Present: make([]bool, len(l.Images))}
	for n, img := range l.Images {
		name := img.ImageID
		if img.Source == contract.ImageFromRegistry {
			name = img.Reference + "@" + img.Digest
		}
		_, err := e.docker.ImageInspect(ctx, name)
		switch {
		case err == nil:
			v.Present[n] = true
		case !errors.Is(err, ErrImageNotFound):
			return nil, errors.Join(ErrDocker, err)
		}
	}
	return v, nil
}

// adopt builds a lock from project's containers, stopped ones included (spec 0031, Adoption):
// the reference is each container's image without a digest, the image ID the one it runs, the
// digest its repo digest. A reference whose image has no repo digest, or whose containers run
// different images, is left out. Nil when nothing is left, or there is no container.
func (e *Engine) adopt(ctx context.Context, project string) (*contract.ImageLock, error) {
	containers, err := e.docker.Containers(ctx, project)
	if err != nil {
		return nil, errors.Join(ErrDocker, err)
	}
	ids := map[string]map[string]bool{}
	stack := ""
	for _, c := range containers {
		ref, _ := splitDigest(c.Image)
		if ids[ref] == nil {
			ids[ref] = map[string]bool{}
		}
		ids[ref][c.ImageID] = true
		if s := c.Labels[render.LabelStack]; s != "" {
			stack = s
		}
	}
	var entries []contract.LockedImage
	left := 0
	for _, ref := range slices.Sorted(maps.Keys(ids)) {
		if len(ids[ref]) != 1 || contract.CheckLockReference(ref) != nil {
			left++
			continue
		}
		var id string
		for k := range ids[ref] {
			id = k
		}
		img, err := e.docker.ImageInspect(ctx, id)
		if err != nil && !errors.Is(err, ErrImageNotFound) {
			return nil, errors.Join(ErrDocker, err)
		}
		d := repoDigest(ref, img.RepoDigests)
		if d == "" {
			left++
			continue
		}
		entries = append(entries, contract.LockedImage{Reference: ref, Source: contract.ImageFromRegistry, Digest: d, ImageID: id})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	l, err := contract.NewImageLock(stack, adoptedOperation, e.now(), entries)
	if err != nil {
		// a container without a valid stack label: nothing to adopt
		e.log.WarnContext(ctx, "lock not adopted", slog.String("project", project), slog.Any("error", err))
		return nil, nil
	}
	e.log.InfoContext(ctx, "lock adopted", slog.String("project", project), slog.Int("count", len(entries)), slog.Int("left_out", left))
	return &l, nil
}

// adoptBeforePull writes the adopted lock as images.lock and keep tags its images when project
// has containers but no images.lock (spec 0031, AC-9), so a rollback of this apply starts the
// images that ran before it. Run in prepare; a failure is logged, the apply goes on.
func (e *Engine) adoptBeforePull(dir, project string) {
	current, err := readLock(dir, lockFile)
	if err != nil || current != nil {
		return
	}
	adopted, err := e.adopt(e.ctx, project)
	if err != nil || adopted == nil {
		if err != nil {
			e.log.ErrorContext(e.ctx, "lock write failed", slog.String("project", project), slog.String("step", "adopt"), slog.Any("error", err))
		}
		return
	}
	for _, img := range adopted.Images {
		if err := e.docker.ImageTag(e.ctx, img.ImageID, keepTag(project, img.ImageID)); err != nil {
			e.log.ErrorContext(e.ctx, "lock write failed", slog.String("project", project), slog.String("step", "tag"), slog.Any("error", err))
			return
		}
	}
	if err := e.writeLock(dir, lockFile, *adopted); err != nil {
		e.log.ErrorContext(e.ctx, "lock write failed", slog.String("project", project), slog.String("step", "adopt"), slog.Any("error", err))
	}
}

// writeLock writes l as the file name of dir: a temp file in the same folder, then a rename.
func (e *Engine) writeLock(dir, name string, l contract.ImageLock) error {
	data, err := l.Encode()
	if err != nil {
		return err
	}
	return e.writeAtomic(dir, name, data)
}

func (e *Engine) writeAtomic(dir, name string, data []byte) error {
	tmp := "." + name + ".tmp"
	if err := e.writeFile(dir, tmp, data, 0o644, false); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, tmp), filepath.Join(dir, name))
}

// lockStepError names the step of the lock bookkeeping that failed.
type lockStepError struct {
	step string
	err  error
}

func (e *lockStepError) Error() string { return e.step + ": " + e.err.Error() }

func (e *lockStepError) Unwrap() error { return e.err }

// recordLock runs after up succeeded (spec 0031, "Order after up succeeds"): it builds the lock
// of req's project from its Compose file and containers, keep tags every image, rotates the
// previous lock when the entries changed, writes images.lock and removes the project's other
// keep tags. A failure is logged as lock write failed; the operation still succeeds.
func (e *Engine) recordLock(op *Operation, dir string, req ApplyRequest) {
	err := e.lockAfterUp(op, dir, req)
	if err == nil {
		return
	}
	step := "build"
	if s, ok := errors.AsType[*lockStepError](err); ok {
		step = s.step
	}
	e.log.ErrorContext(e.ctx, "lock write failed", slog.String("project", req.Project), slog.String("operation", op.ID),
		slog.String("step", step), slog.Any("error", err))
}

func (e *Engine) lockAfterUp(op *Operation, dir string, req ApplyRequest) error {
	fail := func(step string, err error) error { return &lockStepError{step: step, err: err} }
	next, err := e.buildLock(op.ID, req)
	if err != nil {
		return fail("build", err)
	}
	for _, img := range next.Images {
		if err := e.docker.ImageTag(e.ctx, img.ImageID, keepTag(req.Project, img.ImageID)); err != nil {
			return fail("tag", err)
		}
	}
	current, err := readLock(dir, lockFile)
	if err != nil {
		e.log.WarnContext(e.ctx, "lock invalid", slog.String("project", req.Project), slog.String("file", lockFile), slog.Any("error", err))
		current = nil // replaced by the lock of this apply, never rotated
	}
	previous, err := readLock(dir, previousLockFile)
	if err != nil {
		e.log.WarnContext(e.ctx, "lock invalid", slog.String("project", req.Project), slog.String("file", previousLockFile), slog.Any("error", err))
		previous = nil
	}
	if current != nil && !current.SameImages(next) {
		if err := e.writeLock(dir, previousLockFile, *current); err != nil {
			return fail("rotate", err)
		}
		previous = current
	}
	if err := e.writeLock(dir, lockFile, next); err != nil {
		return fail("write", err)
	}
	e.log.InfoContext(e.ctx, "images frozen", slog.String("project", req.Project), slog.String("operation", op.ID), slog.Int("count", len(next.Images)))
	keep := map[string]bool{}
	for _, l := range []*contract.ImageLock{&next, previous} {
		if l == nil {
			continue
		}
		for _, img := range l.Images {
			keep[keepTag(req.Project, img.ImageID)] = true
		}
	}
	if err := e.untagOthers(req.Project, keep); err != nil {
		return fail("untag", err)
	}
	return nil
}

// untagOthers removes every keep tag of project not in keep.
func (e *Engine) untagOthers(project string, keep map[string]bool) error {
	tags, err := e.docker.ImageTags(e.ctx, keepRepo+project)
	if err != nil {
		return err
	}
	removed := 0
	for _, t := range tags {
		if keep[t] {
			continue
		}
		if err := e.docker.ImageUntag(e.ctx, t); err != nil && !errors.Is(err, ErrImageNotFound) {
			return err
		}
		removed++
	}
	if removed > 0 {
		e.log.InfoContext(e.ctx, "keep tags removed", slog.String("project", project), slog.Int("count", removed))
	}
	return nil
}

// buildLock is the lock of what up just started for req: one entry per distinct image of its
// Compose file, the jobs and mp_jobs included. The digest is the one the file names, else the
// image's repo digest; the image ID is the one its containers run, else the image's.
func (e *Engine) buildLock(opID string, req ApplyRequest) (contract.ImageLock, error) {
	var compose, env []byte
	for _, f := range req.Files {
		switch f.Path {
		case "docker-compose.yml":
			compose = f.Data
		case ".env":
			env = f.Data
		}
	}
	project, err := render.LoadProject(e.ctx, compose, env, e.deployEnv())
	if err != nil {
		return contract.ImageLock{}, err
	}
	containers, err := e.docker.Containers(e.ctx, req.Project)
	if err != nil {
		return contract.ImageLock{}, err
	}
	running := map[string]map[string]bool{} // service -> image IDs
	for _, c := range containers {
		svc := c.Labels[labelService]
		if running[svc] == nil {
			running[svc] = map[string]bool{}
		}
		running[svc][c.ImageID] = true
	}
	type use struct {
		image    string
		services []string
	}
	refs := map[string]*use{}
	for svc, image := range render.ServiceImages(project) {
		ref, _ := splitDigest(image)
		if refs[ref] == nil {
			refs[ref] = &use{image: image}
		}
		refs[ref].services = append(refs[ref].services, svc)
	}
	entries := make([]contract.LockedImage, 0, len(refs))
	for ref, u := range refs {
		_, digest := splitDigest(u.image)
		ids := map[string]bool{}
		for _, svc := range u.services {
			for id := range running[svc] {
				ids[id] = true
			}
		}
		var id string
		if len(ids) == 1 {
			for k := range ids {
				id = k
			}
		}
		var img Image
		if id == "" || digest == "" {
			if img, err = e.docker.ImageInspect(e.ctx, cmpOr(id, u.image)); err != nil {
				return contract.ImageLock{}, fmt.Errorf("inspect %s: %w", u.image, err)
			}
			id = cmpOr(id, img.ID)
		}
		entry := contract.LockedImage{Reference: ref, Source: contract.ImageFromRegistry, Digest: digest, ImageID: id}
		if digest == "" {
			entry.Digest = repoDigest(ref, img.RepoDigests)
		}
		if entry.Digest == "" {
			entry.Source = contract.ImageBuilt
		}
		entries = append(entries, entry)
	}
	return contract.NewImageLock(req.Stack, opID, e.now(), entries)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// removeLocks deletes project's lock files and every keep tag of the project (spec 0031,
// AC-11).
func (e *Engine) removeLocks(dir, project string) error {
	errs := []error{removeFile(dir, lockFile), removeFile(dir, previousLockFile)}
	errs = append(errs, e.untagOthers(project, nil))
	return errors.Join(errs...)
}

// Unresolved is a reference a resolve call found no digest for, at its place in the request.
type Unresolved struct {
	Index             int
	Reference, Reason string
}

// Resolved is a resolve call's answer: the digests found, sorted by reference, and the
// references that failed, in request order.
type Resolved struct {
	Images     []contract.ImagePin
	Unresolved []Unresolved
}

// ResolveError is how the container engine failed one registry lookup.
type ResolveError struct {
	Kind ResolveFailure
	Err  error
}

// ResolveFailure sorts a failed lookup so its reason reads in plain words.
type ResolveFailure int

// The lookup failures.
const (
	ResolveOther        ResolveFailure = iota // the registry could not be reached or failed
	ResolveNotFound                           // the registry has no such tag
	ResolveUnauthorized                       // the registry needs a login
	ResolveNoEngine                           // the container engine did not answer
)

func (e *ResolveError) Error() string { return e.Err.Error() }

func (e *ResolveError) Unwrap() error { return e.Err }

// Resolve asks the registry, through the container engine, which digest each of refs names now,
// without pulling (spec 0031). At most 4 lookups run at once, and the call ends after the resolve
// timeout: a reference still open then is unresolved with reason timeout. It returns ErrDocker
// when the container engine does not answer.
func (e *Engine) Resolve(ctx context.Context, refs []string) (Resolved, error) {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.ResolveTimeout)
	defer cancel()
	digests := make([]string, len(refs))
	errs := make([]error, len(refs))
	sem := make(chan struct{}, maxResolving)
	var wg sync.WaitGroup
	for n, ref := range refs {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[n] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			digests[n], errs[n] = e.docker.DistributionDigest(ctx, ref)
		})
	}
	wg.Wait()
	var out Resolved
	seen := map[string]bool{}
	for n, ref := range refs {
		err := errs[n]
		if err == nil && !contract.ValidImageDigest(digests[n]) {
			err = fmt.Errorf("the registry answered the digest %q", digests[n])
		}
		if err == nil {
			if !seen[ref] {
				out.Images = append(out.Images, contract.ImagePin{Reference: ref, Digest: digests[n]})
				seen[ref] = true
			}
			continue
		}
		if re, ok := errors.AsType[*ResolveError](err); ok && re.Kind == ResolveNoEngine {
			return Resolved{}, errors.Join(ErrDocker, err)
		}
		reason := resolveReason(ctx, err)
		e.log.WarnContext(ctx, "image not resolved", slog.String("reference", ref), slog.String("reason", reason))
		out.Unresolved = append(out.Unresolved, Unresolved{Index: n, Reference: ref, Reason: reason})
	}
	slices.SortFunc(out.Images, func(a, b contract.ImagePin) int { return strings.Compare(a.Reference, b.Reference) })
	return out, nil
}

// resolveReason says in plain words why a lookup failed.
func resolveReason(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return "timeout: the registry did not answer within the resolve timeout"
	}
	re, _ := errors.AsType[*ResolveError](err)
	switch {
	case re != nil && re.Kind == ResolveNotFound:
		return "the registry has no image with this tag"
	case re != nil && re.Kind == ResolveUnauthorized:
		return "the registry needs a login, and the platform cannot log in to registries yet"
	}
	return "the registry could not be reached: " + err.Error()
}
