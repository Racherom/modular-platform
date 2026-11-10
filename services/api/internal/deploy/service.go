package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/sdk"
)

// Timing of calls to the deploy target and of polling (spec 0006).
const (
	targetTimeout = 10 * time.Second
	pollEvery     = time.Second
	failingFor    = 60 * time.Second
	runningFor    = 20 * time.Minute
)

// Service orchestrates deployments.
type Service struct {
	store    Store
	renderer Renderer
	target   Target
	hub      *Hub
	log      *slog.Logger
	//nolint:containedctx // the service's lifetime: pollers outlive the request that started them and end with it
	ctx        context.Context
	now        func() time.Time
	every      time.Duration
	watchEvery time.Duration
	retryAfter time.Duration

	mu          sync.Mutex
	polling     map[uuid.UUID]bool
	watches     map[string]*watch // by deployment name
	nextWatcher uint64
	wg          sync.WaitGroup
}

// NewService returns the service; its pollers run until ctx ends.
func NewService(ctx context.Context, store Store, renderer Renderer, target Target, hub *Hub, log *slog.Logger) *Service {
	return &Service{
		store: store, renderer: renderer, target: target, hub: hub, log: log, ctx: ctx,
		now: time.Now, every: pollEvery, watchEvery: watchEvery, retryAfter: retryAfter,
		polling: map[uuid.UUID]bool{}, watches: map[string]*watch{},
	}
}

// Wait blocks until every poller has stopped, for tests.
func (s *Service) Wait() { s.wg.Wait() }

// checkName refuses a graph whose name is not the deployment's, and the platform's own name.
func checkName(name string, g contract.Graph) error {
	if g.Name != name {
		return refused("name_mismatch", "graph.name", fmt.Sprintf("The graph is named %q, not %q.", g.Name, name))
	}
	if name == ReservedName {
		return refused("reserved_name", "graph.name", "The name modular-platform belongs to the platform itself.")
	}
	return nil
}

// Review renders g as an apply would and lists what it would run. It writes nothing and
// touches nothing on the host.
func (s *Service) Review(ctx context.Context, name string, g contract.Graph) (Review, error) {
	if err := checkName(name, g); err != nil {
		return Review{}, err
	}
	id := uuid.Nil
	if d, ok, err := s.store.Deployment(ctx, name); err != nil {
		return Review{}, err
	} else if ok {
		id = d.ID
	}
	project, err := s.renderer.RenderManaged(ctx, g, id)
	if err != nil {
		return Review{}, err
	}
	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	present, err := s.target.Secrets(tctx, project.Project)
	if err != nil {
		return Review{}, err
	}
	r := Review{Check: project.Check, Modules: project.Images, PublishedPorts: project.PublishedPorts}
	for _, sec := range project.Secrets {
		if sec.Kind == contract.Placed {
			r.Placed = append(r.Placed, PlacedCredential{
				Path: sec.Path, Instance: sec.Instance, Capability: sec.Capability, Field: sec.Field,
				Exists: slices.Contains(present, sec.Path),
			})
		}
	}
	return r, nil
}

