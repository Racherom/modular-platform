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

// Renderer is the part of the renderer the editor reaches through api.
type Renderer interface {
	Catalog(ctx context.Context) (contract.Catalog, error)
	Check(ctx context.Context, g contract.Graph) (contract.CheckResult, error)
	Render(ctx context.Context, g contract.Graph) (zip []byte, disposition string, result *contract.CheckResult, err error)
}

// Server implements the strict part of api's contract.
type Server struct {
	signin   *signin.Service
	deploy   *deploy.Service
	renderer Renderer
	log      *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// GetSession answers who is signed in.
func (s *Server) GetSession(ctx context.Context, _ GetSessionRequestObject) (GetSessionResponseObject, error) {
	sess := sessionFrom(ctx)
	return GetSession200JSONResponse{Email: sess.Email, ExpiresAt: sess.ExpiresAt, IdleExpiresAt: sess.IdleExpiresAt}, nil
}

// GetCatalog lists the renderer's catalog.
func (s *Server) GetCatalog(ctx context.Context, _ GetCatalogRequestObject) (GetCatalogResponseObject, error) {
	cat, err := s.renderer.Catalog(ctx)
	if err != nil {
		return GetCatalog502JSONResponse{RendererErrorJSONResponse(s.rendererProblem(ctx, err))}, nil
	}
	return GetCatalog200JSONResponse(cat), nil
}

// Check checks a graph through the renderer.
func (s *Server) Check(ctx context.Context, req CheckRequestObject) (CheckResponseObject, error) {
	result, err := s.renderer.Check(ctx, *req.Body)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return Check400JSONResponse{BadRequestJSONResponse{Problems: refused.Problems}}, nil
	}
	if err != nil {
		return Check502JSONResponse{RendererErrorJSONResponse(s.rendererProblem(ctx, err))}, nil
	}
	return Check200JSONResponse(result), nil
}

// Render answers the static zip, byte for byte what the renderer gives.
func (s *Server) Render(ctx context.Context, req RenderRequestObject) (RenderResponseObject, error) {
	zip, disposition, result, err := s.renderer.Render(ctx, *req.Body)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return Render400JSONResponse{BadRequestJSONResponse{Problems: refused.Problems}}, nil
	}
	if err != nil {
		return Render502JSONResponse{RendererErrorJSONResponse(s.rendererProblem(ctx, err))}, nil
	}
	if result != nil {
		return Render422JSONResponse(*result), nil
	}
	return Render200ApplicationzipResponse{
		Body: bytes.NewReader(zip), ContentLength: int64(len(zip)),
		Headers: Render200ResponseHeaders{ContentDisposition: disposition},
	}, nil
}

// ListStacks lists one page of stacks by name.
func (s *Server) ListStacks(ctx context.Context, req ListStacksRequestObject) (ListStacksResponseObject, error) {
	limit, ok := pageSize(req.Params.Limit)
	if !ok {
		return ListStacks400JSONResponse{BadRequestJSONResponse(badLimit())}, nil
	}
	items, err := s.deploy.Stacks(ctx, req.Params.Cursor, limit+1)
	if err != nil {
		return nil, err
	}
	page := StackPage{Items: make([]StackSummary, 0, min(len(items), limit))}
	for i, d := range items {
		if i == limit {
			page.Next = items[i-1].Name
			break
		}
		page.Items = append(page.Items, summary(d))
	}
	return ListStacks200JSONResponse(page), nil
}

// GetStack reads a stack with the live state of its modules.
func (s *Server) GetStack(ctx context.Context, req GetStackRequestObject) (GetStackResponseObject, error) {
	v, err := s.deploy.View(ctx, req.Name)
	if errors.Is(err, deploy.ErrNotFound) {
		return GetStack404JSONResponse{NotFoundJSONResponse(notFound(req.Name))}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetStack200JSONResponse(view(v)), nil
}

// ListOperations lists one page of a stack's history, newest first.
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

// ReviewStack shows what an apply of the graph would run.
func (s *Server) ReviewStack(ctx context.Context, req ReviewStackRequestObject) (ReviewStackResponseObject, error) {
	r, err := s.deploy.Review(ctx, req.Name, req.Body.Graph, deploy.ImageOptions{CheckNewer: req.Body.CheckNewer, Update: req.Body.Update})
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return ReviewStack422JSONResponse{RefusedJSONResponse{Problems: refused.Problems}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrBusy):
		return ReviewStack409JSONResponse(problems("busy", "", "An operation of this stack is still running; review again when it ends.")), nil
	case errors.Is(err, deploy.ErrLockInvalid):
		s.log.ErrorContext(ctx, "deploy target cannot read a stack's image lock", slog.Any("error", err))
		return ReviewStack503JSONResponse{DeployerUnavailableJSONResponse(problems("lock_invalid", "", "The deployer cannot read this stack's image lock; see its log."))}, nil
	case errors.Is(err, deploy.ErrRenderer):
		return ReviewStack502JSONResponse{RendererErrorJSONResponse(s.rendererProblem(ctx, err))}, nil
	case errors.Is(err, deploy.ErrTargetUnavailable), errors.Is(err, deploy.ErrTarget):
		s.log.WarnContext(ctx, "deploy target failed at review", slog.Any("error", err))
		return ReviewStack503JSONResponse{DeployerUnavailableJSONResponse(problems("deployer_unavailable", "", "The deployer did not answer; try again."))}, nil
	case err != nil:
		return nil, err
	}
	out := Review{
		Check: r.Check, Digest: r.Digest, Modules: r.Modules, Images: reviewImages(r.Images), Bindings: r.Check.Bindings,
		PublishedPorts: r.PublishedPorts, Placed: []PlacedCredential{},
	}
	if out.PublishedPorts == nil {
		out.PublishedPorts = []contract.PublishedPort{}
	}
	for _, p := range r.Placed {
		out.Placed = append(out.Placed, PlacedCredential{
			Path: p.Path, Instance: p.Instance, Capability: p.Capability, Field: p.Field, Exists: p.Exists,
		})
	}
	return ReviewStack200JSONResponse(out), nil
}

