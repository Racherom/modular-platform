package setup

import (
	"context"
	"log/slog"
	"time"
)

const (
	firstRetry = time.Second
	maxRetry   = 5 * time.Minute
)

// Bound tells the service an admin was just bound: the keeper's done call follows in Run.
func (s *Service) Bound() {
	s.settled.Store(true)
	select {
	case s.bound <- struct{}{}:
	default: // a call is already queued
	}
}

// Run calls the keeper's done until it answers, after a bind and at start when an admin is
// bound and a code file still exists (a done call that never landed). It returns when ctx ends.
func (s *Service) Run(ctx context.Context) {
	if pending, err := s.Pending(ctx); err == nil && !pending {
		if _, found, err := s.files.Code(ctx); err == nil && found {
			s.finish(ctx)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.bound:
			s.finish(ctx)
		}
	}
}

// finish retries the done call, 1 second doubling up to 5 minutes, until it answers or ctx ends.
func (s *Service) finish(ctx context.Context) {
	retry := firstRetry
	for attempt := 1; ; attempt++ {
		err := s.keeper.Done(ctx)
		if err == nil {
			s.log.InfoContext(ctx, "setup done", slog.Int("attempt", attempt))
			return
		}
		s.log.ErrorContext(ctx, "setup done failed", slog.Int("attempt", attempt), slog.Duration("retryIn", retry), slog.Any("error", err))
		if s.wait(ctx, retry) != nil {
			return
		}
		retry = min(retry*2, maxRetry)
	}
}
