package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/libs/target/zip"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

const (
	maxBody        = 1 << 20 // 1 MB
	requestTimeout = 10 * time.Second
	burst          = 10
	idleBucket     = 10 * time.Minute
	maxClients     = 10_000 // rate limit buckets held at most (spec 0003)
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
	// past the deadline the client already has its 503; stop between phases
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := render.Render(plan, s.catalog)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
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

// Setup checks the built in catalog and wires the planner for its mode; sdk.Run serves the
// handler it returns behind health, the access log and the binding token check. A catalog entry
// that breaks a rule stops the start with one log line per problem (spec 0002, AC-11). Public mode
// turns the token check off and limits each client; its sweeper stops with ctx. Internal mode
// keeps the check, so it refuses to start without a consumer token in MP_PLANNER_CLIENTS_DIR.
func Setup(catalogFS fs.FS) sdk.Setup[config.Config, config.Uses] {
	return func(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
		entries, problems := contract.LoadCatalog(catalogFS)
		if len(problems) > 0 {
			for _, p := range problems {
				m.Log.ErrorContext(ctx, p.String())
			}
			return nil, fmt.Errorf("the built in catalog breaks %d rules: %w", len(problems), sdk.ErrStart)
		}
		mode, err := m.Config.PlannerMode(ctx)
		if err != nil {
			return nil, err
		}
		if mode == config.PlannerModePublic {
			rate, err := m.Config.PlannerRate(ctx)
			if err != nil {
				return nil, err
			}
			hops, err := m.Config.ProxyHops(ctx)
			if err != nil {
				return nil, err
			}
			m.AllowAnonymous()
			limiter := httpkit.NewRateLimiter(rate, burst, idleBucket, maxClients)
			go limiter.SweepEvery(ctx, time.Minute, m.Log)
			m.WithRateLimit(limiter, hops)
		}
		m.Log.InfoContext(ctx, "planner starting", slog.String("mode", string(mode)), slog.Int("catalog_entries", len(entries)))
		return newHandler(entries, m.Log), nil
	}
}

// newHandler serves the planner contract with a strict graph body and a request timeout.
func newHandler(catalog []contract.CatalogEntry, log *slog.Logger) http.Handler {
	strict := NewStrictHandlerWithOptions(&Server{catalog: catalog}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			httpkit.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The request could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "The planner failed; try again.")
		},
	})
	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []MiddlewareFunc{strictGraphBody},
	})
	return jsonTimeout(http.TimeoutHandler(mux, requestTimeout, `{"problems":[{"severity":"error","code":"timeout","path":"","message":"The planner took too long; try again."}]}`))
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
				httpkit.WriteProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The graph is over 1 MB.")
				return
			}
			httpkit.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The request body could not be read.")
			return
		}
		g, problem := contract.DecodeGraph(data)
		if problem != nil {
			httpkit.WriteJSON(w, http.StatusBadRequest, contract.ProblemList{Problems: []contract.Problem{*problem}})
			return
		}
		clean, err := json.Marshal(g)
		if err != nil {
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "The planner failed; try again.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(clean))
		r.ContentLength = int64(len(clean))
		next.ServeHTTP(w, r)
	})
}

// jsonTimeout marks the 503 that http.TimeoutHandler writes as JSON; it sets no Content-Type itself.
func jsonTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(timeoutWriter{w}, r)
	})
}

type timeoutWriter struct{ http.ResponseWriter }

func (w timeoutWriter) WriteHeader(status int) {
	if status == http.StatusServiceUnavailable && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.ResponseWriter.WriteHeader(status)
}
