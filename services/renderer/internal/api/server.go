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

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/libs/target/zip"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/renderer/internal/config"
)

const (
	maxBody        = 1 << 20 // 1 MB
	requestTimeout = 10 * time.Second
	burst          = 10
	idleBucket     = 10 * time.Minute
	maxClients     = 10_000 // rate limit buckets held at most (spec 0003)
)

var _ StrictServerInterface = (*Server)(nil)

// Server implements the renderer contract over the built in catalog.
type Server struct {
	catalog   contract.Catalog
	templates contract.Templates
	managed   bool  // internal mode: /v1/render/managed answers
	reserved  []int // host ports of the platform's ingress, which no managed project may publish
}

// GetCatalog lists the catalog: its modules and capabilities, each sorted by name.
func (s *Server) GetCatalog(context.Context, GetCatalogRequestObject) (GetCatalogResponseObject, error) {
	return GetCatalog200JSONResponse(s.catalog), nil
}

// Check checks the graph and fills in its automatic bindings.
func (s *Server) Check(_ context.Context, req CheckRequestObject) (CheckResponseObject, error) {
	return Check200JSONResponse(check.Graph(*req.Body, s.catalog)), nil
}

// Render answers the zip of an ok check result, or the check result itself (422) when it has an error.
func (s *Server) Render(ctx context.Context, req RenderRequestObject) (RenderResponseObject, error) {
	result := check.Graph(*req.Body, s.catalog)
	if !result.Ok {
		return Render422JSONResponse(result), nil
	}
	// past the deadline the client already has its 503; stop between phases
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := render.Render(result, s.catalog, s.templates)
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

// RenderManaged answers the project the managed deployer applies, or the check result itself (422) when
// it has an error, the managed flavor refuses an instance (host_access or setup_required, spec
// 0013), or the rendered project fails the guard (host_access, spec 0006 AC-8). Public mode has
// no managed render: 404.
func (s *Server) RenderManaged(ctx context.Context, req RenderManagedRequestObject) (RenderManagedResponseObject, error) {
	if !s.managed {
		return RenderManaged404JSONResponse{Problems: []contract.Problem{{
			Severity: contract.Error, Code: "not_found", Message: "The managed render exists in internal mode only.",
		}}}, nil
	}
	result := check.Graph(req.Body.Graph, s.catalog)
	if !result.Ok {
		return RenderManaged422JSONResponse(result), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := render.Managed(result, s.catalog, s.templates, req.Body.Deployment.String())
	if refused, ok := errors.AsType[*render.RefusedError](err); ok {
		return RenderManaged422JSONResponse(render.Refuse(result, refused.Problems)), nil
	}
	if err != nil {
		return nil, err
	}
	project, err := render.LoadProject(ctx, fileData(m.Output, "docker-compose.yml"), fileData(m.Output, ".env"))
	if err != nil {
		return nil, err
	}
	if problems := render.Guard(project, s.reserved); len(problems) > 0 {
		return RenderManaged422JSONResponse(render.Refuse(result, problems)), nil
	}
	return RenderManaged200JSONResponse(renderedProject(result, m)), nil
}

// fileData is the content of the rendered file at path, nil when there is none.
func fileData(out render.Output, path string) []byte {
	for _, f := range out.Files {
		if f.Path == path {
			return f.Data
		}
	}
	return nil
}

// renderedProject maps a managed render onto the contract.
func renderedProject(result contract.CheckResult, m render.ManagedProject) contract.RenderedProject {
	out := contract.RenderedProject{
		Project:        m.Name,
		Check:          result,
		Images:         make([]contract.ModuleImage, 0, len(m.Images)),
		PublishedPorts: []contract.PublishedPort{},
		Secrets:        make([]contract.RenderedSecret, 0, len(m.Secrets)),
		Files:          make([]contract.RenderedFile, 0, len(m.Files)),
	}
	for _, i := range m.Images {
		out.Images = append(out.Images, contract.ModuleImage{Instance: i.Instance, Module: i.Module, Image: i.Image})
	}
	for _, s := range m.Secrets {
		out.Secrets = append(out.Secrets, contract.RenderedSecret{
			Path: s.Path, Kind: contract.RenderedSecretKind(s.Kind), Instance: s.Instance,
			Capability: s.Capability, Field: s.Field,
		})
	}
	for _, f := range m.Files {
		out.Files = append(out.Files, contract.RenderedFile{Path: f.Path, Mode: int(f.Mode), Data: f.Data})
	}
	return out
}

// Setup checks the built in catalog and wires the renderer for its mode; sdk.Run serves the
// handler it returns behind health, the access log and the binding token check. A catalog entry
// that breaks a rule stops the start with one log line per problem (spec 0002, AC-11). Public mode
// turns the token check off and limits each client; its sweeper stops with ctx. Internal mode
// keeps the check, so it refuses to start without a consumer token in MP_RENDERER_CLIENTS_DIR.
func Setup(catalogFS fs.FS) sdk.Setup[config.Config, config.Uses] {
	return func(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
		entries, problems := contract.LoadCatalog(catalogFS)
		if len(problems) > 0 {
			for _, p := range problems {
				m.Log.ErrorContext(ctx, p.String())
			}
			return nil, fmt.Errorf("the built in catalog breaks %d rules: %w", len(problems), sdk.ErrStart)
		}
		templates, err := contract.LoadTemplates(catalogFS, entries)
		if err != nil {
			return nil, fmt.Errorf("load the built in templates: %w", err)
		}
		mode, err := m.Config.RendererMode(ctx)
		if err != nil {
			return nil, err
		}
		var reserved []int
		if mode == config.RendererModeInternal {
			ingress, bound, err := m.Uses.Ingress(ctx)
			if err != nil {
				return nil, err
			}
			if !bound {
				// the line has the sdk's problem form, so it reads like a refused start (spec 0025)
				m.Log.ErrorContext(ctx, "config: uses.ingress (MP_PLATFORM_HTTP_PORT): missing: internal mode needs the ingress binding")
				return nil, fmt.Errorf("internal mode without the ingress binding: %w", sdk.ErrStart)
			}
			reserved = []int{ingress.HttpPort, ingress.HttpsPort}
		}
		if mode == config.RendererModePublic {
			rate, err := m.Config.RendererRate(ctx)
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
		m.Log.InfoContext(ctx, "renderer starting", slog.String("mode", string(mode)), slog.Int("catalog_modules", len(entries.Modules)), slog.Int("catalog_capabilities", len(entries.Capabilities)))
		return newHandler(&Server{catalog: entries, templates: templates, managed: mode == config.RendererModeInternal, reserved: reserved}, m.Log), nil
	}
}

// newHandler serves the renderer contract with a strict graph body and a request timeout.
func newHandler(s *Server, log *slog.Logger) http.Handler {
	strict := NewStrictHandlerWithOptions(s, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			httpkit.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The request could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "The renderer failed; try again.")
		},
	})
	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []MiddlewareFunc{strictGraphBody},
	})
	return jsonTimeout(http.TimeoutHandler(mux, requestTimeout, `{"problems":[{"severity":"error","code":"timeout","path":"","message":"The renderer took too long; try again."}]}`))
}

