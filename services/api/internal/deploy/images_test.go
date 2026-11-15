package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

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

// TestCheckFailed covers AC-6: with checkNewer, a frozen reference whose lookup fails keeps its
// digest and carries the reason alone as checkFailed, never available, and the review digest is
// the one without checkNewer. A failed lookup of a reference named in update, and a resolve call
// refused or failed as a whole, refuse the review instead.
func TestCheckFailed(t *testing.T) {
	t.Parallel()
	wholeCall := &deploy.ResolveRefusedError{Problems: []contract.Problem{{
		Severity: contract.Error, Code: "invalid_reference", Path: "references[0]", Message: whoamiRef + ": not a valid reference.",
	}}}
	for name, tc := range map[string]struct {
		target     *fakeTarget
		opts       deploy.ImageOptions
		wantFailed string
		wantCode   string
		wantErr    error
	}{
		"unknown tag": {
			target:     &fakeTarget{registry: map[string]string{}},
			opts:       deploy.ImageOptions{CheckNewer: true},
			wantFailed: "the registry has no image with this tag.",
		},
		"no answer for the reference": {
			target:     &fakeTarget{registry: map[string]string{}, silent: true},
			opts:       deploy.ImageOptions{CheckNewer: true},
			wantFailed: "the deployer gave no digest.",
		},
		"named in update": {
			target:   &fakeTarget{registry: map[string]string{}},
			opts:     deploy.ImageOptions{CheckNewer: true, Update: []string{whoamiRef}},
			wantCode: "image_unresolved",
		},
		"call refused as a whole": {
			target:   &fakeTarget{resolveErr: wholeCall},
			opts:     deploy.ImageOptions{CheckNewer: true},
			wantCode: "image_unresolved",
		},
		"call failed": {
			target:  &fakeTarget{resolveErr: deploy.ErrTargetUnavailable},
			opts:    deploy.ImageOptions{CheckNewer: true},
			wantErr: deploy.ErrTargetUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.target.lock = lockWith(digestOf("b"), true)
			s := newService(t, newMem(), tc.target)
			r, err := s.Review(t.Context(), "sample", sample, tc.opts)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			case tc.wantCode != "":
				refused, ok := errors.AsType[*deploy.RefusedError](err)
				if !ok || refused.Problems[0].Code != tc.wantCode || !strings.Contains(refused.Problems[0].Message, "instance web") {
					t.Fatalf("err = %v, want %s naming the instance", err, tc.wantCode)
				}
				return
			case err != nil:
				t.Fatal(err)
			}
			img := r.Images[0]
			if img.Change != contract.ChangeFrozen || img.Digest != digestOf("b") || img.CheckFailed != tc.wantFailed || img.Available != "" {
				t.Errorf("row = %+v, want frozen at b with checkFailed %q", img, tc.wantFailed)
			}
			plain, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Digest != plain.Digest {
				t.Errorf("review digest %s, want the one without checkNewer %s", r.Digest, plain.Digest)
			}
			if plain.Images[0].CheckFailed != "" {
				t.Errorf("checkFailed without checkNewer: %q", plain.Images[0].CheckFailed)
			}
		})
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
	target.imagesErr = &deploy.LockInvalidError{File: deploy.LockFile, Err: errors.New("unexpected end of JSON input")}
	target.mu.Unlock()
	waitFinal(t, st, op)
	waitFor(t, "a failed rollback", func() bool {
		ops := history(t, st)
		return len(ops) == 3 && ops[2].Status == deploy.StatusFailed
	})
	if rb := history(t, st)[2]; rb.RollbackOf == nil || rb.Error != "lock_invalid: the deploy target cannot read images.lock" {
		t.Errorf("rollback = %+v", rb)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.applied) != 2 {
		t.Errorf("the rollback reached the host: %d applies", len(target.applied))
	}
}

// TestApplyOtherUpdateSameLock covers AC-7's last sentence: an update different from the
// review's that moves no digest builds the same lock, so apply goes ahead; once the tag moved,
// the same update is graph_changed and starts nothing.
func TestApplyOtherUpdateSameLock(t *testing.T) {
	t.Parallel()
	t.Run("tag not moved", func(t *testing.T) {
		t.Parallel()
		target := &fakeTarget{lock: lockWith(digestOf("b"), true), registry: map[string]string{whoamiRef: digestOf("b")}}
		s := newService(t, newMem(), target)
		r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Apply(t.Context(), admin, "sample", sample, r.Digest, deploy.ImageOptions{Update: []string{whoamiRef}}, nil); err != nil {
			t.Fatalf("apply with an update that moves nothing: %v, want accepted", err)
		}
		target.mu.Lock()
		defer target.mu.Unlock()
		if compose := string(target.applied[0].Files[0].Data); !strings.Contains(compose, whoamiRef+"@"+digestOf("b")) {
			t.Errorf("applied %s, want the frozen digest", compose)
		}
	})
	t.Run("tag moved", func(t *testing.T) {
		t.Parallel()
		target := &fakeTarget{lock: lockWith(digestOf("b"), true), registry: map[string]string{whoamiRef: digestOf("c")}}
		st := newMem()
		s := newService(t, st, target)
		r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Apply(t.Context(), admin, "sample", sample, r.Digest, deploy.ImageOptions{Update: []string{whoamiRef}}, nil); !errors.Is(err, deploy.ErrGraphChanged) {
			t.Fatalf("err = %v, want graph_changed", err)
		}
		if len(st.deps) != 0 || len(st.ops) != 0 {
			t.Errorf("graph_changed left %d stacks and %d operations", len(st.deps), len(st.ops))
		}
	})
}

