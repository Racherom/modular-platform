package deploy_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

func digestOf(c string) string { return "sha256:" + strings.Repeat(c, 64) }

// lockWith is a current lock holding whoami at digest d.
func lockWith(d string, present bool) *deploytarget.ImageLock {
	return &deploytarget.ImageLock{
		FormatVersion: 1, Stack: "01960000-0000-7000-8000-000000000001", Operation: "op",
		Images: []deploytarget.LockedImage{{
			Reference: whoamiRef, Source: deploytarget.Registry, Digest: d, ImageId: digestOf("9"), Present: present,
		}},
	}
}

func (f *fakeTarget) resolveCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.resolved)
}

// TestReviewNewImage covers AC-5: a reference new to the stack is resolved and shown new, and the
// review digest covers it.
func TestReviewNewImage(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{}
	s := newService(t, newMem(), target)
	r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []deploy.ReviewImage{{
		Reference: whoamiRef, Digest: whoamiDigest, Change: contract.ChangeNew, Present: true,
		UsedBy: []contract.ImageUser{{Service: "web", Kind: contract.ImageUserKindModule, Instance: "web"}},
	}}
	if !slices.EqualFunc(r.Images, want, func(a, b deploy.ReviewImage) bool {
		return a.Reference == b.Reference && a.Digest == b.Digest && a.Change == b.Change && a.Present == b.Present && slices.Equal(a.UsedBy, b.UsedBy)
	}) {
		t.Errorf("images = %+v", r.Images)
	}
	if r.Digest != goodDigest || r.Modules[0].Image != whoamiRef+"@"+whoamiDigest {
		t.Errorf("digest %s, modules %+v", r.Digest, r.Modules)
	}
}

// TestReviewFrozenOffline covers AC-3 and AC-10: a reference in the current lock keeps its digest
// with no registry call, and a missing image shows present false.
func TestReviewFrozenOffline(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{lock: lockWith(digestOf("b"), false), registry: map[string]string{}}
	s := newService(t, newMem(), target)
	r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Images) != 1 || r.Images[0].Change != contract.ChangeFrozen || r.Images[0].Digest != digestOf("b") || r.Images[0].Present {
		t.Errorf("images = %+v", r.Images)
	}
	if calls := target.resolveCalls(); len(calls) != 0 {
		t.Errorf("a frozen review called resolve: %v", calls)
	}
}

// TestReviewRefusals covers AC-5's image_unresolved naming what uses the image, invalid_update,
// and busy from the deploy target.
func TestReviewRefusals(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		target   *fakeTarget
		opts     deploy.ImageOptions
		wantCode string
		wantErr  error
	}{
		"unresolved":     {&fakeTarget{registry: map[string]string{}}, deploy.ImageOptions{}, "image_unresolved", nil},
		"invalid update": {&fakeTarget{}, deploy.ImageOptions{Update: []string{"nginx:1"}}, "invalid_update", nil},
		"busy":           {&fakeTarget{imagesErr: deploy.ErrBusy}, deploy.ImageOptions{}, "", deploy.ErrBusy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newService(t, newMem(), tc.target)
			_, err := s.Review(t.Context(), "sample", sample, tc.opts)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			refused, ok := errors.AsType[*deploy.RefusedError](err)
			if !ok || refused.Problems[0].Code != tc.wantCode {
				t.Fatalf("err = %v, want %s", err, tc.wantCode)
			}
			if tc.wantCode == "image_unresolved" && !strings.Contains(refused.Problems[0].Message, "instance web") {
				t.Errorf("message %q does not name the instance", refused.Problems[0].Message)
			}
		})
	}
}

