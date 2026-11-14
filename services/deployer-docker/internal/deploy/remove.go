package deploy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// removeTimeout bounds docker compose down (child 0007, "Remove").
const removeTimeout = 5 * time.Minute

const codeDownFailed = "down_failed"

// ErrNotFound reports a project with neither a folder nor a container.
var ErrNotFound = errors.New("deploy: the project has no folder and no container")

// RemoveRequest is a remove of a project. Caller is the MP-Caller subject, for the log.
type RemoveRequest struct {
	Project    string
	DeleteData bool
	Caller     string
}

// Remove starts a remove of req's project, which runs on after Remove returns. It returns a
// *RefusedError for a bad project name, ErrNotFound when the project has neither a folder nor a
// container, ErrDocker when Docker does not answer, and ErrBusy while an operation of the
// project runs; nothing happened then.
func (e *Engine) Remove(ctx context.Context, req RemoveRequest) (Operation, error) {
	if !ValidProject(req.Project) {
		return Operation{}, &RefusedError{Problems: []Problem{{
			Code: CodeInvalidProject, Path: "project", Message: fmt.Sprintf("The project %q must be mp- followed by a stack name.", req.Project),
		}}}
	}
	present, _, err := e.State(ctx, req.Project)
	if err != nil {
		return Operation{}, err
	}
	if !present {
		return Operation{}, ErrNotFound
	}
	op, err := e.start(req.Project, ActionRemove)
	if err != nil {
		return Operation{}, err
	}
	snapshot := *op
	e.log.InfoContext(e.ctx, "operation started", slog.String("project", req.Project), slog.String("operation", op.ID),
		slog.String("action", string(ActionRemove)), slog.Bool("delete_data", req.DeleteData), slog.String("caller", req.Caller))
	e.wg.Go(func() {
		err := e.remove(op, req)
		done := e.finish(op, err)
		e.log.InfoContext(e.ctx, "operation finished", slog.String("project", done.Project), slog.String("operation", done.ID),
			slog.String("action", string(done.Action)), slog.String("step", string(done.Step)),
			slog.String("status", string(done.Status)), slog.String("caller", req.Caller))
	})
	return snapshot, nil
}

// remove runs down, with --volumes when data goes too, deletes the image lock files and the
// project's keep tags (spec 0031), then the secrets folder when data goes. The rest of the
// folder stays: it shows what last ran.
func (e *Engine) remove(op *Operation, req RemoveRequest) error {
	dir := e.projectDir(req.Project)
	e.step(op, StepDown)
	args := []string{"down", "--remove-orphans"}
	if req.DeleteData {
		args = append(args, "--volumes")
	}
	ctx, cancel := context.WithTimeout(e.ctx, removeTimeout)
	defer cancel()
	out, err := e.compose.Run(ctx, dir, req.Project, args...)
	if err != nil {
		return composeError(codeDownFailed, out, err)
	}
	if err := e.removeLocks(dir, req.Project); err != nil {
		return fmt.Errorf("%s\n%w", codeDownFailed, err)
	}
	if !req.DeleteData {
		return nil
	}
	if err := removeSecrets(dir); err != nil {
		return fmt.Errorf("%s\n%w", codeDownFailed, err)
	}
	return nil
}

// removeSecrets deletes the project's secrets folder. A link in its place is refused, so the
// delete never leaves the project folder; RemoveAll itself never follows a link below it.
func removeSecrets(dir string) error {
	secrets := filepath.Join(dir, "secrets")
	info, err := os.Lstat(secrets)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("secrets: %w", errSymlink)
	}
	return os.RemoveAll(secrets)
}