// Apply starts an apply of g after the review whose digest is digest (spec 0006, "Apply
// dispatch order"). A refusal leaves no trace. When the deploy target does not answer, the
// operation is recorded as failed with deployer_unavailable and returned without error.
func (s *Service) Apply(ctx context.Context, by Starter, name string, g contract.Graph, digest string, placed []PlacedInput) (Operation, error) {
	if err := checkName(name, g); err != nil {
		return Operation{}, err
	}
	result, err := s.renderer.Check(ctx, g)
	if err != nil {
		return Operation{}, err
	}
	if !result.Ok {
		return Operation{}, &RefusedError{Problems: result.Problems}
	}
	if result.Digest != digest {
		return Operation{}, ErrGraphChanged
	}
	id, project, created, err := s.store.EnsureDeployment(ctx, name)
	if err != nil {
		return Operation{}, err
	}
	dropNew := func() {
		if created {
			if err := s.store.DropNewDeployment(context.WithoutCancel(ctx), id); err != nil {
				s.log.ErrorContext(ctx, "dropping a refused deployment failed", slog.String("deployment", name), slog.Any("error", err))
			}
		}
	}
	rendered, err := s.renderer.RenderManaged(ctx, g, id)
	if err != nil {
		dropNew()
		return Operation{}, err
	}
	graph, err := json.Marshal(g)
	if err != nil {
		dropNew()
		return Operation{}, fmt.Errorf("deploy: %w", err)
	}
	req := applyRequest(id, rendered)
	supplied := []string{}
	for _, p := range placed {
		req.Placed = append(req.Placed, deploytarget.PlacedValue{Path: p.Path, Data: []byte(p.Value), Replace: p.Replace})
		supplied = append(supplied, p.Path)
	}
	op, err := s.store.StartOperation(ctx, Operation{
		DeploymentID: id, Action: ActionApply, Graph: graph, Digest: result.Digest,
		PlacedSupplied: supplied, StartedBy: by,
	})
	if err != nil {
		dropNew()
		return Operation{}, err
	}

	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	accepted, err := s.target.Apply(tctx, project, req)
	switch {
	case err == nil:
	case errors.Is(err, ErrTargetUnavailable):
		s.log.WarnContext(ctx, "deploy target unavailable at apply", slog.String("deployment", name), slog.Any("error", err))
		if _, err := s.store.FinishOperation(context.WithoutCancel(ctx), op.ID, StatusFailed, StepPrepare, "deployer_unavailable"); err != nil {
			return Operation{}, err
		}
		s.logEnd(ctx, name, op.ID, StatusFailed, StepPrepare, by.Subject)
		return s.publishOperation(ctx, name, op.ID)
	default:
		// a refusal leaves no trace: drop what this request wrote
		if derr := s.store.DropOperation(context.WithoutCancel(ctx), op); derr != nil {
			return Operation{}, errors.Join(err, derr)
		}
		dropNew()
		if errors.Is(err, ErrTarget) {
			s.log.ErrorContext(ctx, "deploy target refused api's call", slog.String("deployment", name), slog.Any("error", err))
		}
		return Operation{}, err
	}
	if err := s.store.AcceptOperation(ctx, op.ID, accepted.Id, accepted.BootID); err != nil {
		return Operation{}, err
	}
	s.log.InfoContext(ctx, "operation started", slog.String("deployment", name), slog.String("operation", op.ID.String()),
		slog.String("action", string(ActionApply)), slog.String("caller", by.Subject))
	op.DeployerOperationID, op.DeployerBootID = accepted.Id, accepted.BootID
	s.startPolling(RunningOperation{Operation: op, DeploymentName: name, Project: project})
	return s.publishOperation(ctx, name, op.ID)
}

// applyRequest is the deploy target's apply of a rendered project, without placed values.
func applyRequest(deployment uuid.UUID, rendered contract.RenderedProject) deploytarget.ApplyRequest {
	req := deploytarget.ApplyRequest{Deployment: deployment}
	for _, f := range rendered.Files {
		req.Files = append(req.Files, deploytarget.ProjectFile{Path: f.Path, Mode: f.Mode, Data: f.Data})
	}
	req.Secrets = make([]deploytarget.ProjectSecret, 0, len(rendered.Secrets))
	for _, sec := range rendered.Secrets {
		req.Secrets = append(req.Secrets, deploytarget.ProjectSecret{
			Path: sec.Path, Kind: deploytarget.ProjectSecretKind(sec.Kind), Instance: sec.Instance,
			Capability: sec.Capability, Field: sec.Field,
		})
	}
	return req
}

