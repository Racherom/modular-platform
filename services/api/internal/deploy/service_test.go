package deploy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

// memStore keeps deployments and operations in memory, with the database's busy rule.
type memStore struct {
	mu   sync.Mutex
	deps map[string]*deploy.Deployment
	ops  map[uuid.UUID]*deploy.Operation
}

func newMem() *memStore {
	return &memStore{deps: map[string]*deploy.Deployment{}, ops: map[uuid.UUID]*deploy.Operation{}}
}

func (m *memStore) byID(id uuid.UUID) *deploy.Deployment {
	for _, d := range m.deps {
		if d.ID == id {
			return d
		}
	}
	return nil
}

func (m *memStore) EnsureDeployment(_ context.Context, name string) (uuid.UUID, string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.deps[name]; ok {
		return d.ID, d.Project, false, nil
	}
	d := &deploy.Deployment{ID: uuid.Must(uuid.NewV7()), Name: name, Project: "mp-" + name, CreatedAt: time.Now()}
	m.deps[name] = d
	return d.ID, d.Project, true, nil
}

func (m *memStore) DropNewDeployment(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, d := range m.deps {
		if d.ID == id && d.Latest == nil {
			delete(m.deps, name)
		}
	}
	return nil
}

func (m *memStore) StartOperation(_ context.Context, op deploy.Operation) (deploy.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.ops {
		if o.DeploymentID == op.DeploymentID && o.Status == deploy.StatusRunning {
			return deploy.Operation{}, deploy.ErrBusy
		}
	}
	op.ID, op.Status, op.Step, op.StartedAt = uuid.Must(uuid.NewV7()), deploy.StatusRunning, deploy.StepPrepare, time.Now()
	stored := op
	m.ops[op.ID] = &stored
	m.byID(op.DeploymentID).Latest = &stored
	return op, nil
}

func (m *memStore) StartOperationIfLatest(ctx context.Context, op deploy.Operation, latest uuid.UUID) (deploy.Operation, bool, error) {
	m.mu.Lock()
	d := m.byID(op.DeploymentID)
	current := d.Latest != nil && d.Latest.ID == latest
	m.mu.Unlock()
	if !current {
		return deploy.Operation{}, false, nil
	}
	started, err := m.StartOperation(ctx, op)
	if errors.Is(err, deploy.ErrBusy) {
		return deploy.Operation{}, false, nil
	}
	return started, err == nil, err
}

func (m *memStore) DropOperation(_ context.Context, op deploy.Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ops, op.ID)
	d := m.byID(op.DeploymentID)
	d.Latest = nil
	for _, o := range m.ops {
		if o.DeploymentID == d.ID && (d.Latest == nil || o.StartedAt.After(d.Latest.StartedAt)) {
			d.Latest = o
		}
	}
	return nil
}

func (m *memStore) AcceptOperation(_ context.Context, id uuid.UUID, deployerID, bootID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops[id].DeployerOperationID, m.ops[id].DeployerBootID = deployerID, bootID
	return nil
}

func (m *memStore) ProgressOperation(_ context.Context, id uuid.UUID, step deploy.Step) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := m.ops[id]
	if op.Status != deploy.StatusRunning || op.Step == step {
		return false, nil
	}
	op.Step = step
	return true, nil
}

func (m *memStore) FinishOperation(_ context.Context, id uuid.UUID, status deploy.Status, step deploy.Step, errText string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := m.ops[id]
	if op.Status != deploy.StatusRunning {
		return false, nil
	}
	now := time.Now()
	op.Status, op.Step, op.Error, op.FinishedAt = status, step, errText, &now
	return true, nil
}

func (m *memStore) Operation(_ context.Context, id uuid.UUID) (deploy.Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[id]
	if !ok {
		return deploy.Operation{}, false, nil
	}
	return *op, true, nil
}

func (m *memStore) RunningOperations(context.Context) ([]deploy.RunningOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []deploy.RunningOperation
	for _, op := range m.ops {
		if op.Status == deploy.StatusRunning {
			d := m.byID(op.DeploymentID)
			out = append(out, deploy.RunningOperation{Operation: *op, DeploymentName: d.Name, Project: d.Project})
		}
	}
	return out, nil
}

