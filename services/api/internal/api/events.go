package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

// Timing and limits of the event stream (spec 0006, "API surface").
const (
	pingEvery     = 15 * time.Second
	sessionEvery  = 30 * time.Second
	writeDeadline = 30 * time.Second
	maxStreams    = 8 // per session
	snapshotPage  = 200
)

// events serves GET /events: a snapshot, then live updates. It is no activity for the
// session's idle limit; it checks the session every 30 seconds and ends with signed-out.
type events struct {
	signin *signin.Service
	deploy *deploy.Service
	hub    *deploy.Hub
	log    *slog.Logger

	mu         sync.Mutex
	perSession map[string]int
}

func (e *events) acquire(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.perSession[key] >= maxStreams {
		return false
	}
	e.perSession[key]++
	return true
}

func (e *events) release(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.perSession[key]--; e.perSession[key] <= 0 {
		delete(e.perSession, key)
	}
}

func (e *events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(ctx)
	key := hex.EncodeToString(sess.TokenHash)
	if !e.acquire(key) {
		httpkit.WriteProblem(w, http.StatusTooManyRequests, "too_many_streams", "This session has 8 open event streams already.")
		return
	}
	defer e.release(key)
	filter := r.URL.Query().Get("stack")

	// subscribe before the snapshot, so nothing between the two is lost
	updates, unsubscribe := e.hub.Subscribe()
	defer unsubscribe()
	snapshot, err := e.snapshot(r, filter)
	if errors.Is(err, deploy.ErrNotFound) {
		httpkit.WriteJSON(w, http.StatusNotFound, notFound(filter))
		return
	}
	if err != nil {
		e.log.ErrorContext(ctx, "event snapshot failed", slog.Any("error", err))
		httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "api failed; try again.")
		return
	}

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(name string, data any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(writeDeadline))
		var err error
		if name == "" {
			_, err = fmt.Fprint(w, ": ping\n\n")
		} else {
			payload, merr := json.Marshal(data)
			if merr != nil {
				e.log.ErrorContext(ctx, "event encoding failed", slog.Any("error", merr))
				return false
			}
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload)
		}
		return err == nil && rc.Flush() == nil
	}
	if !send("snapshot", snapshot) {
		return
	}

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	check := time.NewTicker(sessionEvery)
	defer check.Stop()
	if filter != "" {
		// one poller per stack, shared by every stream watching it (task 17)
		by := deploy.Starter{Issuer: sess.Identity.Issuer, Subject: sess.Identity.Subject, Email: sess.Email}
		defer e.deploy.Watch(filter, by)()
	}
	for {
		var ok bool
		select {
		case <-ctx.Done():
			return
		case ev, open := <-updates:
			if !open {
				return // fell behind; the reconnect gets a new snapshot
			}
			ok = filter != "" && ev.Stack != filter || e.sendEvent(send, ev, filter != "")
		case <-ping.C:
			ok = send("", nil)
		case <-check.C:
			reason, err := e.signin.StreamCheck(ctx, sess.TokenHash)
			if err == nil && reason != "" {
				send("signed-out", EventSignedOut{Reason: EventSignedOutReason(reason)})
				return
			}
			ok = true
		}
		if !ok {
			return
		}
	}
}

// snapshot is the first event: every stack without the filter, the full stack with
// it.
func (e *events) snapshot(r *http.Request, filter string) (any, error) {
	if filter == "" {
		items, err := e.deploy.Stacks(r.Context(), "", snapshotPage)
		if err != nil {
			return nil, err
		}
		list := EventSnapshotList{Stacks: make([]StackSummary, 0, len(items))}
		for _, d := range items {
			list.Stacks = append(list.Stacks, summary(d))
		}
		return list, nil
	}
	v, err := e.deploy.View(r.Context(), filter)
	if err != nil {
		return nil, err
	}
	return view(v), nil
}

func (e *events) sendEvent(send func(string, any) bool, ev deploy.Event, filtered bool) bool {
	switch {
	case ev.Kind == deploy.EventOperation && ev.Operation != nil:
		return send("operation", EventOperation{Stack: ev.Stack, Operation: operation(*ev.Operation)})
	case ev.Kind == deploy.EventStack && ev.Summary != nil:
		return send("stack", summary(*ev.Summary))
	case ev.Kind == deploy.EventModules:
		// only a stream with the filter gets modules; the caller skipped other stacks
		if !filtered {
			return true
		}
		return send("modules", EventModules{Stack: ev.Stack, Modules: moduleStates(ev.Modules)})
	}
	return true
}
