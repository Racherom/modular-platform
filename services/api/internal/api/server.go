package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

// Page sizes of the list routes.
const (
	defaultPage = 50
	maxPage     = 200
)

// Planner is the part of the planner the editor reaches through api.
type Planner interface {
	Catalog(ctx context.Context) (contract.Catalog, error)
	Plan(ctx context.Context, g contract.Graph) (contract.PlanResult, error)
	Render(ctx context.Context, g contract.Graph) (zip []byte, disposition string, plan *contract.PlanResult, err error)
}

// Server implements the strict part of api's contract.
type Server struct {
	signin  *signin.Service
	deploy  *deploy.Service
	planner Planner
	log     *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// GetSession answers who is signed in.
func (s *Server) GetSession(ctx context.Context, _ GetSessionRequestObject) (GetSessionResponseObject, error) {
	sess := sessionFrom(ctx)
	return GetSession200JSONResponse{Email: sess.Email, ExpiresAt: sess.ExpiresAt, IdleExpiresAt: sess.IdleExpiresAt}, nil
}

// GetCatalog lists the planner's catalog.
func (s *Server) GetCatalog(ctx context.Context, _ GetCatalogRequestObject) (GetCatalogResponseObject, error) {
	cat, err := s.planner.Catalog(ctx)
	if err != nil {
		return GetCatalog502JSONResponse{PlannerErrorJSONResponse(s.plannerProblem(ctx, err))}, nil
	}
	return GetCatalog200JSONResponse(cat), nil
}

// Plan plans a graph through the planner.
func (s *Server) Plan(ctx context.Context, req PlanRequestObject) (PlanResponseObject, error) {
	plan, err := s.planner.Plan(ctx, *req.Body)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return Plan400JSONResponse{BadRequestJSONResponse{Problems: refused.Problems}}, nil
	}
	if err != nil {
		return Plan502JSONResponse{PlannerErrorJSONResponse(s.plannerProblem(ctx, err))}, nil
	}
	return Plan200JSONResponse(plan), nil
}

// Render answers the static zip, byte for byte what the planner gives.
func (s *Server) Render(ctx context.Context, req RenderRequestObject) (RenderResponseObject, error) {
	zip, disposition, plan, err := s.planner.Render(ctx, *req.Body)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return Render400JSONResponse{BadRequestJSONResponse{Problems: refused.Problems}}, nil
	}
	if err != nil {
		return Render502JSONResponse{PlannerErrorJSONResponse(s.plannerProblem(ctx, err))}, nil
	}
	if plan != nil {
		return Render422JSONResponse(*plan), nil
	}
	return Render200ApplicationzipResponse{
		Body: bytes.NewReader(zip), ContentLength: int64(len(zip)),
		Headers: Render200ResponseHeaders{ContentDisposition: disposition},
	}, nil
}

// ListDeployments lists one page of deployments by name.
func (s *Server) ListDeployments(ctx context.Context, req ListDeploymentsRequestObject) (ListDeploymentsResponseObject, error) {
	limit, ok := pageSize(req.Params.Limit)
	if !ok {
		return ListDeployments400JSONResponse{BadRequestJSONResponse(badLimit())}, nil
	}
	items, err := s.deploy.Deployments(ctx, req.Params.Cursor, limit+1)
	if err != nil {
		return nil, err
	}
	page := DeploymentPage{Items: make([]DeploymentSummary, 0, min(len(items), limit))}
	for i, d := range items {
		if i == limit {
			page.Next = items[i-1].Name
			break
		}
		page.Items = append(page.Items, summary(d))
	}
	return ListDeployments200JSONResponse(page), nil
}

// GetDeployment reads a deployment with the live state of its modules.
func (s *Server) GetDeployment(ctx context.Context, req GetDeploymentRequestObject) (GetDeploymentResponseObject, error) {
	v, err := s.deploy.View(ctx, req.Name)
	if errors.Is(err, deploy.ErrNotFound) {
		return GetDeployment404JSONResponse{NotFoundJSONResponse(notFound(req.Name))}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetDeployment200JSONResponse(view(v)), nil
}

// ListOperations lists one page of a deployment's history, newest first.
func (s *Server) ListOperations(ctx context.Context, req ListOperationsRequestObject) (ListOperationsResponseObject, error) {
	limit, ok := pageSize(req.Params.Limit)
	if !ok {
		return ListOperations400JSONResponse{BadRequestJSONResponse(badLimit())}, nil
	}
	before, ok := cursorID(req.Params.Cursor)
	if !ok {
		return ListOperations400JSONResponse{BadRequestJSONResponse(problems("invalid_request", "cursor", "The cursor is not an operation id."))}, nil
	}
	items, err := s.deploy.Operations(ctx, req.Name, before, limit+1)
	if errors.Is(err, deploy.ErrNotFound) {
		return ListOperations404JSONResponse{NotFoundJSONResponse(notFound(req.Name))}, nil
	}
	if err != nil {
		return nil, err
	}
	page := OperationPage{Items: make([]Operation, 0, min(len(items), limit))}
	for i, op := range items {
		if i == limit {
			page.Next = items[i-1].ID.String()
			break
		}
		page.Items = append(page.Items, operation(op))
	}
	return ListOperations200JSONResponse(page), nil
}

// ReviewDeployment shows what an apply of the graph would run.
func (s *Server) ReviewDeployment(ctx context.Context, req ReviewDeploymentRequestObject) (ReviewDeploymentResponseObject, error) {
	r, err := s.deploy.Review(ctx, req.Name, req.Body.Graph)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return ReviewDeployment422JSONResponse{RefusedJSONResponse{Problems: refused.Problems}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrPlanner):
		return ReviewDeployment502JSONResponse{PlannerErrorJSONResponse(s.plannerProblem(ctx, err))}, nil
	case errors.Is(err, deploy.ErrTargetUnavailable), errors.Is(err, deploy.ErrTarget):
		s.log.WarnContext(ctx, "deploy target failed at review", slog.Any("error", err))
		return ReviewDeployment503JSONResponse{DeployerUnavailableJSONResponse(problems("deployer_unavailable", "", "The deployer did not answer; try again."))}, nil
	case err != nil:
		return nil, err
	}
	out := Review{Plan: r.Plan, Modules: r.Modules, Bindings: r.Plan.Bindings, PublishedPorts: r.PublishedPorts, Placed: []PlacedCredential{}}
	if out.PublishedPorts == nil {
		out.PublishedPorts = []contract.PublishedPort{}
	}
	for _, p := range r.Placed {
		out.Placed = append(out.Placed, PlacedCredential{
			Path: p.Path, Instance: p.Instance, Capability: p.Capability, Field: PlacedCredentialField(p.Field), Exists: p.Exists,
		})
	}
	return ReviewDeployment200JSONResponse(out), nil
}

