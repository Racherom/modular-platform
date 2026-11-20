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

// ApplyStack starts an apply. It needs an MP-Caller token, whose subject is logged.
func (s *Server) ApplyStack(ctx context.Context, req ApplyStackRequestObject) (ApplyStackResponseObject, error) {
	who, ok := sdk.CallerFrom(ctx)
	if !ok {
		return ApplyStack401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Apply needs an MP-Caller token.")}}, nil
	}
	body := req.Body
	in := deploy.ApplyRequest{Project: req.Stack, Stack: body.Stack.String(), Caller: who.User}
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
		return ApplyStack422JSONResponse{Body: refusal(refused)}, nil
	}
	if errors.Is(err, deploy.ErrBusy) {
		return ApplyStack409JSONResponse{Body: problems("busy", "", "An operation of this project is still running.")}, nil
	}
	if err != nil {
		return nil, err
	}
	return ApplyStack202JSONResponse{Body: operation(op)}, nil
}

// RemoveStack starts a remove. It needs an MP-Caller token, whose subject is logged.
func (s *Server) RemoveStack(ctx context.Context, req RemoveStackRequestObject) (RemoveStackResponseObject, error) {
	who, ok := sdk.CallerFrom(ctx)
	if !ok {
		return RemoveStack401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Remove needs an MP-Caller token.")}}, nil
	}
	op, err := s.engine.Remove(ctx, deploy.RemoveRequest{Project: req.Stack, DeleteData: req.Body.DeleteData, Caller: who.User})
	if _, ok := errors.AsType[*deploy.RefusedError](err); ok {
		return RemoveStack400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Stack)}}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		return RemoveStack404JSONResponse{Body: problems("not_found", "project", "The project has neither a folder nor a container.")}, nil
	case errors.Is(err, deploy.ErrBusy):
		return RemoveStack409JSONResponse{Body: problems("busy", "", "An operation of this project is still running.")}, nil
	case errors.Is(err, deploy.ErrDocker):
		return RemoveStack503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, nil
	case err != nil:
		return nil, err
	}
	return RemoveStack202JSONResponse{Body: operation(op)}, nil
}

// GetOperation reads an operation this boot knows.
func (s *Server) GetOperation(_ context.Context, req GetOperationRequestObject) (GetOperationResponseObject, error) {
	op, ok := s.engine.Operation(req.Id)
	if !ok {
		return GetOperation404JSONResponse{NotFoundJSONResponse{Body: problems("not_found", "", "No operation with this id since the deployer started.")}}, nil
	}
	return GetOperation200JSONResponse{Body: operation(op)}, nil
}

// GetStackState reads each container of the project.
func (s *Server) GetStackState(ctx context.Context, req GetStackStateRequestObject) (GetStackStateResponseObject, error) {
	if !deploy.ValidProject(req.Stack) {
		return GetStackState400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Stack)}}, nil
	}
	present, services, err := s.engine.State(ctx, req.Stack)
	if errors.Is(err, deploy.ErrDocker) {
		return GetStackState503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, err
	}
	if err != nil {
		return nil, err
	}
	out := deploytarget.StackState{Present: present, Services: make([]deploytarget.ServiceState, 0, len(services))}
	for _, svc := range services {
		out.Services = append(out.Services, deploytarget.ServiceState{
			Instance: svc.Instance, Module: svc.Module, Image: svc.Image,
			State: deploytarget.ServiceStateState(svc.State), Health: svc.Health, ExitCode: svc.ExitCode,
			LastExitCode: svc.LastExitCode, RestartCount: svc.RestartCount, Since: svc.Since,
		})
	}
	return GetStackState200JSONResponse{Body: out}, nil
}

// GetStackImages reads the project's frozen image versions (spec 0031). It needs an MP-Caller
// token like apply.
func (s *Server) GetStackImages(ctx context.Context, req GetStackImagesRequestObject) (GetStackImagesResponseObject, error) {
	if _, ok := sdk.CallerFrom(ctx); !ok {
		return GetStackImages401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Reading the images needs an MP-Caller token.")}}, nil
	}
	if !deploy.ValidProject(req.Stack) {
		return GetStackImages400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Stack)}}, nil
	}
	images, err := s.engine.Images(ctx, req.Stack)
	if invalid, ok := errors.AsType[*deploy.LockInvalidError](err); ok {
		return GetStackImages500JSONResponse{Body: problems("lock_invalid", invalid.File, fmt.Sprintf("The lock file %s cannot be read: %v.", invalid.File, invalid.Err))}, nil
	}
	switch {
	case errors.Is(err, deploy.ErrBusy):
		return GetStackImages409JSONResponse{Body: problems("busy", "", "An operation of this project is still running.")}, nil
	case errors.Is(err, deploy.ErrDocker):
		return GetStackImages503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, err
	case err != nil:
		return nil, err
	}
	return GetStackImages200JSONResponse{Body: deploytarget.StackImages{
		Adopted: images.Adopted, Current: lockView(images.Current), Previous: lockView(images.Previous),
	}}, nil
}

