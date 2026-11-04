package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"
)

// watchEvery is how often a watched deployment's module state is read while no operation of it
// runs; while one runs it is read at the poll interval (spec 0006, Build plan task 17).
const watchEvery = 5 * time.Second

// watcher is one open screen watching a deployment, as whom its state reads call.
type watcher struct {
	id uint64
	by Starter
}

// watch is the one state poller of a deployment, shared by every screen that watches it.
type watch struct {
	watchers []watcher // oldest first
	stop     context.CancelFunc
}

// Watch starts following the live module state of the deployment named name for an open screen,
// and returns the func that ends it. Every screen watching a deployment shares one poller, which
// publishes a modules event on each change. Its reads carry the MP-Caller of the oldest watcher.
// Nothing polls a deployment nobody watches.
func (s *Service) Watch(name string, by Starter) (stop func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextWatcher++
	id := s.nextWatcher
	w, ok := s.watches[name]
	if !ok {
		ctx, cancel := context.WithCancel(s.ctx)
		w = &watch{stop: cancel}
		s.watches[name] = w
		s.wg.Go(func() { s.pollModules(ctx, name) })
	}
	w.watchers = append(w.watchers, watcher{id: id, by: by})
	return func() { s.unwatch(name, id) }
}

func (s *Service) unwatch(name string, id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.watches[name]
	if !ok {
		return
	}
	w.watchers = slices.DeleteFunc(w.watchers, func(x watcher) bool { return x.id == id })
	if len(w.watchers) == 0 {
		w.stop()
		delete(s.watches, name)
	}
}

// oldestWatcher is whom a watched deployment's state reads call as; false once nobody watches.
func (s *Service) oldestWatcher(name string) (Starter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.watches[name]
	if !ok || len(w.watchers) == 0 {
		return Starter{}, false
	}
	return w.watchers[0].by, true
}

// pollModules reads the deployment's module state until ctx ends: every second while an
// operation runs, else every five, and publishes it when it changed. A read that fails is
// tried again at the next tick; the screen keeps what it last showed.
func (s *Service) pollModules(ctx context.Context, name string) {
	var last []byte
	t := time.NewTimer(s.watchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		next := s.watchEvery
		by, ok := s.oldestWatcher(name)
		if !ok {
			return
		}
		v, err := s.View(callerCtx(ctx, by), name)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			s.log.WarnContext(ctx, "reading a watched deployment failed", slog.String("deployment", name), slog.Any("error", err))
		case v.StateError == "":
			if v.Latest != nil && v.Latest.Status == StatusRunning {
				next = s.every
			}
			// plain structs always encode
			current, _ := json.Marshal(v.Modules) //nolint:errchkjson // plain structs
			if !bytes.Equal(current, last) {
				last = current
				s.hub.Publish(Event{Kind: EventModules, Deployment: name, Modules: v.Modules})
			}
		}
		t.Reset(next)
	}
}