func (m *memStore) Deployment(_ context.Context, name string) (deploy.Deployment, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deps[name]
	if !ok {
		return deploy.Deployment{}, false, nil
	}
	out := *d
	if d.Latest != nil {
		latest := *d.Latest
		out.Latest = &latest
	}
	return out, true, nil
}

func (m *memStore) Deployments(ctx context.Context, after string, limit int) ([]deploy.Deployment, error) {
	var names []string
	m.mu.Lock()
	for n := range m.deps {
		if n > after {
			names = append(names, n)
		}
	}
	m.mu.Unlock()
	slices.Sort(names)
	var out []deploy.Deployment
	for _, n := range names[:min(limit, len(names))] {
		d, _, _ := m.Deployment(ctx, n)
		out = append(out, d)
	}
	return out, nil
}

func (m *memStore) Operations(_ context.Context, id uuid.UUID, _ *uuid.UUID, _ int) ([]deploy.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []deploy.Operation
	for _, op := range m.ops {
		if op.DeploymentID == id {
			out = append(out, *op)
		}
	}
	return out, nil
}

func (m *memStore) latest(id uuid.UUID, keep func(*deploy.Operation) bool) (deploy.Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *deploy.Operation
	for _, op := range m.ops {
		if op.DeploymentID == id && keep(op) && (best == nil || op.StartedAt.After(best.StartedAt)) {
			best = op
		}
	}
	if best == nil {
		return deploy.Operation{}, false, nil
	}
	return *best, true, nil
}

func (m *memStore) LatestSucceededApply(_ context.Context, id uuid.UUID) (deploy.Operation, bool, error) {
	return m.latest(id, func(op *deploy.Operation) bool {
		return op.Action == deploy.ActionApply && op.Status == deploy.StatusSucceeded
	})
}

func (m *memStore) LatestApplyReachedUp(_ context.Context, id uuid.UUID) (deploy.Operation, bool, error) {
	return m.latest(id, func(op *deploy.Operation) bool {
		return op.Action == deploy.ActionApply && (op.Step == deploy.StepUp || op.Step == deploy.StepDone)
	})
}

func (m *memStore) LatestApply(_ context.Context, id uuid.UUID) (deploy.Operation, bool, error) {
	return m.latest(id, func(op *deploy.Operation) bool { return op.Action == deploy.ActionApply })
}

func (m *memStore) LatestSucceededBefore(_ context.Context, id, failed uuid.UUID) (deploy.Operation, bool, error) {
	m.mu.Lock()
	before := m.ops[failed].StartedAt
	m.mu.Unlock()
	return m.latest(id, func(op *deploy.Operation) bool {
		return op.Status == deploy.StatusSucceeded && op.StartedAt.Before(before)
	})
}

func (m *memStore) CountRollbacks(_ context.Context, of uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, op := range m.ops {
		if op.RollbackOf != nil && *op.RollbackOf == of {
			n++
		}
	}
	return n, nil
}

type fakeRenderer struct{ digest string }

func (p fakeRenderer) Plan(_ context.Context, g contract.Graph) (contract.PlanResult, error) {
	return contract.PlanResult{Ok: true, Graph: g, Digest: p.digest}, nil
}

func (p fakeRenderer) RenderManaged(_ context.Context, g contract.Graph, _ uuid.UUID) (contract.RenderedProject, error) {
	return contract.RenderedProject{
		Project: "mp-" + g.Name, Plan: contract.PlanResult{Ok: true, Graph: g, Digest: p.digest},
		Images:  []contract.ModuleImage{{Instance: "web", Module: "whoami", Image: "traefik/whoami:v1.12.0"}},
		Secrets: []contract.RenderedSecret{{Path: "secrets/app/uses_db_password", Kind: contract.Placed, Instance: "app", Capability: "postgresql", Field: "password"}},
		Files:   []contract.RenderedFile{{Path: "docker-compose.yml", Mode: 0o644, Data: []byte("name: x\n")}},
	}, nil
}

