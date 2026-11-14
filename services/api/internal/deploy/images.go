package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
)

// resolveTimeout is the longest api waits for one resolve call; the deploy target's own
// timeout (at most 50 s) always ends first (spec 0031).
const resolveTimeout = 60 * time.Second

// Problem codes of the image freeze (spec 0031).
const (
	codeImageUnresolved = "image_unresolved"
	codeInvalidUpdate   = "invalid_update"
)

// frozen is a graph's render with every image frozen: the render without a lock (its
// references are the lock keys), the frozen digests, the current lock's entries and presence,
// the registry's digest of each frozen reference checkNewer asked about, and the review digest.
type frozen struct {
	plain     contract.RenderedStack
	images    []contract.FrozenImage
	present   map[string]bool
	available map[string]string
	digest    string
}

// pins is the lock the managed render takes.
func (f frozen) pins() []contract.ImagePin { return contract.Pins(f.images) }

// freeze renders g for stack and freezes each of its image references (spec 0031, "the freeze
// rule"): a reference in the stack's current lock keeps its digest unless opts.Update names it;
// any other is resolved at the registry. With opts.CheckNewer it also asks the registry about
// every frozen reference. An update entry the graph does not run is invalid_update, a reference
// the registry cannot resolve image_unresolved, both as a *RefusedError.
func (s *Service) freeze(ctx context.Context, g contract.Graph, stack uuid.UUID, opts ImageOptions) (frozen, error) {
	plain, err := s.renderer.RenderManaged(ctx, g, stack, nil)
	if err != nil {
		return frozen{}, err
	}
	refs := make([]string, 0, len(plain.References))
	users := map[string][]contract.ImageUser{}
	for _, r := range plain.References {
		refs = append(refs, r.Reference)
		users[r.Reference] = r.UsedBy
	}
	var bad []contract.Problem
	for n, u := range opts.Update {
		if !slices.Contains(refs, u) {
			bad = append(bad, contract.Problem{
				Severity: contract.Error, Code: codeInvalidUpdate, Path: fmt.Sprintf("update[%d]", n),
				Message: u + " is not an image this stack runs.",
			})
		}
	}
	if len(bad) > 0 {
		return frozen{}, &RefusedError{Problems: bad}
	}
	current, present, err := s.currentLock(ctx, plain.ComposeProject)
	if err != nil {
		return frozen{}, err
	}
	need := contract.ToResolve(refs, current, opts.Update)
	ask := need
	if opts.CheckNewer {
		ask = slices.Compact(slices.Sorted(slices.Values(refs)))
	}
	resolved, failed, err := s.resolve(ctx, ask)
	if err != nil {
		return frozen{}, err
	}
	for _, ref := range need {
		if reason, ok := failed[ref]; ok {
			bad = append(bad, unresolved(ref, users[ref], reason))
		}
	}
	if len(bad) > 0 {
		return frozen{}, &RefusedError{Problems: bad}
	}
	images, err := contract.Freeze(refs, current, resolved, opts.Update)
	if err != nil {
		return frozen{}, fmt.Errorf("deploy: %w", err) // every reference of need resolved or was refused above
	}
	f := frozen{plain: plain, images: images, present: present, available: map[string]string{}}
	counts := map[contract.ImageChange]int{}
	for _, img := range images {
		counts[img.Change]++
		if d, ok := resolved[img.Reference]; ok && opts.CheckNewer && d != img.Digest {
			f.available[img.Reference] = d
		}
	}
	f.digest = contract.ReviewDigest(plain.Check.Digest, f.pins())
	s.log.InfoContext(ctx, "images resolved", slog.String("stack", g.Name), slog.Int("new", counts[contract.ChangeNew]),
		slog.Int("updated", counts[contract.ChangeUpdated]), slog.Int("available", len(f.available)))
	return f, nil
}

