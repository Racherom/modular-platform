package deploy_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

// changed is sample with one more module: the graph of a later apply.
var changed = func() contract.Graph {
	g := sample
	g.Modules = map[string]contract.GraphModule{"web": {Module: "whoami"}, "db": {Module: "postgres"}, "cache": {Module: "valkey"}}
	return g
}()

func stateOf(t *testing.T, st *memStore) deploy.State {
	t.Helper()
	d, _, _ := st.Deployment(t.Context(), "sample")
	return d.State()
}

// applyAndWait applies g and waits until its operation has a final status.
func applyAndWait(t *testing.T, s *deploy.Service, st *memStore, g contract.Graph) deploy.Operation {
	t.Helper()
	op, err := s.Apply(t.Context(), admin, "sample", g, "sha256:abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	return waitFinal(t, st, op)
}

func waitFinal(t *testing.T, st *memStore, op deploy.Operation) deploy.Operation {
	t.Helper()
	var done deploy.Operation
	waitFor(t, "a final status", func() bool {
		done, _, _ = st.Operation(t.Context(), op.ID)
		return done.Status != deploy.StatusRunning
	})
	return done
}

// history lists the deployment's operations oldest first.
func history(t *testing.T, st *memStore) []deploy.Operation {
	t.Helper()
	d, _, _ := st.Deployment(t.Context(), "sample")
	ops, _ := st.Operations(t.Context(), d.ID, nil, 50)
	slices.SortFunc(ops, func(a, b deploy.Operation) int { return a.StartedAt.Compare(b.StartedAt) })
	return ops
}

func TestRemove(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusSucceeded,
	}}
	s := newService(t, st, target)
	applyAndWait(t, s, st, sample)

	op, err := s.Remove(t.Context(), admin, "sample", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if op.Action != deploy.ActionRemove || op.DeleteData {
		t.Errorf("started as %+v", op)
	}
	waitFinal(t, st, op)
	if got := stateOf(t, st); got != deploy.StateRemoved {
		t.Fatalf("state %s, want removed", got)
	}
	v, err := s.View(t.Context(), "sample")
	if err != nil || len(v.Modules) != 0 {
		t.Errorf("a removed deployment shows modules %+v, err %v", v.Modules, err)
	}
	if _, err := s.Remove(t.Context(), admin, "sample", false, ""); !errors.Is(err, deploy.ErrAlreadyRemoved) {
		t.Errorf("a plain remove of a removed deployment: %v, want ErrAlreadyRemoved", err)
	}
	if _, err := s.Remove(t.Context(), admin, "sample", true, "Sample"); !refusedWith(err, "confirm_mismatch") {
		t.Errorf("delete data with the wrong name: %v", err)
	}
	op, err = s.Remove(t.Context(), admin, "sample", true, "sample")
	if err != nil {
		t.Fatalf("delete data of a removed deployment is allowed: %v", err)
	}
	if !op.DeleteData {
		t.Errorf("delete data not recorded: %+v", op)
	}
	waitFinal(t, st, op)
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.removed) != 2 || target.removed[0] || !target.removed[1] {
		t.Errorf("removes sent %v, want [false true]", target.removed)
	}
	// the history stays
	if n := len(history(t, st)); n != 3 {
		t.Errorf("history has %d operations, want 3", n)
	}
}