// fakeTarget answers apply with applyErr and remove with removeErr, and each poll from its
// script in turn; a failed poll reports failStep, by default up.
type fakeTarget struct {
	mu        sync.Mutex
	applyErr  error
	removeErr error
	boot      string
	polls     []deploytarget.OperationStatus
	failStep  deploytarget.OperationStep
	callers   []string
	applied   []deploytarget.ApplyRequest
	removed   []bool // deleteData of each remove
	present   []string
	// services, when set, is the state read's answer; stateCallers records each read's caller
	services     []deploytarget.ServiceState
	stateReads   int
	stateCallers []string
}

func (f *fakeTarget) Apply(ctx context.Context, _ string, req deploytarget.ApplyRequest) (deploy.TargetOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := sdk.CallerFrom(ctx); ok {
		f.callers = append(f.callers, c.User)
	}
	if f.applyErr != nil {
		return deploy.TargetOperation{}, f.applyErr
	}
	f.applied = append(f.applied, req)
	return deploy.TargetOperation{Operation: deploytarget.Operation{Id: "t1", Status: deploytarget.OperationStatusRunning}, BootID: "boot-1"}, nil
}

func (f *fakeTarget) Remove(ctx context.Context, _ string, deleteData bool) (deploy.TargetOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := sdk.CallerFrom(ctx); ok {
		f.callers = append(f.callers, c.User)
	}
	if f.removeErr != nil {
		return deploy.TargetOperation{}, f.removeErr
	}
	f.removed = append(f.removed, deleteData)
	return deploy.TargetOperation{Operation: deploytarget.Operation{Id: "r1", Status: deploytarget.OperationStatusRunning}, BootID: "boot-1"}, nil
}

func (f *fakeTarget) Operation(ctx context.Context, id string) (deploy.TargetOperation, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := sdk.CallerFrom(ctx); ok {
		f.callers = append(f.callers, c.User)
	}
	status := deploytarget.OperationStatusRunning
	if len(f.polls) > 0 {
		status, f.polls = f.polls[0], f.polls[1:]
	}
	step := deploytarget.Up
	switch {
	case status == deploytarget.OperationStatusSucceeded:
		step = deploytarget.Done
	case status == deploytarget.OperationStatusFailed && f.failStep != "":
		step = f.failStep
	}
	return deploy.TargetOperation{Operation: deploytarget.Operation{Id: id, Status: status, Step: step}}, f.boot, nil
}

func (f *fakeTarget) State(ctx context.Context, _ string) (deploytarget.ProjectState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateReads++
	if c, ok := sdk.CallerFrom(ctx); ok {
		f.stateCallers = append(f.stateCallers, c.User)
	}
	if f.services != nil {
		return deploytarget.ProjectState{Present: true, Services: slices.Clone(f.services)}, nil
	}
	return deploytarget.ProjectState{Present: true, Services: []deploytarget.ServiceState{
		{Instance: "web", Module: "whoami", State: deploytarget.ServiceStateStateRunning},
	}}, nil
}

func (f *fakeTarget) Secrets(context.Context, string) ([]string, error) { return f.present, nil }

var sample = contract.Graph{
	ApiVersion: contract.APIVersion, Kind: "Graph", Name: "sample",
	Modules: map[string]contract.GraphModule{"web": {Module: "whoami"}, "db": {Module: "postgres"}},
}

var admin = deploy.Starter{Issuer: "https://idp", Subject: "user-1", Email: "admin@example.com"}

