package deploy

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Limits and timing of the exit record (child 0007, "Last exit code").
const (
	maxExits     = 4096
	replayWindow = time.Hour
	reopenFirst  = time.Second
	reopenMax    = 30 * time.Second
)

// The container events the exit record follows.
const (
	EventDie     = "die"
	EventDestroy = "destroy"
)

// ExitEvent is a container's die or destroy event, as the engine reports it. ExitCode is the
// event's raw exitCode attribute; a die whose code is missing or not an integer is ignored.
type ExitEvent struct {
	ID       string
	Action   string // EventDie or EventDestroy
	ExitCode string
	Time     time.Time
}

// exit is the latest exit seen for one container.
type exit struct {
	code int
	at   time.Time
}

// exits keeps the code of each deployed container's most recent exit, from Docker's die events,
// because Docker sets a container's exit code back to 0 when it starts it again. It lives in
// memory only: a restarted deployer starts empty.
type exits struct {
	mu   sync.Mutex
	byID map[string]exit
}

func newExits() *exits { return &exits{byID: map[string]exit{}} }

// record applies one event: a die stores its code unless the stored one is as new or newer, so a
// replayed event changes nothing; a destroy forgets the container. It reports false for a die
// without a usable code.
func (x *exits) record(ev ExitEvent) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if ev.Action == EventDestroy {
		delete(x.byID, ev.ID)
		return true
	}
	code, err := strconv.Atoi(ev.ExitCode)
	if err != nil {
		return false
	}
	if old, ok := x.byID[ev.ID]; ok && !ev.Time.After(old.at) {
		return true
	}
	x.byID[ev.ID] = exit{code: code, at: ev.Time}
	if len(x.byID) > maxExits {
		x.dropOldest()
	}
	return true
}

// dropOldest forgets the entry with the oldest exit. The caller holds mu.
func (x *exits) dropOldest() {
	var oldest string
	var at time.Time
	for id, e := range x.byID {
		if oldest == "" || e.at.Before(at) {
			oldest, at = id, e.at
		}
	}
	delete(x.byID, oldest)
}

// last returns the code of the container's most recent exit seen, if any.
func (x *exits) last(id string) (int, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	e, ok := x.byID[id]
	return e.code, ok
}

// WatchExits follows Docker's die and destroy events of deployed containers until ctx ends, so
// a module's last exit code still shows after Docker restarted it. A dropped stream is opened
// again after 1 second, doubling up to 30, from the replay point: the later of the last event
// received and one hour before the deployer started. The state read never waits for it.
func (e *Engine) WatchExits(ctx context.Context) {
	since := e.started.Add(-replayWindow)
	wait := reopenFirst
	lost := false
	for {
		// each stream gets its own context, so the one a failure left behind ends with it
		sctx, stop := context.WithCancel(ctx)
		events, errs := e.docker.Events(sctx, since)
		received := false
	read:
		for {
			select {
			case <-ctx.Done():
				stop()
				return
			case ev, ok := <-events:
				if !ok {
					break read
				}
				if !received && lost {
					e.log.InfoContext(ctx, "docker events resumed")
					lost = false
				}
				received = true
				wait = reopenFirst
				if ev.Time.After(since) {
					since = ev.Time
				}
				if !e.exits.record(ev) {
					e.log.WarnContext(ctx, "docker die event without an exit code", slog.String("container", ev.ID))
				}
			case err := <-errs:
				if ctx.Err() != nil {
					stop()
					return
				}
				if !lost {
					e.log.WarnContext(ctx, "docker events lost", slog.Any("error", err))
					lost = true
				}
				break read
			}
		}
		stop()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, reopenMax)
	}
}