func TestRemoveRefusals(t *testing.T) {
	t.Parallel()
	t.Run("no such deployment", func(t *testing.T) {
		t.Parallel()
		s := newService(t, newMem(), &fakeTarget{})
		if _, err := s.Remove(t.Context(), admin, "sample", false, ""); !errors.Is(err, deploy.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	for name, tc := range map[string]struct {
		removeErr, want error
	}{
		"nothing on the host": {deploy.ErrNotFound, deploy.ErrNotFound},
		"deployer busy":       {deploy.ErrBusy, deploy.ErrBusy},
		"deployer miswired":   {deploy.ErrTarget, deploy.ErrTarget},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newMem()
			target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{deploytarget.OperationStatusSucceeded}}
			s := newService(t, st, target)
			applyAndWait(t, s, st, sample)
			target.mu.Lock()
			target.removeErr = tc.removeErr
			target.mu.Unlock()
			if _, err := s.Remove(t.Context(), admin, "sample", false, ""); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			// a refusal leaves no trace
			if n := len(history(t, st)); n != 1 || stateOf(t, st) != deploy.StateRunning {
				t.Errorf("after a refused remove: %d operations, state %s", n, stateOf(t, st))
			}
		})
	}
	t.Run("busy in the record", func(t *testing.T) {
		t.Parallel()
		st := newMem()
		s := newService(t, st, &fakeTarget{boot: "boot-1"})
		if _, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Remove(t.Context(), admin, "sample", false, ""); !errors.Is(err, deploy.ErrBusy) {
			t.Errorf("remove during an apply: %v, want ErrBusy", err)
		}
	})
	t.Run("deployer down", func(t *testing.T) {
		t.Parallel()
		st := newMem()
		target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{deploytarget.OperationStatusSucceeded}}
		s := newService(t, st, target)
		applyAndWait(t, s, st, sample)
		target.mu.Lock()
		target.removeErr = deploy.ErrTargetUnavailable
		target.mu.Unlock()
		op, err := s.Remove(t.Context(), admin, "sample", true, "sample")
		if err != nil {
			t.Fatal(err)
		}
		if op.Status != deploy.StatusFailed || op.Error != "deployer_unavailable" || !op.DeleteData {
			t.Errorf("operation %+v", op)
		}
	})
}

func refusedWith(err error, code string) bool {
	refused, ok := errors.AsType[*deploy.RefusedError](err)
	return ok && refused.Problems[0].Code == code
}

// TestLatestApplyGraph: Apply again reviews the newest apply's graph, whatever its status, while
// graph stays the one that runs (AC-16).
func TestLatestApplyGraph(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", failStep: deploytarget.Pull, polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed,
	}}
	s := newService(t, st, target)
	applyAndWait(t, s, st, sample)
	applyAndWait(t, s, st, changed)
	v, err := s.View(t.Context(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	if v.Graph == nil || len(v.Graph.Modules) != 2 {
		t.Errorf("graph %+v, want the running one", v.Graph)
	}
	if v.LatestApplyGraph == nil || len(v.LatestApplyGraph.Modules) != 3 {
		t.Errorf("latest apply graph %+v, want the changed one", v.LatestApplyGraph)
	}
}

// TestRollback: a changed graph that fails at up is followed by a rollback to the last good
// graph, started as the failed apply's starter, and the deployment shows rolled-back (AC-14).
func TestRollback(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed, deploytarget.OperationStatusSucceeded,
	}}
	s := newService(t, st, target)
	applyAndWait(t, s, st, sample)
	failed := applyAndWait(t, s, st, changed)
	waitFor(t, "rolled-back", func() bool { return stateOf(t, st) == deploy.StateRolledBack })

	ops := history(t, st)
	if len(ops) != 3 {
		t.Fatalf("history has %d operations, want 3", len(ops))
	}
	rb := ops[2]
	if rb.RollbackOf == nil || *rb.RollbackOf != failed.ID || rb.StartedBy != admin || rb.Status != deploy.StatusSucceeded {
		t.Errorf("rollback %+v", rb)
	}
	if string(rb.Graph) != string(ops[0].Graph) {
		t.Errorf("the rollback applied %s, want the last good graph %s", rb.Graph, ops[0].Graph)
	}
	v, err := s.View(t.Context(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	if v.RolledBackFrom == nil || v.RolledBackFrom.ID != failed.ID || v.RolledBackFrom.Step != deploy.StepUp {
		t.Errorf("rolled back from %+v", v.RolledBackFrom)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.applied) != 3 || len(target.applied[2].Placed) != 0 {
		t.Errorf("the rollback sent %+v; it sends no placed values", target.applied)
	}
}

// TestNoRollback: a failure before up, a first apply, and an unknown outcome start nothing.
func TestNoRollback(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		polls    []deploytarget.OperationStatus
		failStep deploytarget.OperationStep
		graphs   []contract.Graph
	}{
		"failed at pull": {[]deploytarget.OperationStatus{deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed}, deploytarget.Pull, []contract.Graph{sample, changed}},
		"first apply":    {[]deploytarget.OperationStatus{deploytarget.OperationStatusFailed}, "", []contract.Graph{sample}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newMem()
			s := newService(t, st, &fakeTarget{boot: "boot-1", polls: tc.polls, failStep: tc.failStep})
			for _, g := range tc.graphs {
				applyAndWait(t, s, st, g)
			}
			time.Sleep(50 * time.Millisecond) // a rollback would have started by now
			if n := len(history(t, st)); n != len(tc.graphs) || stateOf(t, st) != deploy.StateFailed {
				t.Errorf("%d operations, state %s; want %d and failed", n, stateOf(t, st), len(tc.graphs))
			}
		})
	}
}