func newService(t *testing.T, st *memStore, target *fakeTarget) *deploy.Service {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := deploy.NewService(ctx, st, fakeRenderer{digest: "sha256:abc"}, target, deploy.NewHub(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() { cancel(); s.Wait() })
	deploy.SetPollEvery(s, 5*time.Millisecond)
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestApplyRunsToRunning(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{deploytarget.OperationStatusRunning, deploytarget.OperationStatusSucceeded}}
	s := newService(t, st, target)
	op, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", []deploy.PlacedInput{{Path: "secrets/app/uses_db_password", Value: "pw"}})
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != deploy.StatusRunning || op.DeployerOperationID != "t1" || op.DeployerBootID != "boot-1" {
		t.Errorf("started as %+v", op)
	}
	if !slices.Equal(op.PlacedSupplied, []string{"secrets/app/uses_db_password"}) {
		t.Errorf("placed supplied %q", op.PlacedSupplied)
	}
	waitFor(t, "running", func() bool {
		d, _, _ := st.Deployment(t.Context(), "sample")
		return d.State() == deploy.StateRunning
	})
	v, err := s.View(t.Context(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	// db has no container: missing; web runs
	if len(v.Modules) != 2 || v.Modules[0].Instance != "db" || v.Modules[0].State != "missing" || v.Modules[1].State != "running" {
		t.Errorf("modules %+v", v.Modules)
	}
	if v.Graph == nil || v.Graph.Name != "sample" {
		t.Errorf("graph %+v", v.Graph)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	for _, c := range target.callers {
		if c != "user-1" {
			t.Errorf("a call to the deploy target carried caller %q", c)
		}
	}
	if len(target.applied) != 1 || string(target.applied[0].Placed[0].Data) != "pw" {
		t.Errorf("applied %+v", target.applied)
	}
}

func TestApplyRefusals(t *testing.T) {
	t.Parallel()
	refusal := &deploy.RefusedError{Problems: []contract.Problem{{Code: "host_access"}}}
	for name, tc := range map[string]struct {
		name, digest string
		applyErr     error
		want         error
		wantCode     string
	}{
		"name mismatch":     {"other", "sha256:abc", nil, nil, "name_mismatch"},
		"plan changed":      {"sample", "sha256:old", nil, deploy.ErrPlanChanged, ""},
		"deployer refuses":  {"sample", "sha256:abc", refusal, nil, "host_access"},
		"deployer busy":     {"sample", "sha256:abc", deploy.ErrBusy, deploy.ErrBusy, ""},
		"deployer miswired": {"sample", "sha256:abc", deploy.ErrTarget, deploy.ErrTarget, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newMem()
			s := newService(t, st, &fakeTarget{applyErr: tc.applyErr})
			_, err := s.Apply(t.Context(), admin, tc.name, sample, tc.digest, nil)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if tc.wantCode != "" {
				refused, ok := errors.AsType[*deploy.RefusedError](err)
				if !ok || refused.Problems[0].Code != tc.wantCode {
					t.Errorf("err = %v, want %s", err, tc.wantCode)
				}
			}
			if len(st.deps) != 0 || len(st.ops) != 0 {
				t.Errorf("a refusal left %d deployments and %d operations", len(st.deps), len(st.ops))
			}
		})
	}
	t.Run("reserved name", func(t *testing.T) {
		t.Parallel()
		g := sample
		g.Name = deploy.ReservedName
		s := newService(t, newMem(), &fakeTarget{})
		_, err := s.Apply(t.Context(), admin, deploy.ReservedName, g, "sha256:abc", nil)
		if refused, ok := errors.AsType[*deploy.RefusedError](err); !ok || refused.Problems[0].Code != "reserved_name" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestApplyWithTheDeployerDown(t *testing.T) {
	t.Parallel()
	st := newMem()
	s := newService(t, st, &fakeTarget{applyErr: deploy.ErrTargetUnavailable})
	op, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != deploy.StatusFailed || op.Error != "deployer_unavailable" {
		t.Errorf("operation %+v", op)
	}
	d, _, _ := st.Deployment(t.Context(), "sample")
	if d.State() != deploy.StateFailed {
		t.Errorf("state %s", d.State())
	}
}

func TestPollMarksUnknownOnANewBoot(t *testing.T) {
	t.Parallel()
	st := newMem()
	target := &fakeTarget{boot: "boot-2"}
	s := newService(t, st, target)
	if _, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "unknown", func() bool {
		d, _, _ := st.Deployment(t.Context(), "sample")
		return d.State() == deploy.StateUnknown
	})
}

func TestApplyBusyInTheRecord(t *testing.T) {
	t.Parallel()
	st := newMem()
	s := newService(t, st, &fakeTarget{boot: "boot-1"})
	if _, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil); !errors.Is(err, deploy.ErrBusy) {
		t.Errorf("second apply: %v, want ErrBusy", err)
	}
}

func TestReviewListsPlacedCredentials(t *testing.T) {
	t.Parallel()
	s := newService(t, newMem(), &fakeTarget{present: []string{"secrets/app/uses_db_password"}})
	r, err := s.Review(t.Context(), "sample", sample)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Placed) != 1 || !r.Placed[0].Exists || r.Placed[0].Field != "password" || len(r.Modules) != 1 {
		t.Errorf("review %+v", r)
	}
}

func TestStateIsDerived(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	for _, tc := range []struct {
		op   *deploy.Operation
		want deploy.State
	}{
		{nil, deploy.StateApplying},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusRunning}, deploy.StateApplying},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusRunning, RollbackOf: &id}, deploy.StateRollingBack},
		{&deploy.Operation{Action: deploy.ActionRemove, Status: deploy.StatusRunning}, deploy.StateRemoving},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusSucceeded}, deploy.StateRunning},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusSucceeded, RollbackOf: &id}, deploy.StateRolledBack},
		{&deploy.Operation{Action: deploy.ActionRemove, Status: deploy.StatusSucceeded}, deploy.StateRemoved},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusFailed}, deploy.StateFailed},
		{&deploy.Operation{Action: deploy.ActionApply, Status: deploy.StatusUnknown}, deploy.StateUnknown},
	} {
		if got := (deploy.Deployment{Latest: tc.op}).State(); got != tc.want {
			t.Errorf("%+v: %s, want %s", tc.op, got, tc.want)
		}
	}
}