// Remove starts a remove of the deployment named name (AC-15). It keeps volumes and secrets
// unless deleteData is set, which needs confirmName equal to name. A plain remove of a removed
// deployment is ErrAlreadyRemoved. As with Apply, a refusal leaves no trace, and a deploy target
// that does not answer gives an operation recorded as failed with deployer_unavailable.
func (s *Service) Remove(ctx context.Context, by Starter, name string, deleteData bool, confirmName string) (Operation, error) {
	d, ok, err := s.store.Deployment(ctx, name)
	if err != nil {
		return Operation{}, err
	}
	if !ok {
		return Operation{}, ErrNotFound
	}
	if deleteData && confirmName != name {
		return Operation{}, refused("confirm_mismatch", "confirmName",
			fmt.Sprintf("Type the deployment's name, %s, to delete its data.", name))
	}
	if !deleteData && d.State() == StateRemoved {
		return Operation{}, ErrAlreadyRemoved
	}
	op, err := s.store.StartOperation(ctx, Operation{DeploymentID: d.ID, Action: ActionRemove, DeleteData: deleteData, StartedBy: by})
	if err != nil {
		return Operation{}, err
	}

	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	accepted, err := s.target.Remove(tctx, d.Project, deleteData)
	switch {
	case err == nil:
	case errors.Is(err, ErrTargetUnavailable):
		s.log.WarnContext(ctx, "deploy target unavailable at remove", slog.String("deployment", name), slog.Any("error", err))
		if _, err := s.store.FinishOperation(context.WithoutCancel(ctx), op.ID, StatusFailed, StepPrepare, "deployer_unavailable"); err != nil {
			return Operation{}, err
		}
		s.logEnd(ctx, name, op.ID, StatusFailed, StepPrepare, by.Subject)
		return s.publishOperation(ctx, name, op.ID)
	default:
		if derr := s.store.DropOperation(context.WithoutCancel(ctx), op); derr != nil {
			return Operation{}, errors.Join(err, derr)
		}
		if errors.Is(err, ErrTarget) {
			s.log.ErrorContext(ctx, "deploy target refused api's call", slog.String("deployment", name), slog.Any("error", err))
		}
		return Operation{}, err
	}
	if err := s.store.AcceptOperation(ctx, op.ID, accepted.Id, accepted.BootID); err != nil {
		return Operation{}, err
	}
	s.log.InfoContext(ctx, "operation started", slog.String("deployment", name), slog.String("operation", op.ID.String()),
		slog.String("action", string(ActionRemove)), slog.Bool("delete_data", deleteData), slog.String("caller", by.Subject))
	op.DeployerOperationID, op.DeployerBootID = accepted.Id, accepted.BootID
	s.startPolling(RunningOperation{Operation: op, DeploymentName: name, Project: d.Project})
	return s.publishOperation(ctx, name, op.ID)
}

func (s *Service) logEnd(ctx context.Context, deployment string, id uuid.UUID, status Status, step Step, caller string) {
	s.log.InfoContext(ctx, "operation finished", slog.String("deployment", deployment), slog.String("operation", id.String()),
		slog.String("status", string(status)), slog.String("step", string(step)), slog.String("caller", caller))
}

// publishOperation reads the operation again and tells every stream about it and its deployment.
func (s *Service) publishOperation(ctx context.Context, name string, id uuid.UUID) (Operation, error) {
	op, ok, err := s.store.Operation(ctx, id)
	if err != nil || !ok {
		return Operation{}, errors.Join(err, fmt.Errorf("deploy: operation %s is gone", id))
	}
	s.hub.Publish(Event{Kind: EventOperation, Deployment: name, Operation: &op})
	if d, ok, err := s.store.Deployment(ctx, name); err == nil && ok {
		s.hub.Publish(Event{Kind: EventDeployment, Deployment: name, Summary: &d})
	}
	return op, nil
}

// Deployments lists one page of deployments sorted by name.
func (s *Service) Deployments(ctx context.Context, after string, limit int) ([]Deployment, error) {
	return s.store.Deployments(ctx, after, limit)
}

