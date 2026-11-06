package sdk

import (
	"context"
	"errors"
	"time"
)

// Source answers at start, by value name (not env name), above every other level (spec 0005,
// "Sources and precedence"). Text in; the SDK parses and checks it. Not found, or an empty
// answer, falls through to the env, the env files and the default.
type Source interface {
	Lookup(ctx context.Context, value string) (string, bool, error)
}

// PushSource delivers whole snapshots while the module runs (#17's config server). Watch calls
// apply with each snapshot, keyed by value name; apply returns an error wrapping
// ErrSnapshotRefused when the snapshot has a problem, and the previous snapshot stays.
type PushSource interface {
	Watch(ctx context.Context, values []string, apply func(snapshot map[string]string) error) error
}

// CallerSource answers a value for one caller (#21's policy server). Not found means the
// snapshot value.
type CallerSource interface {
	LookupFor(ctx context.Context, caller Caller, value string) (string, bool, error)
}

// BindingResolver picks the provider binding for one caller and slot (#21). Not found means the
// module's own binding. For a slot with no client the answer is Binding.Fields, each field in the
// Go form its getter returns (string, int, bool, *url.URL, a string for an enum, Secret, []byte
// PEM for a certificate); the sdk checks it by the start rules (spec 0025).
type BindingResolver interface {
	BindingFor(ctx context.Context, caller Caller, capability string) (Binding, bool, error)
}

var (
	// ErrCallerSource reports a caller source or binding resolver that failed or missed its 200 ms
	// budget; the module answers 503.
	ErrCallerSource = errors.New("sdk: the caller source cannot answer")
	// ErrSnapshotRefused reports a pushed snapshot with a problem; the problem lines are logged.
	ErrSnapshotRefused = errors.New("sdk: the snapshot is refused")
)

// The source limits of spec 0005, "Pluggable sources".
const (
	sourceDeadline = 10 * time.Second // all start source calls together
	pushWait       = 30 * time.Second // for the first accepted snapshot
	callerBudget   = 200 * time.Millisecond
	callerTTL      = 30 * time.Second
	callerEntries  = 10_000
)

// WithSource asks s for every declared value at start, above every other level. A second
// WithSource replaces the first; it cannot be combined with WithPushSource.
func WithSource(s Source) Option {
	return func(o *options) { o.source = s }
}

// WithPushSource takes every declared value from the snapshots s pushes. The module listens with
// /readyz at 503 until the first snapshot is accepted, and exits 1 when none is within 30
// seconds. A declared value set in the env or an env file then stops the start (managed_env).
func WithPushSource(s PushSource) Option {
	return func(o *options) { o.push = s }
}

// WithCallerSource asks s first when a getter's ctx carries a caller.
func WithCallerSource(s CallerSource) Option {
	return func(o *options) { o.callers = s }
}

// WithBindingResolver asks r first when a client getter's ctx carries a caller.
func WithBindingResolver(r BindingResolver) Option {
	return func(o *options) { o.resolver = r }
}

// ask runs fn under budget and stops waiting once the budget is spent, even when fn ignores ctx.
// A timeout is an error.
func ask[T any](ctx context.Context, budget time.Duration, fn func(context.Context) (T, bool, error)) (T, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type answer struct {
		v     T
		found bool
		err   error
	}
	got := make(chan answer, 1)
	go func() {
		v, found, err := fn(ctx)
		got <- answer{v, found, err}
	}()
	select {
	case a := <-got:
		return a.v, a.found, a.err
	case <-ctx.Done():
		var zero T
		return zero, false, ctx.Err()
	}
}
