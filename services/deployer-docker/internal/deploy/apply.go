package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// Problem codes of a refused request.
const (
	CodeInvalidProject = render.CodeInvalidProject
	CodeHostAccess     = render.CodeHostAccess
	CodeInvalidPath    = "invalid_path"
	CodePlacedMissing  = "placed_missing"
	CodePlacedInvalid  = "placed_invalid"
)

// Error codes at the start of a failed operation's error.
const (
	codePrepareFailed = "prepare_failed"
	codePullFailed    = "pull_failed"
	codeUpFailed      = "up_failed"
)

const (
	maxPlaced = 4096      // bytes in one placed value
	maxOutput = 16 * 1024 // bytes of Compose output kept in an error
)

var (
	projectPattern = regexp.MustCompile(`^mp-[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	secretPattern  = regexp.MustCompile(`^secrets/[a-z0-9-]+/[A-Za-z0-9._-]+$`)
	// configPattern is an sdk module's env file config/<instance>.env, or a template output
	// config/<instance>/<file> (spec 0013, Templates), never a dot name like ..
	configPattern   = regexp.MustCompile(`^config/[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.env|/[A-Za-z0-9_-][A-Za-z0-9._-]*)$`)
	projectFiles    = map[string]bool{"docker-compose.yml": true, ".env": true, "graph.yaml": true, "README.md": true, "setup.sh": true}
	errNotSecretDir = errors.New("not a secret path")
)

// Problem is one reason a request was refused. Path names the offending item, like
// files.docker-compose.yml.
type Problem struct {
	Code, Path, Message string
}

// RefusedError lists why a request was refused; nothing happened.
type RefusedError struct {
	Problems []Problem
}

func (e *RefusedError) Error() string {
	codes := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		codes = append(codes, p.Code)
	}
	return "deploy: refused: " + strings.Join(codes, ", ")
}

// ValidProject reports whether project is a managed project name, mp-<deployment name>.
func ValidProject(project string) bool { return projectPattern.MatchString(project) }

// Apply checks req and starts its operation, which runs on after Apply returns. A refused
// request returns a *RefusedError, a project with a running operation ErrBusy; nothing
// happened then.
func (e *Engine) Apply(req ApplyRequest) (Operation, error) {
	if problems := e.check(req); len(problems) > 0 {
		return Operation{}, &RefusedError{Problems: problems}
	}
	op, err := e.start(req.Project, ActionApply)
	if err != nil {
		return Operation{}, err
	}
	snapshot := *op
	e.log.InfoContext(e.ctx, "operation started", slog.String("project", req.Project), slog.String("operation", op.ID),
		slog.String("action", string(ActionApply)), slog.String("caller", req.Caller))
	e.wg.Go(func() {
		err := e.apply(op, req)
		done := e.finish(op, err)
		e.log.InfoContext(e.ctx, "operation finished", slog.String("project", done.Project), slog.String("operation", done.ID),
			slog.String("action", string(done.Action)), slog.String("step", string(done.Step)),
			slog.String("status", string(done.Status)), slog.String("caller", req.Caller))
	})
	return snapshot, nil
}

// check runs every check that needs no lock: the project name, each path, each placed value,
// the guard and the labels on the loaded Compose file, and that every placed secret exists or
// is supplied.
func (e *Engine) check(req ApplyRequest) []Problem {
	var problems []Problem
	refuse := func(code, path, format string, args ...any) {
		problems = append(problems, Problem{Code: code, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if !ValidProject(req.Project) {
		refuse(CodeInvalidProject, "project", "The project %q must be mp- followed by a deployment name.", req.Project)
		return problems
	}
	for _, f := range req.Files {
		if !projectFiles[f.Path] && !configPattern.MatchString(f.Path) {
			refuse(CodeInvalidPath, "files."+f.Path, "%q is not a file a rendered project holds.", f.Path)
		}
	}
	placed := map[string]bool{}
	for _, s := range req.Secrets {
		if !secretPattern.MatchString(s.Path) {
			refuse(CodeInvalidPath, "secrets."+s.Path, "%q is not a secret path secrets/<instance>/<name>.", s.Path)
		}
		placed[s.Path] = s.Kind == SecretPlaced
	}
	supplied := map[string]bool{}
	for _, p := range req.Placed {
		switch {
		case !secretPattern.MatchString(p.Path):
			refuse(CodeInvalidPath, "placed."+p.Path, "%q is not a secret path secrets/<instance>/<name>.", p.Path)
		case !placed[p.Path]:
			// a generated secret is never overwritten, so no value may name one
			refuse(CodeInvalidPath, "placed."+p.Path, "%s is not a credential this project places.", p.Path)
		case len(p.Data) == 0:
			refuse(CodePlacedInvalid, "placed."+p.Path, "The value for %s is empty.", p.Path)
		case len(p.Data) > maxPlaced:
			refuse(CodePlacedInvalid, "placed."+p.Path, "The value for %s is over %d bytes.", p.Path, maxPlaced)
		case bytes.IndexByte(p.Data, 0) >= 0:
			refuse(CodePlacedInvalid, "placed."+p.Path, "The value for %s holds a NUL byte.", p.Path)
		default:
			supplied[p.Path] = true
		}
	}
	if len(problems) > 0 {
		return problems
	}
	if problems := e.guard(req); len(problems) > 0 {
		return problems
	}
	dir := e.projectDir(req.Project)
	for _, s := range req.Secrets {
		if s.Kind != SecretPlaced || supplied[s.Path] {
			continue
		}
		exists, err := fileExists(dir, s.Path)
		switch {
		case err != nil:
			refuse(CodeInvalidPath, "secrets."+s.Path, "%s cannot be checked: %v.", s.Path, err)
		case !exists:
			refuse(CodePlacedMissing, "secrets."+s.Path, "%s needs a value: it does not exist yet.", s.Path)
		}
	}
	return problems
}

// guard loads docker-compose.yml with .env and deploy.env as docker compose will, then runs
// render.Guard and render.CheckProject on it: this service cannot trust its caller, so a
// graph that bypassed the checks must not reach the host (spec 0006, AC-8).
func (e *Engine) guard(req ApplyRequest) []Problem {
	var compose, env []byte
	for _, f := range req.Files {
		switch f.Path {
		case "docker-compose.yml":
			compose = f.Data
		case ".env":
			env = f.Data
		}
	}
	if compose == nil {
		return []Problem{{Code: CodeInvalidProject, Path: "files", Message: "The project has no docker-compose.yml."}}
	}
	project, err := render.LoadProject(e.ctx, compose, env, e.deployEnv())
	if err != nil {
		return []Problem{{Code: CodeInvalidProject, Path: "files.docker-compose.yml", Message: err.Error()}}
	}
	var problems []Problem
	for _, p := range append(render.Guard(project, e.cfg.ReservedPorts), render.CheckProject(project, req.Project, req.Deployment)...) {
		problems = append(problems, fromContract(p))
	}
	return problems
}

func fromContract(p contract.Problem) Problem {
	return Problem{Code: p.Code, Path: p.Path, Message: p.Message}
}

// deployEnv is the deploy.env the engine writes beside .env: the user deployed sdk modules run as.
func (e *Engine) deployEnv() []byte {
	return fmt.Appendf(nil, "# Written by the deployer: the user deployed sdk modules run as.\nMP_UID=%d\nMP_GID=%d\n", e.cfg.UID, e.cfg.GID)
}

func (e *Engine) projectDir(project string) string { return filepath.Join(e.cfg.Dir, project) }

// apply runs prepare, pull and up. A failure in prepare or pull puts the folder back as it
// was, so the running containers and their files stay untouched (child 0007, "Apply pipeline").
func (e *Engine) apply(op *Operation, req ApplyRequest) error {
	dir := e.projectDir(req.Project)
	before, err := e.snapshot(dir, req)
	if err != nil {
		return fmt.Errorf("%s\n%w", codePrepareFailed, err)
	}
	created, err := e.prepare(dir, req)
	if err != nil {
		return e.restore(dir, before, created, fmt.Errorf("%s\n%w", codePrepareFailed, err))
	}

	e.step(op, StepPull)
	ctx, cancel := context.WithTimeout(e.ctx, e.cfg.PullTimeout)
	out, err := e.compose.Run(ctx, dir, req.Project, "pull")
	cancel()
	if err != nil {
		return e.restore(dir, before, created, composeError(codePullFailed, out, err))
	}

	e.step(op, StepUp)
	// a replaced value is written only now, so a failure before up never loses the old one
	for _, p := range req.Placed {
		if p.Replace {
			if err := e.writeSecret(dir, p.Path, p.Data, true); err != nil {
				return fmt.Errorf("%s\n%w", codeUpFailed, err)
			}
		}
	}
	// --wait-timeout ends the wait; the context is a backstop a minute later
	ctx, cancel = context.WithTimeout(e.ctx, e.cfg.WaitTimeout+time.Minute)
	defer cancel()
	out, err = e.compose.Run(ctx, dir, req.Project, "up", "--detach", "--remove-orphans",
		"--wait", "--wait-timeout", strconv.Itoa(int(e.cfg.WaitTimeout.Seconds())))
	if err != nil {
		return composeError(codeUpFailed, out, err)
	}
	return nil
}

// prepare writes the project's files, deploy.env, every missing generated secret and every
// supplied placed value that is missing. It returns the secrets it created, also on failure.
func (e *Engine) prepare(dir string, req ApplyRequest) (created []string, err error) {
	if err := e.ensureDir(e.cfg.Dir, dir, 0o755); err != nil {
		return nil, err
	}
	for _, f := range req.Files {
		if err := e.writeFile(dir, f.Path, f.Data, 0o644, false); err != nil {
			return nil, err
		}
	}
	if err := e.writeFile(dir, "deploy.env", e.deployEnv(), 0o644, false); err != nil {
		return nil, err
	}
	if err := e.dataFolders(dir, req); err != nil {
		return nil, err
	}
	create := func(path string, data []byte) error {
		err := e.writeSecret(dir, path, data, false)
		if err == nil {
			created = append(created, path)
		}
		if errors.Is(err, errExists) {
			return nil
		}
		return err
	}
	for _, s := range req.Secrets {
		if s.Kind != SecretGenerated {
			continue
		}
		if err := create(s.Path, []byte(newSecret())); err != nil {
			return created, err
		}
	}
	for _, p := range req.Placed {
		if p.Replace {
			continue
		}
		if err := create(p.Path, p.Data); err != nil {
			return created, err
		}
	}
	return created, nil
}

// dataFolders makes every data/<instance>/<volume> folder the project binds, mode 0700 and owned
// by the platform user, so a module that runs as that user can write its data; Docker would make
// a missing one as root.
func (e *Engine) dataFolders(dir string, req ApplyRequest) error {
	var compose, env []byte
	for _, f := range req.Files {
		switch f.Path {
		case "docker-compose.yml":
			compose = f.Data
		case ".env":
			env = f.Data
		}
	}
	project, err := render.LoadProject(e.ctx, compose, env, e.deployEnv())
	if err != nil {
		return err
	}
	for _, folder := range render.DataFolders(project) {
		parent := dir
		for part := range strings.SplitSeq(folder, "/") {
			next := filepath.Join(parent, part)
			if err := e.ensureDir(parent, next, 0o700); err != nil {
				return err
			}
			parent = next
		}
	}
	return nil
}

// snapshot reads the current content of every file prepare will write; nil marks a file that
// does not exist yet.
func (e *Engine) snapshot(dir string, req ApplyRequest) (map[string][]byte, error) {
	out := make(map[string][]byte, len(req.Files)+1)
	paths := []string{"deploy.env"}
	for _, f := range req.Files {
		paths = append(paths, f.Path)
	}
	for _, rel := range paths {
		data, err := readFile(dir, rel)
		if err != nil {
			return nil, err
		}
		out[rel] = data
	}
	return out, nil
}

// restore puts back the files of before, deletes the ones that did not exist, and deletes the
// secrets this operation created, never an existing one. It returns cause, with any restore
// failure joined to it.
func (e *Engine) restore(dir string, before map[string][]byte, created []string, cause error) error {
	var errs []error
	for rel, data := range before {
		if data == nil {
			errs = append(errs, removeFile(dir, rel))
			continue
		}
		errs = append(errs, e.writeFile(dir, rel, data, 0o644, false))
	}
	for _, rel := range created {
		errs = append(errs, removeFile(dir, rel))
	}
	if err := errors.Join(errs...); err != nil {
		e.log.WarnContext(e.ctx, "restoring the previous files failed", slog.String("dir", dir), slog.Any("error", err))
		return fmt.Errorf("%w\nrestoring the previous files failed: %w", cause, err)
	}
	return cause
}

// composeError is code, the command's error, and at most the last 16 KB of its output.
func composeError(code string, out []byte, err error) error {
	if len(out) > maxOutput {
		out = out[len(out)-maxOutput:]
	}
	return fmt.Errorf("%s: %w\n%s", code, err, bytes.TrimSpace(out))
}