// currentLock reads the stack's current lock from the deploy target: its entries, and whether
// each reference's image is on the host.
func (s *Service) currentLock(ctx context.Context, composeProject string) ([]contract.LockedImage, map[string]bool, error) {
	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	imgs, err := s.target.Images(tctx, composeProject)
	if err != nil {
		return nil, nil, err
	}
	present := map[string]bool{}
	var current []contract.LockedImage
	if imgs.Current != nil {
		for _, img := range imgs.Current.Images {
			current = append(current, contract.LockedImage{
				Reference: img.Reference, Source: contract.ImageSource(img.Source), Digest: img.Digest, ImageID: img.ImageId,
			})
			present[img.Reference] = img.Present
		}
	}
	return current, present, nil
}

// resolve asks the deploy target for the digest of each of refs; the second map holds the
// reason of each reference that failed.
func (s *Service) resolve(ctx context.Context, refs []string) (map[string]string, map[string]string, error) {
	resolved, failed := map[string]string{}, map[string]string{}
	if len(refs) == 0 {
		return resolved, failed, nil
	}
	tctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	res, err := s.target.Resolve(tctx, refs)
	if err != nil {
		return nil, nil, err
	}
	for _, img := range res.Images {
		resolved[img.Reference] = img.Digest
	}
	for _, p := range res.Problems {
		var n int
		if _, err := fmt.Sscanf(p.Path, "references[%d]", &n); err == nil && n >= 0 && n < len(refs) {
			failed[refs[n]] = p.Message
		}
	}
	for _, ref := range refs {
		if _, ok := resolved[ref]; !ok && failed[ref] == "" {
			failed[ref] = ref + ": the deployer gave no digest."
		}
	}
	return resolved, failed, nil
}

// unresolved is the image_unresolved problem of ref, naming what uses it.
func unresolved(ref string, users []contract.ImageUser, reason string) contract.Problem {
	return contract.Problem{
		Severity: contract.Error, Code: codeImageUnresolved, Path: "images." + ref,
		Message: fmt.Sprintf("The image %s, used by %s, cannot be resolved: %s", ref, usedBy(users), reason),
	}
}

// usedBy names what runs an image in words: the instance for a module, the job and its
// capability, provider and consumer for a job. mp_jobs only reuses a job's image, so it is left
// out.
func usedBy(users []contract.ImageUser) string {
	var names []string
	for _, u := range reviewUsers(users) {
		switch u.Kind {
		case contract.ImageUserKindProvision:
			names = append(names, fmt.Sprintf("provision job of %s on %s for %s", u.Capability, u.Instance, u.Consumer))
		case contract.ImageUserKindRevoke:
			names = append(names, fmt.Sprintf("revoke job of %s on %s", u.Capability, u.Instance))
		case contract.ImageUserKindModule, contract.ImageUserKindWait:
			names = append(names, "instance "+u.Instance)
		}
	}
	return strings.Join(names, "; ")
}

// reviewUsers is users without the wait anchor, which only reuses a job's image.
func reviewUsers(users []contract.ImageUser) []contract.ImageUser {
	out := make([]contract.ImageUser, 0, len(users))
	for _, u := range users {
		if u.Kind != contract.ImageUserKindWait {
			out = append(out, u)
		}
	}
	return out
}

// reviewImages is the review's Images section of f.
func (f frozen) reviewImages() []ReviewImage {
	users := map[string][]contract.ImageUser{}
	for _, r := range f.plain.References {
		users[r.Reference] = r.UsedBy
	}
	out := make([]ReviewImage, 0, len(f.images))
	for _, img := range f.images {
		present, known := f.present[img.Reference]
		out = append(out, ReviewImage{
			Reference: img.Reference, UsedBy: reviewUsers(users[img.Reference]), Digest: img.Digest,
			Change: img.Change, PreviousDigest: img.PreviousDigest, Available: f.available[img.Reference],
			// a reference the lock does not hold is pulled at apply; only a frozen one can be missing
			Present: !known || present || img.Change != contract.ChangeFrozen,
		})
	}
	return out
}