// Operations lists one page of a deployment's history, newest first.
func (s *Service) Operations(ctx context.Context, name string, before *uuid.UUID, limit int) ([]Operation, error) {
	d, ok, err := s.store.Deployment(ctx, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.store.Operations(ctx, d.ID, before, limit)
}

// View reads a deployment with its running graph and, unless it is removed, the live state of
// its modules. A state read that fails is reported in StateError, not as an error.
func (s *Service) View(ctx context.Context, name string) (View, error) {
	d, ok, err := s.store.Deployment(ctx, name)
	if err != nil {
		return View{}, err
	}
	if !ok {
		return View{}, ErrNotFound
	}
	v := View{Deployment: d}
	if v.Graph, err = graphOf(s.store.LatestSucceededApply(ctx, d.ID)); err != nil {
		return View{}, err
	}
	if v.LatestApplyGraph, err = graphOf(s.store.LatestApply(ctx, d.ID)); err != nil {
		return View{}, err
	}
	if d.Latest != nil && d.Latest.RollbackOf != nil && d.State() == StateRolledBack {
		if from, ok, err := s.store.Operation(ctx, *d.Latest.RollbackOf); err == nil && ok {
			v.RolledBackFrom = &from
		}
	}
	if d.State() == StateRemoved {
		v.Modules = []ModuleState{}
		return v, nil
	}
	v.Modules, err = s.Modules(ctx, d)
	if err != nil {
		v.Modules, v.StateError = nil, err.Error()
	}
	return v, nil
}

// graphOf decodes the graph of an apply a store read found; nil when it found none.
func graphOf(op Operation, ok bool, err error) (*contract.Graph, error) {
	if err != nil || !ok {
		return nil, err
	}
	var g contract.Graph
	if err := json.Unmarshal(op.Graph, &g); err != nil {
		return nil, nil //nolint:nilerr // a stored graph always decodes; one that does not shows as none
	}
	return &g, nil
}

// Modules reads the live state of d's modules: one entry per instance of the latest apply that
// reached up, missing (or pending while an operation runs) when it has no container, plus any
// container the graph does not name. Sorted by instance.
func (s *Service) Modules(ctx context.Context, d Deployment) ([]ModuleState, error) {
	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	state, err := s.target.State(tctx, d.Project)
	if err != nil {
		return nil, err
	}
	byInstance := map[string]ModuleState{}
	for _, svc := range state.Services {
		byInstance[svc.Instance] = ModuleState{
			Instance: svc.Instance, Module: svc.Module, Image: svc.Image, State: string(svc.State), Health: svc.Health,
			ExitCode: svc.ExitCode, LastExitCode: svc.LastExitCode, RestartCount: svc.RestartCount, Since: svc.Since,
		}
	}
	expected, err := s.expectedModules(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	absent := "missing"
	if d.Latest != nil && d.Latest.Status == StatusRunning {
		absent = "pending"
	}
	for i, module := range expected {
		if _, ok := byInstance[i]; !ok {
			byInstance[i] = ModuleState{Instance: i, Module: module, State: absent}
		}
	}
	out := make([]ModuleState, 0, len(byInstance))
	for _, m := range byInstance {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b ModuleState) int { return strings.Compare(a.Instance, b.Instance) })
	return out, nil
}

// expectedModules maps each instance of the latest apply that reached up to its module.
func (s *Service) expectedModules(ctx context.Context, deploymentID uuid.UUID) (map[string]string, error) {
	op, ok, err := s.store.LatestApplyReachedUp(ctx, deploymentID)
	if err != nil || !ok {
		return nil, err
	}
	var g contract.Graph
	// a stored graph always decodes; one that does not expects nothing
	_ = json.Unmarshal(op.Graph, &g)
	out := make(map[string]string, len(g.Modules))
	for i, m := range g.Modules {
		out[i] = m.Module
	}
	return out, nil
}

// Resume polls every operation still running, as at api's start: under the same boot id
// polling picks up where it was; a new boot id or a 404 marks it unknown.
func (s *Service) Resume(ctx context.Context) error {
	running, err := s.store.RunningOperations(ctx)
	if err != nil {
		return err
	}
	for _, op := range running {
		if op.DeployerOperationID == "" {
			// the deploy target never accepted it: api stopped between insert and dispatch
			s.finish(op, StatusUnknown, op.Step, "api restarted before the deploy target answered")
			continue
		}
		s.startPolling(op)
	}
	return nil
}

// callerCtx carries the operation's starter as the MP-Caller subject of background calls.
func callerCtx(ctx context.Context, by Starter) context.Context {
	return sdk.WithCaller(ctx, sdk.Caller{Issuer: by.Issuer, User: by.Subject})
}