// TestCheckNewerAndUpdate covers AC-6 and AC-7: checkNewer shows the available digest, update
// takes it with old and new digest, and an apply whose update differs from the review's is
// graph_changed.
func TestCheckNewerAndUpdate(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{lock: lockWith(digestOf("b"), true), registry: map[string]string{whoamiRef: digestOf("c")}}
	s := newService(t, newMem(), target)
	r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{CheckNewer: true})
	if err != nil {
		t.Fatal(err)
	}
	if img := r.Images[0]; img.Change != contract.ChangeFrozen || img.Digest != digestOf("b") || img.Available != digestOf("c") {
		t.Errorf("checkNewer row = %+v", img)
	}
	frozenDigest := r.Digest
	up, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{Update: []string{whoamiRef}})
	if err != nil {
		t.Fatal(err)
	}
	if img := up.Images[0]; img.Change != contract.ChangeUpdated || img.Digest != digestOf("c") || img.PreviousDigest != digestOf("b") {
		t.Errorf("update row = %+v", img)
	}
	if up.Digest == frozenDigest {
		t.Error("update did not change the review digest")
	}
	// the review with update, then an apply without it
	if _, err := s.Apply(t.Context(), admin, "sample", sample, up.Digest, deploy.ImageOptions{}, nil); !errors.Is(err, deploy.ErrGraphChanged) {
		t.Errorf("apply with another update: %v, want graph_changed", err)
	}
	if _, err := s.Apply(t.Context(), admin, "sample", sample, up.Digest, deploy.ImageOptions{Update: []string{whoamiRef}}, nil); err != nil {
		t.Fatalf("apply with the review's update: %v", err)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if compose := string(target.applied[0].Files[0].Data); !strings.Contains(compose, whoamiRef+"@"+digestOf("c")) {
		t.Errorf("applied %s, want the updated digest", compose)
	}
}

// TestApplyRaceMovedTag covers AC-7: a tag that moves between review and apply is graph_changed
// and nothing starts.
func TestApplyRaceMovedTag(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{}
	st := newMem()
	s := newService(t, st, target)
	r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target.mu.Lock()
	target.registry = map[string]string{whoamiRef: digestOf("d")}
	target.mu.Unlock()
	if _, err := s.Apply(t.Context(), admin, "sample", sample, r.Digest, deploy.ImageOptions{}, nil); !errors.Is(err, deploy.ErrGraphChanged) {
		t.Fatalf("err = %v, want graph_changed", err)
	}
	if len(st.deps) != 0 || len(st.ops) != 0 {
		t.Errorf("graph_changed left %d stacks and %d operations", len(st.deps), len(st.ops))
	}
}

// TestRollbackUsesTheLock covers AC-4: the rollback renders with the current lock, the last good
// apply's digest, and asks the registry nothing.
func TestRollbackUsesTheLock(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed, deploytarget.OperationStatusSucceeded,
	}}
	s := newService(t, st, target)
	applyAndWait(t, s, st, sample) // runs whoami at whoamiDigest
	// the deployer now holds that apply's lock; the tag moves to a broken image you take
	target.mu.Lock()
	target.lock = lockWith(whoamiDigest, true)
	target.registry = map[string]string{whoamiRef: digestOf("e")}
	target.mu.Unlock()
	broken := contract.ReviewDigest("sha256:abc", []contract.ImagePin{{Reference: whoamiRef, Digest: digestOf("e")}})
	op, err := s.Apply(t.Context(), admin, "sample", changed, broken, deploy.ImageOptions{Update: []string{whoamiRef}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := len(target.resolveCalls())
	waitFinal(t, st, op)
	waitFor(t, "rolled-back", func() bool { return stateOf(t, st) == deploy.StateRolledBack })
	if calls := target.resolveCalls(); len(calls) != before {
		t.Errorf("the rollback called resolve: %v", calls[before:])
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	var composes []string
	for _, a := range target.applied {
		composes = append(composes, string(a.Files[0].Data))
	}
	if len(composes) != 3 || !strings.Contains(composes[1], digestOf("e")) || !strings.Contains(composes[2], whoamiRef+"@"+whoamiDigest) {
		t.Errorf("applied composes = %q, want the rollback at the locked digest", composes)
	}
}

// TestRollbackLockUnreadable covers task 10: a rollback that cannot read the stack's lock ends
// failed with lock_invalid, and starts nothing on the host.
func TestRollbackLockUnreadable(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed,
	}}
	s := newService(t, st, target)
	applyAndWait(t, s, st, sample)
	op, err := s.Apply(t.Context(), admin, "sample", changed, goodDigest, deploy.ImageOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target.mu.Lock()
	target.imagesErr = deploy.ErrLockInvalid
	target.mu.Unlock()
	waitFinal(t, st, op)
	waitFor(t, "a failed rollback", func() bool {
		ops := history(t, st)
		return len(ops) == 3 && ops[2].Status == deploy.StatusFailed
	})
	if rb := history(t, st)[2]; rb.RollbackOf == nil || !strings.HasPrefix(rb.Error, "lock_invalid") {
		t.Errorf("rollback = %+v", rb)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.applied) != 2 {
		t.Errorf("the rollback reached the host: %d applies", len(target.applied))
	}
}