func lockView(v *deploy.LockView) *deploytarget.ImageLock {
	if v == nil {
		return nil
	}
	out := &deploytarget.ImageLock{
		FormatVersion: v.Lock.FormatVersion, Stack: v.Lock.Stack, Operation: v.Lock.Operation,
		WrittenAt: v.Lock.WrittenAt, Images: make([]deploytarget.LockedImage, 0, len(v.Lock.Images)),
	}
	for n, img := range v.Lock.Images {
		out.Images = append(out.Images, deploytarget.LockedImage{
			Reference: img.Reference, Source: deploytarget.LockedImageSource(img.Source), Digest: img.Digest,
			ImageId: img.ImageID, Present: v.Present[n],
		})
	}
	return out
}

// ResolveImages resolves image references to their registry digests without pulling (spec
// 0031). It needs an MP-Caller token like apply.
func (s *Server) ResolveImages(ctx context.Context, req ResolveImagesRequestObject) (ResolveImagesResponseObject, error) {
	if _, ok := sdk.CallerFrom(ctx); !ok {
		return ResolveImages401JSONResponse{UnauthorizedJSONResponse{Body: problems("invalid_token", "", "Resolve needs an MP-Caller token.")}}, nil
	}
	refs := req.Body.References
	var bad contract.ProblemList
	if len(refs) == 0 || len(refs) > 200 {
		bad.Problems = append(bad.Problems, contract.Problem{Severity: contract.Error, Code: "invalid_request", Path: "references", Message: "Send 1 to 200 references."})
	}
	for n, ref := range refs {
		if err := contract.CheckLockReference(ref); err != nil {
			bad.Problems = append(bad.Problems, contract.Problem{
				Severity: contract.Error, Code: "invalid_reference", Path: fmt.Sprintf("references[%d]", n),
				Message: err.Error() + ".",
			})
		}
	}
	if len(bad.Problems) > 0 {
		return ResolveImages400JSONResponse{Body: bad}, nil
	}
	res, err := s.engine.Resolve(ctx, refs)
	if errors.Is(err, deploy.ErrDocker) {
		return ResolveImages503JSONResponse{Body: problems("docker_unavailable", "", "Docker did not answer.")}, err
	}
	if err != nil {
		return nil, err
	}
	out := deploytarget.ResolveResult{
		Images:   make([]deploytarget.ResolvedImage, 0, len(res.Images)),
		Problems: make([]contract.Problem, 0, len(res.Unresolved)),
	}
	for _, img := range res.Images {
		out.Images = append(out.Images, deploytarget.ResolvedImage{Reference: img.Reference, Digest: img.Digest})
	}
	for _, u := range res.Unresolved {
		out.Problems = append(out.Problems, contract.Problem{
			Severity: contract.Error, Code: "image_unresolved", Path: fmt.Sprintf("references[%d]", u.Index),
			Message: fmt.Sprintf("%s: %s.", u.Reference, u.Reason),
		})
	}
	return ResolveImages200JSONResponse{Body: out}, nil
}

// GetStackSecrets lists the project's secret files that exist.
func (s *Server) GetStackSecrets(_ context.Context, req GetStackSecretsRequestObject) (GetStackSecretsResponseObject, error) {
	if !deploy.ValidProject(req.Stack) {
		return GetStackSecrets400JSONResponse{BadRequestJSONResponse{Body: badProject(req.Stack)}}, nil
	}
	present, err := s.engine.Secrets(req.Stack)
	if err != nil {
		return nil, err
	}
	return GetStackSecrets200JSONResponse{Body: deploytarget.PresentSecrets{Present: present}}, nil
}

func operation(op deploy.Operation) deploytarget.Operation {
	return deploytarget.Operation{
		Id: op.ID, Stack: op.Project, Action: deploytarget.OperationAction(op.Action), Status: deploytarget.OperationStatus(op.Status),
		Step: deploytarget.OperationStep(op.Step), Error: op.Error, StartedAt: op.StartedAt, FinishedAt: op.FinishedAt,
	}
}

func problems(code, path, message string) contract.ProblemList {
	return contract.ProblemList{Problems: []contract.Problem{{Severity: contract.Error, Code: code, Path: path, Message: message}}}
}

func badProject(project string) contract.ProblemList {
	return problems(deploy.CodeInvalidProject, "project", fmt.Sprintf("The project %q must be mp- followed by a stack name.", project))
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
	if cfg.Dir, err = m.Config.StacksDir(ctx); err != nil {
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
	if cfg.ResolveTimeout, err = m.Config.ResolveTimeout(ctx); err != nil {
		return nil, err
	}
	ingress, err := m.Uses.Ingress(ctx)
	if err != nil {
		return nil, err
	}
	cfg.ReservedPorts = []int{ingress.HttpPort, ingress.HttpsPort}
	engine, err := docker.New()
	if err != nil {
		return nil, err
	}
	e := deploy.New(ctx, cfg, docker.Compose{}, engine, m.Log)
	// before the server listens; shutdown ends ctx, and with it the stream and its reconnects
	go e.WatchExits(ctx)
	m.Log.InfoContext(ctx, "deployer starting", slog.String("stacks_dir", cfg.Dir))
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