// jobRenderer renders the fake stack plus a provisioner whose job image postgresRef runs the
// provision job, the revoke job and the mp_jobs wait anchor (spec 0031, AC-14).
type jobRenderer struct{ fakeRenderer }

const postgresRef = "postgres:18.6"

func (p jobRenderer) RenderManaged(ctx context.Context, g contract.Graph, stack uuid.UUID, lock []contract.ImagePin) (contract.RenderedStack, error) {
	r, err := p.fakeRenderer.RenderManaged(ctx, g, stack, lock)
	r.References = append(r.References, contract.ImageReference{Reference: postgresRef, UsedBy: []contract.ImageUser{
		{Service: "db_provision_app_postgresql", Kind: contract.ImageUserKindProvision, Instance: "db", Capability: "postgresql-provisioner", Consumer: "app"},
		{Service: "db_revoke_postgresql", Kind: contract.ImageUserKindRevoke, Instance: "db", Capability: "postgresql-provisioner"},
		{Service: "mp_jobs", Kind: contract.ImageUserKindWait},
	}})
	return r, err
}

func newJobService(t *testing.T, target *fakeTarget) *deploy.Service {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := deploy.NewService(ctx, newMem(), jobRenderer{fakeRenderer{digest: "sha256:abc"}}, target, deploy.NewHub(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() { cancel(); s.Wait() })
	return s
}

// TestUnresolvedNamesTheJobs covers AC-5 and AC-14: a job image that cannot be resolved names
// the provision and revoke jobs by capability, provider and consumer, and never mp_jobs.
func TestUnresolvedNamesTheJobs(t *testing.T) {
	t.Parallel()
	s := newJobService(t, &fakeTarget{registry: map[string]string{whoamiRef: whoamiDigest}})
	_, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
	refused, ok := errors.AsType[*deploy.RefusedError](err)
	if !ok || len(refused.Problems) != 1 {
		t.Fatalf("err = %v, want one image_unresolved", err)
	}
	p := refused.Problems[0]
	if p.Code != "image_unresolved" || p.Path != "images."+postgresRef {
		t.Errorf("problem = %+v", p)
	}
	// exactly the two jobs: a wait anchor would add "; instance " before the comma
	want := "The image " + postgresRef + ", used by provision job of postgresql-provisioner on db for app; revoke job of postgresql-provisioner on db, cannot be resolved"
	if !strings.HasPrefix(p.Message, want) {
		t.Errorf("message %q, want it to start %q", p.Message, want)
	}
}

// TestReviewListsJobImages covers AC-14: the review lists the job image with its provision and
// revoke jobs, its own badge and digest, and leaves the wait anchor out.
func TestReviewListsJobImages(t *testing.T) {
	t.Parallel()
	s := newJobService(t, &fakeTarget{registry: map[string]string{whoamiRef: whoamiDigest, postgresRef: digestOf("d")}})
	r, err := s.Review(t.Context(), "sample", sample, deploy.ImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(r.Images, func(img deploy.ReviewImage) bool { return img.Reference == postgresRef })
	if i < 0 {
		t.Fatalf("no %s row in %+v", postgresRef, r.Images)
	}
	img := r.Images[i]
	if img.Change != contract.ChangeNew || img.Digest != digestOf("d") {
		t.Errorf("job image row = %+v", img)
	}
	var kinds []contract.ImageUserKind
	for _, u := range img.UsedBy {
		kinds = append(kinds, u.Kind)
	}
	if !slices.Equal(kinds, []contract.ImageUserKind{contract.ImageUserKindProvision, contract.ImageUserKindRevoke}) {
		t.Errorf("used by kinds = %v, want provision and revoke only", kinds)
	}
	want := contract.ReviewDigest("sha256:abc", []contract.ImagePin{{Reference: postgresRef, Digest: digestOf("d")}, {Reference: whoamiRef, Digest: whoamiDigest}})
	if r.Digest != want {
		t.Errorf("review digest %s does not cover the job image", r.Digest)
	}
}

// lockedLog is a log sink the polling goroutines and the test may use at once.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestRollbackResolvesAMissingReference covers the rollback row of spec 0031's value sourcing
// table: a reference missing from the current lock is resolved at the registry instead of
// failing the rollback, and api logs "rollback resolved an image" at warn level.
func TestRollbackResolvesAMissingReference(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed, deploytarget.OperationStatusSucceeded,
	}}
	logs := &lockedLog{}
	ctx, cancel := context.WithCancel(t.Context())
	s := deploy.NewService(ctx, st, fakeRenderer{digest: "sha256:abc"}, target, deploy.NewHub(), slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { cancel(); s.Wait() })
	applyAndWait(t, s, st, sample)
	// the deployer holds no lock (the first apply's write failed), and the tag moved
	target.mu.Lock()
	target.registry = map[string]string{whoamiRef: digestOf("e")}
	target.mu.Unlock()
	moved := contract.ReviewDigest("sha256:abc", []contract.ImagePin{{Reference: whoamiRef, Digest: digestOf("e")}})
	op, err := s.Apply(t.Context(), admin, "sample", changed, moved, deploy.ImageOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := len(target.resolveCalls())
	waitFinal(t, st, op)
	waitFor(t, "rolled-back", func() bool { return stateOf(t, st) == deploy.StateRolledBack })
	if calls := target.resolveCalls(); len(calls) == before {
		t.Error("the rollback did not resolve the reference the lock lacks")
	}
	if !strings.Contains(logs.String(), `"level":"WARN","msg":"rollback resolved an image"`) || !strings.Contains(logs.String(), `"reference":"`+whoamiRef+`"`) {
		t.Errorf("no warn log naming the reference: %s", logs)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.applied) != 3 {
		t.Fatalf("%d applies, want apply, failed apply and rollback", len(target.applied))
	}
}