// TestRollbackRetriedOnce: a failed rollback is retried once after the wait, then the
// deployment shows failed (AC-14).
func TestRollbackRetriedOnce(t *testing.T) {
	t.Parallel()
	st := newMem()
	failed := deploytarget.OperationStatusFailed
	s := newService(t, st, &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, failed, failed, failed,
	}})
	deploy.SetRetryAfter(s, 20*time.Millisecond)
	applyAndWait(t, s, st, sample)
	bad := applyAndWait(t, s, st, changed)
	waitFor(t, "two rollbacks", func() bool {
		ops := history(t, st)
		return len(ops) == 4 && ops[3].Status == deploy.StatusFailed
	})
	time.Sleep(100 * time.Millisecond) // a third rollback would have started by now
	ops := history(t, st)
	if len(ops) != 4 {
		t.Fatalf("history has %d operations, want 4", len(ops))
	}
	for _, rb := range ops[2:] {
		if rb.RollbackOf == nil || *rb.RollbackOf != bad.ID {
			t.Errorf("rollback %+v does not roll back %s", rb, bad.ID)
		}
	}
	if got := stateOf(t, st); got != deploy.StateFailed {
		t.Errorf("state %s, want failed", got)
	}
}

// TestApplyCancelsTheRetry: an apply inside the wait after a failed rollback cancels the retry.
func TestApplyCancelsTheRetry(t *testing.T) {
	t.Parallel()
	st := newMem()
	failed := deploytarget.OperationStatusFailed
	s := newService(t, st, &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, failed, failed, deploytarget.OperationStatusSucceeded,
	}})
	deploy.SetRetryAfter(s, 300*time.Millisecond)
	applyAndWait(t, s, st, sample)
	applyAndWait(t, s, st, changed)
	waitFor(t, "a failed rollback", func() bool {
		ops := history(t, st)
		return len(ops) == 3 && ops[2].Status == deploy.StatusFailed
	})
	applyAndWait(t, s, st, sample)
	time.Sleep(500 * time.Millisecond)
	if n := len(history(t, st)); n != 4 || stateOf(t, st) != deploy.StateRunning {
		t.Errorf("%d operations, state %s; want 4 and running, with no retry", n, stateOf(t, st))
	}
}

// TestRollbackRefusedIsRecorded: a rollback the deploy target refuses ends failed with the
// problem, unlike a refused apply, which leaves no trace.
func TestRollbackRefusedIsRecorded(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{
		deploytarget.OperationStatusSucceeded, deploytarget.OperationStatusFailed,
	}}
	s := newService(t, st, target)
	deploy.SetRetryAfter(s, time.Hour)
	applyAndWait(t, s, st, sample)
	op, err := s.Apply(t.Context(), admin, "sample", changed, "sha256:abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	target.mu.Lock()
	target.applyErr = &deploy.RefusedError{Problems: []contract.Problem{{Code: "placed_missing", Path: "secrets.secrets/app/uses_db_password", Message: "needs a value"}}}
	target.mu.Unlock()
	waitFinal(t, st, op)
	waitFor(t, "a recorded rollback", func() bool {
		ops := history(t, st)
		return len(ops) == 3 && ops[2].Status == deploy.StatusFailed
	})
	if rb := history(t, st)[2]; rb.RollbackOf == nil || !strings.HasPrefix(rb.Error, "placed_missing") {
		t.Errorf("rollback %+v", rb)
	}
}
