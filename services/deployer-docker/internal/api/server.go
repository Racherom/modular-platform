package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/config"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/docker"
)

const (
	maxBody = 8 << 20 // 8 MB
	// BootHeader carries the boot id on every answer.
	BootHeader = "MP-Boot-Id"
)

var _ StrictServerInterface = (*Server)(nil)

// Server implements the deploy target contract over a deploy engine.
type Server struct {
	engine *deploy.Engine
}

// NewServer serves engine.
func NewServer(engine *deploy.Engine) *Server { return &Server{engine: engine} }

// ApplyProject starts an apply. It needs an MP-Caller token, whose subject is logged.
func (s *Server) ApplyProject(ctx context.Context, req ApplyProjectRequestObject) (ApplyProjectResponseObject, error) {
	who, ok := sdk.CallerFrom(ctx)
	if !ok {
		return ApplyProject401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Apply needs an MP-Caller token.")}}, nil
	}
	body := req.Body
	in := deploy.ApplyRequest{Project: req.Project, Deployment: body.Deployment.String(), Caller: who.User}
	for _, f := range body.Files {
		in.Files = append(in.Files, deploy.File{Path: f.Path, Data: f.Data})
	}
	for _, sec := range body.Secrets {
		in.Secrets = append(in.Secrets, deploy.Secret{Path: sec.Path, Kind: deploy.SecretKind(sec.Kind)})
	}
	for _, p := range body.Placed {
		in.Placed = append(in.Placed, deploy.Placed{Path: p.Path, Data: p.Data, Replace: p.Replace})
	}
	op, err := s.engine.Apply(in)
	if refused, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return ApplyProject422JSONResponse{Body: refusal(refused)}, nil
	}
	if errors.Is(err, deploy.ErrBusy) {
		return ApplyProject409JSONResponse{Body: problems("busy", "", "An operation of this project is still running.")}, nil
	}
	if err != nil {
		return nil, err
	}
	return ApplyProject202JSONResponse{Body: operation(op)}, nil
}

// RemoveProject starts a remove. It needs an MP-Caller token, whose subject is logged.
func (s *Server) RemoveProject(ctx context.Context, req RemoveProjectRequestObject) (RemoveProjectResponseObject, error) {
	who, ok := sdk.CallerFrom(ctx)
	if !ok {
		return RemoveProject401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Remove needs an MP-Caller token.")}}, nil
	}
	op, err := s.engine.Remove(ctx, deploy.RemoveRequest{Project: req.Project, DeleteData: req.Body.DeleteData, Caller: who.User})
	if _, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return RemoveProject400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Project)}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		return RemoveProject404JSONResponse{Body: problems("not_found", "project", "The project has neither a folder nor a container.")}, nil
	case errors.Is(err, deploy.ErrBusy):
		return RemoveProject409JSONResponse{Body: problems("busy", "", "An operation of this project is still running.")}, nil
	case errors.Is(err, deploy.ErrDocker):
		return RemoveProject503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, nil
	case err != nil:
		return nil, err
	}
	return RemoveProject202JSONResponse{Body: operation(op)}, nil
}

// GetOperation reads an operation this boot knows.
func (s *Server) GetOperation(_ context.Context, req GetOperationRequestObject) (GetOperationResponseObject, error) {
	op, ok := s.engine.Operation(req.Id)
	if !ok {
		return GetOperation404JSONResponse{NotFoundJSONResponse{Body: problems("not_found", "", "No operation with this id since the deployer started.")}}, nil
	}
	return GetOperation200JSONResponse{Body: operation(op)}, nil
}

// GetProjectState reads each container of the project.
func (s *Server) GetProjectState(ctx context.Context, req GetProjectStateRequestObject) (GetProjectStateResponseObject, error) {
	if !deploy.ValidProject(req.Project) {
		return GetProjectState400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Project)}}, nil
	}
	present, services, err := s.engine.State(ctx, req.Project)
	if errors.Is(err, deploy.ErrDocker) {
		return GetProjectState503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, err
	}
	if err != nil {
		return nil, err
	}
	out := deploytarget.ProjectState{Present: present, Services: make([]deploytarget.ServiceState, 0, len(services))}
	for _, svc := range services {
		out.Services = append(out.Services, deploytarget.ServiceState{
			Instance: svc.Instance, Module: svc.Module, Image: svc.Image,
			State: deploytarget.ServiceStateState(svc.State), Health: svc.Health, ExitCode: svc.ExitCode,
			RestartCount: svc.RestartCount, Since: svc.Since,
		})
	}
	return GetProjectState200JSONResponse{Body: out}, nil
}

