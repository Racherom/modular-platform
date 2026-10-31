package sdk

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Readiness polls each required binding with backoff from readyMin to readyMax until it answers
// once; it is not checked again after that (spec 0005, "Middleware order and readiness").
const (
	readyMin     = time.Second
	readyMax     = 30 * time.Second
	readyAttempt = 5 * time.Second // the longest one poll may take
)

// pinger is a binding client readiness can poll.
type pinger interface {
	Ready(ctx context.Context) error
}

// readiness counts the required bindings that have not answered yet.
type readiness struct {
	pending atomic.Int64
}

func (r *readiness) ready() bool { return r.pending.Load() == 0 }

// wait polls p in the background until it answers once or ctx ends. Each failed attempt logs one
// warning naming the capability and the error, never a credential.
func (r *readiness) wait(ctx context.Context, log *slog.Logger, capability string, p pinger) {
	r.pending.Add(1)
	go func() {
		backoff := readyMin
		for {
			attempt, cancel := context.WithTimeout(ctx, readyAttempt)
			err := p.Ready(attempt)
			cancel()
			if err == nil {
				r.pending.Add(-1)
				return
			}
			if ctx.Err() != nil {
				return
			}
			log.WarnContext(ctx, "binding not ready", slog.String("capability", capability), slog.Any("error", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, readyMax)
		}
	}()
}
