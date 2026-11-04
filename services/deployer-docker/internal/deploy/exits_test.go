package deploy_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

func newStream() fakeStream {
	return fakeStream{since: make(chan time.Time, 1), events: make(chan deploy.ExitEvent), errs: make(chan error, 1)}
}

// watch runs WatchExits until the test's cleanup, which waits for it to end.
func watch(t *testing.T, e *deploy.Engine) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		e.WatchExits(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// lastExits reads the state and maps each instance to its last exit code, -1 for none.
func lastExits(t *testing.T, e *deploy.Engine) map[string]int {
	t.Helper()
	_, services, err := e.State(t.Context(), "mp-sample")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, s := range services {
		out[s.Instance] = -1
		if s.LastExitCode != nil {
			out[s.Instance] = *s.LastExitCode
		}
	}
	return out
}

// eventually polls cond until it holds, or fails after 5 seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func container(id, status string, exitCode int) deploy.Container {
	return deploy.Container{
		ID: id, Status: status, ExitCode: exitCode,
		Labels: map[string]string{"io.github.racherom.modular-platform.instance": id},
	}
}

func TestLastExitCode(t *testing.T) {
	t.Parallel()
	streams := make(chan fakeStream, 1)
	d := fakeDocker{streams: streams, containers: []deploy.Container{
		container("crashed", "running", 0),         // Docker restarted it: its own code is 0 again
		container("restarting", "restarting", 137), // the inspect still holds the code: it wins
		container("exited", "exited", 1),           // the inspect wins over an older die
		container("created", "created", 0),         // never ran: no last exit code
		container("quiet", "running", 0),           // no die seen: absent, never 0
		container("garbled", "running", 0),         // its die carries no usable code
	}}
	e, _, logs := newEngine(t, &fakeCompose{}, d)
	s := newStream()
	streams <- s
	watch(t, e)

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	for _, ev := range []deploy.ExitEvent{
		{ID: "crashed", Action: deploy.EventDie, ExitCode: "137", Time: at},
		{ID: "crashed", Action: deploy.EventDie, ExitCode: "143", Time: at.Add(-time.Second)}, // older: ignored
		{ID: "crashed", Action: deploy.EventDie, ExitCode: "2", Time: at},                     // replayed time: ignored
		{ID: "restarting", Action: deploy.EventDie, ExitCode: "9", Time: at},
		{ID: "exited", Action: deploy.EventDie, ExitCode: "9", Time: at},
		{ID: "created", Action: deploy.EventDie, ExitCode: "3", Time: at},
		{ID: "garbled", Action: deploy.EventDie, ExitCode: "", Time: at},
		{ID: "garbled", Action: deploy.EventDie, ExitCode: "oom", Time: at},
	} {
		s.events <- ev
	}
	// an unbuffered send returns once WatchExits holds the event; this one proves the last is stored
	s.events <- deploy.ExitEvent{ID: "sentinel", Action: deploy.EventDestroy, Time: at}

	want := map[string]int{"crashed": 137, "restarting": 137, "exited": 1, "created": -1, "quiet": -1, "garbled": -1}
	eventually(t, "the die events are recorded", func() bool { return lastExits(t, e)["crashed"] == 137 })
	got := lastExits(t, e)
	for id, code := range want {
		if got[id] != code {
			t.Errorf("%s: last exit %d, want %d", id, got[id], code)
		}
	}

	// a destroy forgets the container
	s.events <- deploy.ExitEvent{ID: "crashed", Action: deploy.EventDestroy, Time: at.Add(time.Second)}
	s.events <- deploy.ExitEvent{ID: "sentinel", Action: deploy.EventDestroy, Time: at.Add(time.Second)}
	eventually(t, "the destroy is applied", func() bool { return lastExits(t, e)["crashed"] == -1 })

	// the warnings name the container only
	s.errs <- io.EOF
	s2 := newStream()
	streams <- s2
	<-s2.since
	if got := strings.Count(logs.String(), "docker die event without an exit code"); got != 2 {
		t.Errorf("%d warnings for the garbled events, want 2:\n%s", got, logs)
	}
}

func TestWatchExitsReopensFromTheReplayPoint(t *testing.T) {
	t.Parallel()
	streams := make(chan fakeStream, 1)
	e, _, logs := newEngine(t, &fakeCompose{}, fakeDocker{streams: streams})
	opened := time.Now()
	first := newStream()
	streams <- first
	watch(t, e)

	// the first stream replays the hour before the deployer started
	since := <-first.since
	if hour := opened.Add(-time.Hour); since.After(hour) || since.Before(hour.Add(-time.Minute)) {
		t.Errorf("first since %v, want about %v", since, hour)
	}
	at := time.Now().Add(-time.Minute).UTC()
	first.events <- deploy.ExitEvent{ID: "a", Action: deploy.EventDie, ExitCode: "1", Time: at}

	// a dropped stream opens again after a second, from the last event received
	first.errs <- io.ErrUnexpectedEOF
	second := newStream()
	streams <- second
	if got := <-second.since; !got.Equal(at) {
		t.Errorf("second since %v, want the last event's time %v", got, at)
	}
	second.events <- deploy.ExitEvent{ID: "b", Action: deploy.EventDie, ExitCode: "2", Time: at.Add(time.Second)}

	// a stream that fails again opens from the newer event; that it opened proves the event landed
	second.errs <- io.ErrUnexpectedEOF
	third := newStream()
	streams <- third
	if got := <-third.since; !got.Equal(at.Add(time.Second)) {
		t.Errorf("third since %v, want %v", got, at.Add(time.Second))
	}
	out := logs.String()
	if strings.Count(out, "docker events lost") != 2 || strings.Count(out, "docker events resumed") != 1 {
		t.Errorf("want two lost lines and one resumed line:\n%s", out)
	}
}
