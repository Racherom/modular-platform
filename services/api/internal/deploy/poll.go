package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/racherom/modular-platform/libs/target/deploytarget"
)

// startPolling polls op once a second until it ends, unless a poller already runs for it.
func (s *Service) startPolling(op RunningOperation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.polling[op.ID] {
		return
	}
	s.polling[op.ID] = true
	s.wg.Go(func() {
		defer func() {
			s.mu.Lock()
			delete(s.polling, op.ID)
			s.mu.Unlock()
		}()
		s.poll(op)
	})
}

// poll follows op at the deploy target and copies its step and outcome into the record. It
// marks op unknown on a new boot id, a 404 under the same one, 60 seconds of failed polls, or
// 20 minutes after it started.
func (s *Service) poll(op RunningOperation) {
	ctx := callerCtx(s.ctx, op.StartedBy)
	t := time.NewTicker(s.every)
	defer t.Stop()
	var failingSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := s.now()
		if now.Sub(op.StartedAt) >= runningFor {
			s.finish(op, StatusUnknown, op.Step, "timeout: the operation ran for 20 minutes")
			return
		}
		tctx, cancel := context.WithTimeout(ctx, targetTimeout)
		got, boot, err := s.target.Operation(tctx, op.DeployerOperationID)
		cancel()
		switch {
		case boot != "" && boot != op.DeployerBootID:
			s.finish(op, StatusUnknown, op.Step, "deployer_restarted: the deploy target restarted during the operation")
			return
		case errors.Is(err, ErrNotFound):
			s.finish(op, StatusUnknown, op.Step, "deployer_forgot: the deploy target no longer knows the operation")
			return
		case err != nil:
			if failingSince.IsZero() {
				failingSince = now
			}
			if now.Sub(failingSince) >= failingFor {
				s.finish(op, StatusUnknown, op.Step, fmt.Sprintf("deployer_unavailable: polls failed for 60 seconds: %v", err))
				return
			}
			continue
		}
		failingSince = time.Time{}
		step := Step(got.Step)
		if got.Status == deploytarget.OperationStatusRunning {
			if step != op.Step {
				op.Step = step
				if changed, err := s.store.ProgressOperation(ctx, op.ID, step); err != nil {
					s.log.WarnContext(ctx, "recording a step failed", slog.Any("error", err))
				} else if changed {
					_, _ = s.publishOperation(ctx, op.DeploymentName, op.ID)
				}
			}
			continue
		}
		status := StatusSucceeded
		if got.Status == deploytarget.OperationStatusFailed {
			status = StatusFailed
		}
		s.finish(op, status, step, got.Error)
		return
	}
}

// finish records op's final status and tells the streams.
func (s *Service) finish(op RunningOperation, status Status, step Step, errText string) {
	ctx := context.WithoutCancel(s.ctx)
	changed, err := s.store.FinishOperation(ctx, op.ID, status, step, errText)
	if err != nil {
		s.log.ErrorContext(ctx, "recording an outcome failed", slog.String("operation", op.ID.String()), slog.Any("error", err))
		return
	}
	if !changed {
		return
	}
	s.logEnd(ctx, op.DeploymentName, op.ID, status, step, op.StartedBy.Subject)
	_, _ = s.publishOperation(ctx, op.DeploymentName, op.ID)
}
