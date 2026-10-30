package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/platform"
	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/libs/target/zip"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

const (
	maxBody        = 1 << 20 // 1 MB
	requestTimeout = 10 * time.Second
	burst          = 10
	idleBucket     = 10 * time.Minute
)

var _ StrictServerInterface = (*Server)(nil)

// Server implements the planner contract over the built in catalog.
type Server struct {
	catalog []contract.CatalogEntry
}

// GetCatalog lists the catalog, sorted by name.
func (s *Server) GetCatalog(context.Context, GetCatalogRequestObject) (GetCatalogResponseObject, error) {
	return GetCatalog200JSONResponse{Entries: s.catalog}, nil
}

// Plan checks the graph and fills in its automatic bindings.
func (s *Server) Plan(_ context.Context, req PlanRequestObject) (PlanResponseObject, error) {
	return Plan200JSONResponse(planner.Plan(*req.Body, s.catalog)), nil
}

// Render answers the zip of an ok plan, or the plan itself (422) when it has an error.
func (s *Server) Render(ctx context.Context, req RenderRequestObject) (RenderResponseObject, error) {
	plan := planner.Plan(*req.Body, s.catalog)
	if !plan.Ok {
		return Render422JSONResponse(plan), nil
	}
	out, err := render.Render(plan, s.catalog)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := zip.New(&buf).Apply(ctx, out); err != nil {
		return nil, err
	}
	return Render200ApplicationzipResponse{
		Body:          &buf,
		ContentLength: int64(buf.Len()),
		Headers:       Render200ResponseHeaders{ContentDisposition: fmt.Sprintf(`attachment; filename="%s.zip"`, out.Name)},
	}, nil
}

// NewHandler wires the planner for its mode. In public mode it starts the rate limiter's sweeper,
// which stops with ctx.
func NewHandler(ctx context.Context, cfg config.Config, catalog []contract.CatalogEntry, log *slog.Logger) http.Handler {
	strict := NewStrictHandlerWithOptions(&Server{catalog: catalog}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			platform.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The request could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
			platform.WriteProblem(w, http.StatusInternalServerError, "internal", "The planner failed; try again.")
		},
	})
	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []MiddlewareFunc{strictGraphBody},
	})
	platform.Health(mux, func() bool { return true })

	h := http.TimeoutHandler(mux, requestTimeout, `{"problems":[{"severity":"error","code":"timeout","path":"","message":"The planner took too long; try again."}]}`)
	switch cfg.Mode {
	case config.ModePublic:
		limiter := platform.NewRateLimiter(cfg.Rate, burst, idleBucket)
		go limiter.SweepEvery(ctx, time.Minute)
		h = limiter.Middleware(cfg.ProxyHops, platform.HealthPaths, h)
	case config.ModeInternal:
		h = platform.BearerAuth(cfg.Token, platform.HealthPaths, h)
	}
	return platform.AccessLog(log, h)
}

// strictGraphBody decodes a graph body with the strict decoder (spec 0002) before the generated
// code sees it: over 1 MB is 413; unknown keys, duplicate keys, another apiVersion or kind are
// 400. The generated handler then decodes a clean re-encoding of the same graph.
func strictGraphBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				platform.WriteProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The graph is over 1 MB.")
				return
			}
			platform.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The request body could not be read.")
			return
		}
		g, problem := contract.DecodeGraph(data)
		if problem != nil {
			platform.WriteJSON(w, http.StatusBadRequest, contract.ProblemList{Problems: []contract.Problem{*problem}})
			return
		}
		clean, err := json.Marshal(g)
		if err != nil {
			platform.WriteProblem(w, http.StatusInternalServerError, "internal", "The planner failed; try again.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(clean))
		r.ContentLength = int64(len(clean))
		next.ServeHTTP(w, r)
	})
}