// strictGraphBody decodes a graph body with the strict decoder (spec 0002) before the generated
// code sees it: over 1 MB is 413; unknown keys, duplicate keys, another apiVersion or kind are
// 400. The generated handler then decodes a clean re-encoding of the same graph. The managed
// render's body wraps the graph in {graph, deployment}; its graph is decoded the same way.
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
		var body any
		if r.URL.Path == managedPath {
			body, data = decodeManaged(data)
			if data == nil {
				httpkit.WriteProblem(w, http.StatusBadRequest, contract.CodeInvalidGraph, "The body must be an object with exactly graph and deployment.")
				return
			}
		}
		g, problem := contract.DecodeGraph(data)
		if problem != nil {
			if problem.Path != "" && body != nil {
				problem.Path = "graph." + problem.Path
			}
			httpkit.WriteJSON(w, http.StatusBadRequest, contract.ProblemList{Problems: []contract.Problem{*problem}})
			return
		}
		if m, ok := body.(*managedBody); ok {
			body = map[string]any{"graph": g, "deployment": m.Deployment}
		} else {
			body = g
		}
		clean, err := json.Marshal(body)
		if err != nil {
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "The renderer failed; try again.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(clean))
		r.ContentLength = int64(len(clean))
		next.ServeHTTP(w, r)
	})
}

const managedPath = "/v1/render/managed"

type managedBody struct {
	Graph      json.RawMessage `json:"graph"`
	Deployment string          `json:"deployment"`
}

// decodeManaged splits a managed render body into its envelope and the raw graph; a nil graph
// means the envelope itself is wrong. The deployment's format is checked by the generated code.
func decodeManaged(data []byte) (*managedBody, []byte) {
	var m managedBody
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil || len(m.Graph) == 0 || m.Deployment == "" {
		return nil, nil
	}
	return &m, m.Graph
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