// ApplyDeployment deploys a reviewed graph.
func (s *Server) ApplyDeployment(ctx context.Context, req ApplyDeploymentRequestObject) (ApplyDeploymentResponseObject, error) {
	sess := sessionFrom(ctx)
	by := deploy.Starter{Issuer: sess.Identity.Issuer, Subject: sess.Identity.Subject, Email: sess.Email}
	placed := make([]deploy.PlacedInput, 0, len(req.Body.Placed))
	for _, p := range req.Body.Placed {
		placed = append(placed, deploy.PlacedInput{Path: p.Path, Value: p.Value, Replace: p.Replace})
	}
	op, err := s.deploy.Apply(ctx, by, req.Name, req.Body.Graph, req.Body.PlanDigest, placed)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return ApplyDeployment422JSONResponse{RefusedJSONResponse{Problems: refused.Problems}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrBusy):
		return ApplyDeployment409JSONResponse(problems("busy", "", "An operation of this deployment is still running.")), nil
	case errors.Is(err, deploy.ErrPlanChanged):
		return ApplyDeployment409JSONResponse(problems("plan_changed", "planDigest",
			"The plan changed since you reviewed it, for example after a platform upgrade; review it again.")), nil
	case errors.Is(err, deploy.ErrPlanner):
		return ApplyDeployment502JSONResponse{PlannerErrorJSONResponse(s.plannerProblem(ctx, err))}, nil
	case errors.Is(err, deploy.ErrTarget):
		return ApplyDeployment502JSONResponse{PlannerErrorJSONResponse(problems("deployer_error", "", "The deployer refused api's call; see api's log."))}, nil
	case err != nil:
		return nil, err
	}
	return ApplyDeployment202JSONResponse(operation(op)), nil
}

// RemoveDeployment removes a deployment's containers, and with deleteData its data too.
func (s *Server) RemoveDeployment(ctx context.Context, req RemoveDeploymentRequestObject) (RemoveDeploymentResponseObject, error) {
	sess := sessionFrom(ctx)
	by := deploy.Starter{Issuer: sess.Identity.Issuer, Subject: sess.Identity.Subject, Email: sess.Email}
	op, err := s.deploy.Remove(ctx, by, req.Name, req.Body.DeleteData, req.Body.ConfirmName)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return RemoveDeployment422JSONResponse{Problems: refused.Problems}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		return RemoveDeployment404JSONResponse{NotFoundJSONResponse(notFound(req.Name))}, nil
	case errors.Is(err, deploy.ErrBusy):
		return RemoveDeployment409JSONResponse(problems("busy", "", "An operation of this deployment is still running.")), nil
	case errors.Is(err, deploy.ErrAlreadyRemoved):
		return RemoveDeployment409JSONResponse(problems("already_removed", "", "The deployment is removed already; choose Delete data too to delete its data.")), nil
	case errors.Is(err, deploy.ErrTarget):
		return RemoveDeployment502JSONResponse(problems("deployer_error", "", "The deployer refused api's call; see api's log.")), nil
	case err != nil:
		return nil, err
	}
	return RemoveDeployment202JSONResponse(operation(op)), nil
}

func (s *Server) plannerProblem(ctx context.Context, err error) contract.ProblemList {
	s.log.WarnContext(ctx, "planner failed", slog.Any("error", err))
	return problems("planner_error", "", "The planner did not answer as expected; try again.")
}

// cursorID reads an operations cursor: none, or an operation id.
func cursorID(cursor string) (*uuid.UUID, bool) {
	if cursor == "" {
		return nil, true
	}
	id, err := uuid.Parse(cursor)
	if err != nil {
		return nil, false
	}
	return &id, true
}

func pageSize(limit int) (int, bool) {
	if limit == 0 {
		return defaultPage, true
	}
	return limit, limit >= 1 && limit <= maxPage
}

func badLimit() contract.ProblemList {
	return problems("invalid_request", "limit", fmt.Sprintf("limit must be from 1 to %d.", maxPage))
}

func notFound(name string) contract.ProblemList {
	return problems("not_found", "name", fmt.Sprintf("No deployment is named %q.", name))
}

func problems(code, path, message string) contract.ProblemList {
	return contract.ProblemList{Problems: []contract.Problem{{Severity: contract.Error, Code: code, Path: path, Message: message}}}
}
