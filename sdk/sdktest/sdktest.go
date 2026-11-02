// Package sdktest holds fakes of the sdk's pluggable sources and helpers that write env files and
// clients directories, for tests of modules on the sdk (spec 0005, "Pluggable sources").
package sdktest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
)

// EnvFile writes lines (each KEY=value, a comment or blank) as an env file and returns its path.
func EnvFile(t testing.TB, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "module.env")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ClientsDir writes one token file per consumer instance into a new directory and returns its
// path.
func ClientsDir(t testing.TB, tokens map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, token := range tokens {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Source is a start source that answers from Answers. A name in Errs fails with that error; a
// name in Slow answers only after that long, ignoring ctx, as a hung source would.
type Source struct {
	Answers map[string]string
	Errs    map[string]error
	Slow    map[string]time.Duration

	mu    sync.Mutex
	asked []string
}

var _ sdk.Source = (*Source)(nil)

// Lookup answers value from Answers.
func (s *Source) Lookup(_ context.Context, value string) (string, bool, error) {
	s.mu.Lock()
	s.asked = append(s.asked, value)
	s.mu.Unlock()
	if d := s.Slow[value]; d > 0 {
		time.Sleep(d)
	}
	if err := s.Errs[value]; err != nil {
		return "", false, err
	}
	text, ok := s.Answers[value]
	return text, ok, nil
}

// Asked returns the value names asked so far, in order.
func (s *Source) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// Push is a push source a test drives: Send hands one snapshot to the module.
type Push struct {
	sends    chan pushSend
	started  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	declared []string
}

type pushSend struct {
	snapshot map[string]string
	result   chan error
}

var _ sdk.PushSource = (*Push)(nil)

// NewPush returns a push source that sends nothing until Send.
func NewPush() *Push {
	return &Push{sends: make(chan pushSend), started: make(chan struct{})}
}

// Watch applies each snapshot Send hands it until ctx ends.
func (p *Push) Watch(ctx context.Context, values []string, apply func(map[string]string) error) error {
	p.mu.Lock()
	p.declared = append([]string(nil), values...)
	p.mu.Unlock()
	p.once.Do(func() { close(p.started) })
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s := <-p.sends:
			s.result <- apply(s.snapshot)
		}
	}
}

// Send waits until the module watches, hands it snapshot and returns what apply returned.
func (p *Push) Send(ctx context.Context, snapshot map[string]string) error {
	select {
	case <-p.started:
	case <-ctx.Done():
		return ctx.Err()
	}
	s := pushSend{snapshot: snapshot, result: make(chan error, 1)}
	select {
	case p.sends <- s:
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-s.result
}

// Values returns the value names the module asked Watch for.
func (p *Push) Values() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.declared...)
}

// CallerSource answers values per user from Answers (user to value name to text). With Err set it
// fails; with Delay set it answers only after that long, ignoring ctx.
type CallerSource struct {
	Answers map[string]map[string]string
	Err     error
	Delay   time.Duration

	calls atomic.Int64
}

var _ sdk.CallerSource = (*CallerSource)(nil)

// LookupFor answers value for c.User.
func (s *CallerSource) LookupFor(_ context.Context, c sdk.Caller, value string) (string, bool, error) {
	s.calls.Add(1)
	if s.Delay > 0 {
		time.Sleep(s.Delay)
	}
	if s.Err != nil {
		return "", false, s.Err
	}
	text, ok := s.Answers[c.User][value]
	return text, ok, nil
}

// Calls returns how often LookupFor ran.
func (s *CallerSource) Calls() int { return int(s.calls.Load()) }

// Resolver picks bindings per user from Bindings (user to capability to binding). With Err set it
// fails; with Delay set it answers only after that long, ignoring ctx.
type Resolver struct {
	Bindings map[string]map[string]sdk.Binding
	Err      error
	Delay    time.Duration

	calls atomic.Int64
}

var _ sdk.BindingResolver = (*Resolver)(nil)

// BindingFor picks the binding of capability for c.User.
func (r *Resolver) BindingFor(_ context.Context, c sdk.Caller, capability string) (sdk.Binding, bool, error) {
	r.calls.Add(1)
	if r.Delay > 0 {
		time.Sleep(r.Delay)
	}
	if r.Err != nil {
		return sdk.Binding{}, false, r.Err
	}
	b, ok := r.Bindings[c.User][capability]
	return b, ok, nil
}

// Calls returns how often BindingFor ran.
func (r *Resolver) Calls() int { return int(r.calls.Load()) }