// GetProjectSecrets lists the project's secret files that exist.
func (s *Server) GetProjectSecrets(_ context.Context, req GetProjectSecretsRequestObject) (GetProjectSecretsResponseObject, error) {
	if !deploy.ValidProject(req.Project) {
		return GetProjectSecrets400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Project)}}, nil
	}
	present, err := s.engine.Secrets(req.Project)
	if err != nil {
		return nil, err
	}
	return GetProjectSecrets200JSONResponse{Body: deploytarget.PresentSecrets{Present: present}}, nil
}

func operation(op deploy.Operation) deploytarget.Operation {
	return deploytarget.Operation{
		Id: op.ID, Project: op.Project, Action: deploytarget.OperationAction(op.Action), Status: deploytarget.OperationStatus(op.Status),
		Step: deploytarget.OperationStep(op.Step), Error: op.Error, StartedAt: op.StartedAt, FinishedAt: op.FinishedAt,
	}
}

func problems(code, path, message string) contract.ProblemList {
	return contract.ProblemList{Problems: []contract.Problem{{Severity: contract.Error, Code: code, Path: path, Message: message}}}
}

func badProject(project string) contract.ProblemList {
	return problems(deploy.CodeInvalidProject, "project", fmt.Sprintf("The project %q must be mp- followed by a deployment name.", project))
}

func refusal(err *deploy.RefusedError) contract.ProblemList {
	out := contract.ProblemList{Problems: make([]contract.Problem, 0, len(err.Problems))}
	for _, p := range err.Problems {
		out.Problems = append(out.Problems, contract.Problem{Severity: contract.Error, Code: p.Code, Path: p.Path, Message: p.Message})
	}
	return out
}

// Setup reads the config, connects to Docker and serves the contract; sdk.Run puts the binding
// token and MP-Caller checks in front.
func Setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	var cfg deploy.Config
	var err error
	if cfg.Dir, err = m.Config.DeploymentsDir(ctx); err != nil {
		return nil, err
	}
	if cfg.UID, err = m.Config.PlatformUid(ctx); err != nil {
		return nil, err
	}
	if cfg.GID, err = m.Config.PlatformGid(ctx); err != nil {
		return nil, err
	}
	if cfg.PullTimeout, err = m.Config.PullTimeout(ctx); err != nil {
		return nil, err
	}
	if cfg.WaitTimeout, err = m.Config.WaitTimeout(ctx); err != nil {
		return nil, err
	}
	for _, port := range []func(context.Context) (int, error){m.Config.PlatformHttpPort, m.Config.PlatformHttpsPort} {
		p, err := port(ctx)
		if err != nil {
			return nil, err
		}
		cfg.ReservedPorts = append(cfg.ReservedPorts, p)
	}
	engine, err := docker.New()
	if err != nil {
		return nil, err
	}
	e := deploy.New(ctx, cfg, docker.Compose{}, engine, m.Log)
	m.Log.InfoContext(ctx, "deployer starting", slog.String("deployments_dir", cfg.Dir))
	return NewHandler(e, m.Log), nil
}

// NewHandler serves the contract over e, stamping the boot id on every answer.
func NewHandler(e *deploy.Engine, log *slog.Logger) http.Handler {
	strict := NewStrictHandlerWithOptions(NewServer(e), nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				httpkit.WriteProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The body is over 8 MB.")
				return
			}
			httpkit.WriteProblem(w, http.StatusBadRequest, "invalid_request", "The request could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "The deployer failed; try again.")
		},
	})
	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		mux.ServeHTTP(&bootWriter{ResponseWriter: w, id: e.BootID()}, r)
	})
}

// bootWriter sets the boot id just before the status line goes out, over whatever the
// generated code put there.
type bootWriter struct {
	http.ResponseWriter
	id    string
	wrote bool
}

func (w *bootWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		w.Header().Set(BootHeader, w.id)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *bootWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *bootWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