// ApplyStack deploys a reviewed graph.
func (s *Server) ApplyStack(ctx context.Context, req ApplyStackRequestObject) (ApplyStackResponseObject, error) {
	sess := sessionFrom(ctx)
	by := deploy.Starter{Issuer: sess.Identity.Issuer, Subject: sess.Identity.Subject, Email: sess.Email}
	placed := make([]deploy.PlacedInput, 0, len(req.Body.Placed))
	for _, p := range req.Body.Placed {
		placed = append(placed, deploy.PlacedInput{Path: p.Path, Value: p.Value, Replace: p.Replace})
	}
	op, err := s.deploy.Apply(ctx, by, req.Name, req.Body.Graph, req.Body.Digest, deploy.ImageOptions{Update: req.Body.Update}, placed)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return ApplyStack422JSONResponse{RefusedJSONResponse{Problems: refused.Problems}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrBusy):
		return ApplyStack409JSONResponse(problems("busy", "", "An operation of this stack is still running.")), nil
	case errors.Is(err, deploy.ErrGraphChanged):
		return ApplyStack409JSONResponse(problems("graph_changed", "digest",
			"The graph or an image version changed since your review. Review again.")), nil
	case errors.Is(err, deploy.ErrLockInvalid):
		s.log.ErrorContext(ctx, "deploy target cannot read a stack's image lock", slog.Any("error", err))
		return ApplyStack503JSONResponse{DeployerUnavailableJSONResponse(problems("lock_invalid", "", "The deployer cannot read this stack's image lock; see its log."))}, nil
	case errors.Is(err, deploy.ErrTargetUnavailable):
		s.log.WarnContext(ctx, "deploy target failed before apply", slog.Any("error", err))
		return ApplyStack503JSONResponse{DeployerUnavailableJSONResponse(problems("deployer_unavailable", "", "The deployer did not answer; try again."))}, nil
	case errors.Is(err, deploy.ErrRenderer):
		return ApplyStack502JSONResponse{RendererErrorJSONResponse(s.rendererProblem(ctx, err))}, nil
	case errors.Is(err, deploy.ErrTarget):
		return ApplyStack502JSONResponse{RendererErrorJSONResponse(problems("deployer_error", "", "The deployer refused api's call; see api's log."))}, nil
	case err != nil:
		return nil, err
	}
	return ApplyStack202JSONResponse(operation(op)), nil
}

// reviewImages maps the review's Images section onto the contract.
func reviewImages(images []deploy.ReviewImage) []ReviewImage {
	out := make([]ReviewImage, 0, len(images))
	for _, img := range images {
		users := make([]ReviewImageUser, 0, len(img.UsedBy))
		for _, u := range img.UsedBy {
			users = append(users, ReviewImageUser{
				Kind: ReviewImageUserKind(u.Kind), Instance: u.Instance, Capability: u.Capability, Consumer: u.Consumer,
			})
		}
		out = append(out, ReviewImage{
			Reference: img.Reference, UsedBy: users, Digest: img.Digest, Change: ReviewImageChange(img.Change),
			PreviousDigest: img.PreviousDigest, Available: img.Available, Present: img.Present,
		})
	}
	return out
}

// RemoveStack removes a stack's containers, and with deleteData its data too.
func (s *Server) RemoveStack(ctx context.Context, req RemoveStackRequestObject) (RemoveStackResponseObject, error) {
	sess := sessionFrom(ctx)
	by := deploy.Starter{Issuer: sess.Identity.Issuer, Subject: sess.Identity.Subject, Email: sess.Email}
	op, err := s.deploy.Remove(ctx, by, req.Name, req.Body.DeleteData, req.Body.ConfirmName)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return RemoveStack422JSONResponse{Problems: refused.Problems}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		return RemoveStack404JSONResponse{NotFoundJSONResponse(notFound(req.Name))}, nil
	case errors.Is(err, deploy.ErrBusy):
		return RemoveStack409JSONResponse(problems("busy", "", "An operation of this stack is still running.")), nil
	case errors.Is(err, deploy.ErrAlreadyRemoved):
		return RemoveStack409JSONResponse(problems("already_removed", "", "The stack is removed already; choose Delete data too to delete its data.")), nil
	case errors.Is(err, deploy.ErrTarget):
		return RemoveStack502JSONResponse(problems("deployer_error", "", "The deployer refused api's call; see api's log.")), nil
	case err != nil:
		return nil, err
	}
	return RemoveStack202JSONResponse(operation(op)), nil
}

func (s *Server) rendererProblem(ctx context.Context, err error) contract.ProblemList {
	s.log.WarnContext(ctx, "renderer failed", slog.Any("error", err))
	return problems("renderer_error", "", "The renderer did not answer as expected; try again.")
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
	return problems("not_found", "name", fmt.Sprintf("No stack is named %q.", name))
}

func problems(code, path, message string) contract.ProblemList {
	return contract.ProblemList{Problems: []contract.Problem{{Severity: contract.Error, Code: code, Path: path, Message: message}}}
}
