package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
)

// retryAfter is how long api waits before it retries a failed rollback, once (AC-14).
const retryAfter = 30 * time.Second

// afterFailure starts what a failed operation calls for (spec 0006, "Rollback"): an apply that
// failed at up and is no rollback itself rolls back at once; a failed rollback is retried once.
// A failure before up, a remove and an unknown outcome start nothing.
func (s *Service) afterFailure(op RunningOperation) {
	if op.Action != ActionApply || op.Status != StatusFailed {
		return
	}
	if op.RollbackOf == nil {
		if op.Step == StepUp {
			s.rollback(op, op.Operation, op.ID)
		}
		return
	}
	s.retryLater(op)
}

// rollback applies the graph of the newest succeeded apply before failed, as an operation whose
// rollback_of is failed and whose starter is failed's, provided latest is still the stack's
// latest operation. The check result is checked again, only the digest compare is skipped, and no placed
// value is sent: the existing files are reused. When no succeeded apply came before, nothing
// happens.
func (s *Service) rollback(at RunningOperation, failed Operation, latest uuid.UUID) {
	ctx := callerCtx(s.ctx, failed.StartedBy)
	good, ok, err := s.store.LatestSucceededBefore(ctx, failed.StackID, failed.ID)
	if err != nil {
		s.log.ErrorContext(ctx, "finding the version to roll back to failed", slog.String("stack", at.StackName), slog.Any("error", err))
		return
	}
	if !ok || good.Action != ActionApply {
		return // no earlier version to return to
	}
	var g contract.Graph
	if err := json.Unmarshal(good.Graph, &g); err != nil {
		s.log.ErrorContext(ctx, "a stored graph does not decode", slog.String("operation", good.ID.String()), slog.Any("error", err))
		return
	}
	result, checkErr := s.renderer.Check(ctx, g)
	op, started, err := s.store.StartOperationIfLatest(ctx, Operation{
		StackID: failed.StackID, Action: ActionApply, Graph: good.Graph, Digest: result.Digest,
		RollbackOf: &failed.ID, StartedBy: failed.StartedBy,
	}, latest)
	if err != nil {
		s.log.ErrorContext(ctx, "starting a rollback failed", slog.String("stack", at.StackName), slog.Any("error", err))
		return
	}
	if !started {
		return // an apply or remove came first
	}
	run := RunningOperation{Operation: op, StackName: at.StackName, ComposeProject: at.ComposeProject}
	s.log.InfoContext(ctx, "rollback started", slog.String("stack", run.StackName), slog.String("operation", op.ID.String()),
		slog.String("rollback_of", failed.ID.String()), slog.String("caller", failed.StartedBy.Subject))
	_, _ = s.publishOperation(ctx, run.StackName, op.ID)
	if text := s.sendRollback(ctx, run, g, result, checkErr); text != "" {
		s.finish(run, StatusFailed, StepPrepare, text)
	}
}

// sendRollback hands a started rollback to the deploy target and polls it. It returns why it
// could not, as the operation's error text, or "" once polling runs.
func (s *Service) sendRollback(ctx context.Context, run RunningOperation, g contract.Graph, result contract.CheckResult, checkErr error) string {
	switch {
	case checkErr != nil:
		return failureText(checkErr)
	case !result.Ok:
		return problemsText(result.Problems)
	}
	rendered, err := s.renderer.RenderManaged(ctx, g, run.StackID)
	if err != nil {
		return failureText(err)
	}
	tctx, cancel := context.WithTimeout(ctx, targetTimeout)
	defer cancel()
	accepted, err := s.target.Apply(tctx, run.ComposeProject, applyRequest(run.StackID, rendered))
	if err != nil {
		return failureText(err)
	}
	if err := s.store.AcceptOperation(ctx, run.ID, accepted.Id, accepted.BootID); err != nil {
		// polling still knows the ids; only a restart of api loses them
		s.log.ErrorContext(ctx, "recording the deploy target's ids failed", slog.String("operation", run.ID.String()), slog.Any("error", err))
	}
	run.DeployerOperationID, run.DeployerBootID = accepted.Id, accepted.BootID
	s.startPolling(run)
	return ""
}

// retryLater starts a second rollback after retryAfter, unless two rollbacks of the same failed
// apply exist already, or another operation became the latest meanwhile. An api restart drops
// the timer; the stack then stays failed.
func (s *Service) retryLater(failedRollback RunningOperation) {
	n, err := s.store.CountRollbacks(s.ctx, *failedRollback.RollbackOf)
	if err != nil {
		s.log.ErrorContext(s.ctx, "counting rollbacks failed", slog.String("stack", failedRollback.StackName), slog.Any("error", err))
		return
	}
	if n >= 2 {
		return
	}
	s.wg.Go(func() {
		t := time.NewTimer(s.retryAfter)
		defer t.Stop()
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		failed, ok, err := s.store.Operation(s.ctx, *failedRollback.RollbackOf)
		if err != nil || !ok {
			s.log.ErrorContext(s.ctx, "reading the failed apply to retry its rollback failed", slog.Bool("found", ok), slog.Any("error", err))
			return
		}
		s.rollback(failedRollback, failed, failedRollback.ID)
	})
}

// failureText is the error a rollback that could not start records: a problem code first, as
// the deploy target's errors start with one. It never carries a renderer or target body.
func failureText(err error) string {
	if r, ok := errors.AsType[*RefusedError](err); ok {
		return problemsText(r.Problems)
	}
	switch {
	case errors.Is(err, ErrTargetUnavailable):
		return "deployer_unavailable"
	case errors.Is(err, ErrBusy):
		return "busy: the deploy target still runs an operation of this stack"
	case errors.Is(err, ErrRenderer):
		return "renderer_error"
	case errors.Is(err, ErrTarget):
		return "deployer_error"
	}
	return "internal: " + err.Error()
}

// problemsText is the codes of problems on one line, then one line per problem.
func problemsText(problems []contract.Problem) string {
	codes := make([]string, 0, len(problems))
	var lines strings.Builder
	for _, p := range problems {
		codes = append(codes, p.Code)
		fmt.Fprintf(&lines, "\n%s: %s", p.Path, p.Message)
	}
	return strings.Join(codes, ", ") + lines.String()
}
