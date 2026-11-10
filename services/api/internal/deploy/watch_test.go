package deploy_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

func TestWatchSharesOnePollerAndPublishesChanges(t *testing.T) {
	t.Parallel()
	st := newMem()
	code := 137
	target := &fakeTarget{boot: "boot-1", polls: []deploytarget.OperationStatus{deploytarget.OperationStatusSucceeded}}
	hub := deploy.NewHub()
	ctx, cancel := context.WithCancel(t.Context())
	s := deploy.NewService(ctx, st, fakeRenderer{digest: "sha256:abc"}, target, hub, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { cancel(); s.Wait() })
	deploy.SetPollEvery(s, 5*time.Millisecond)
	deploy.SetWatchEvery(s, 20*time.Millisecond)
	if _, err := s.Apply(t.Context(), admin, "sample", sample, "sha256:abc", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool {
		d, _, _ := st.Stack(t.Context(), "sample")
		return d.State() == deploy.StateRunning
	})

	// nobody watches: no state read
	time.Sleep(60 * time.Millisecond)
	target.mu.Lock()
	reads := target.stateReads
	target.mu.Unlock()
	if reads != 0 {
		t.Fatalf("%d state reads while nobody watched", reads)
	}

	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	modules := func() []deploy.ModuleState {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-events:
				if ev.Kind == deploy.EventModules && ev.Stack == "sample" {
					return ev.Modules
				}
			case <-deadline:
				t.Fatal("no modules event")
			}
		}
	}

	other := deploy.Starter{Issuer: "https://idp", Subject: "user-2", Email: "b@example.com"}
	stopFirst := s.Watch("sample", admin)
	stopSecond := s.Watch("sample", other)
	first := modules()
	if len(first) != 2 || first[1].Instance != "web" || first[1].State != "running" {
		t.Fatalf("first modules %+v", first)
	}

	// a crash Docker restarted at once: a higher restart count and the last exit code
	target.mu.Lock()
	target.services = []deploytarget.ServiceState{{
		Instance: "web", Module: "whoami", State: deploytarget.ServiceStateStateRunning, RestartCount: 1, LastExitCode: &code,
	}}
	target.mu.Unlock()
	crashed := modules()
	if w := crashed[1]; w.RestartCount != 1 || w.LastExitCode == nil || *w.LastExitCode != 137 {
		t.Fatalf("after the crash %+v", w)
	}

	// the oldest watcher leaves: reads now call as the next one
	stopFirst()
	target.mu.Lock()
	target.stateCallers = nil
	target.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	target.mu.Lock()
	callers := target.stateCallers
	target.mu.Unlock()
	if len(callers) == 0 {
		t.Fatal("no state read after the oldest watcher left")
	}
	for _, c := range callers {
		if c != "user-2" {
			t.Errorf("a state read called as %q, want user-2", c)
		}
	}

	// an unchanged state publishes nothing
	for len(events) > 0 {
		<-events
	}
	time.Sleep(80 * time.Millisecond)
	for len(events) > 0 {
		if ev := <-events; ev.Kind == deploy.EventModules {
			t.Errorf("a modules event without a change: %+v", ev.Modules)
		}
	}

	// the last watcher leaves: polling stops
	stopSecond()
	stopSecond() // idempotent
	time.Sleep(40 * time.Millisecond)
	target.mu.Lock()
	reads = target.stateReads
	target.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.stateReads != reads {
		t.Errorf("%d state reads after the last watcher left", target.stateReads-reads)
	}
}