// TestPlacedValuesNeverLeak applies with a placed value, through a run to running, a refusal by
// the deploy target and a deploy target that does not answer, then searches every log line,
// every answer and everything the store holds for it: only paths travel back (spec 0006, AC-11).
func TestPlacedValuesNeverLeak(t *testing.T) {
	t.Parallel()
	const value = "placed-value-6f1c0de5"
	placed := []deploy.PlacedInput{{Path: "secrets/app/uses_db_password", Value: value}}
	for name, target := range map[string]*fakeTarget{
		"runs":        {boot: "boot-1", polls: []deploytarget.OperationStatus{deploytarget.OperationStatusSucceeded}},
		"refused":     {applyErr: &deploy.RefusedError{Problems: []contract.Problem{{Code: "placed_invalid", Path: "placed.secrets/app/uses_db_password"}}}},
		"unavailable": {applyErr: deploy.ErrTargetUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newMem()
			var logs bytes.Buffer
			ctx, cancel := context.WithCancel(t.Context())
			s := deploy.NewService(ctx, st, fakeRenderer{digest: "sha256:abc"}, target, deploy.NewHub(),
				slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { cancel(); s.Wait() })
			deploy.SetPollEvery(s, 5*time.Millisecond)

			op, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", placed)
			seen := []any{op, fmt.Sprint(err)}
			if err == nil {
				waitFor(t, "a final status", func() bool {
					o, _, _ := st.Operation(t.Context(), op.ID)
					return o.Status != deploy.StatusRunning
				})
				history, err := s.Operations(t.Context(), "sample", nil, 50)
				if err != nil {
					t.Fatal(err)
				}
				view, err := s.View(t.Context(), "sample")
				if err != nil {
					t.Fatal(err)
				}
				seen = append(seen, history, view)
			}
			st.mu.Lock()
			for _, o := range st.ops {
				seen = append(seen, *o)
			}
			st.mu.Unlock()
			cancel()
			s.Wait()
			for _, v := range seen {
				data, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte(value)) || strings.Contains(fmt.Sprintf("%+v", v), value) {
					t.Errorf("the placed value leaked into %T: %s", v, data)
				}
			}
			if bytes.Contains(logs.Bytes(), []byte(value)) {
				t.Errorf("the placed value leaked into the log:\n%s", logs.String())
			}
		})
	}
}
