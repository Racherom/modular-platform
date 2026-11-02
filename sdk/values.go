package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/racherom/modular-platform/sdk/auth"
	"github.com/racherom/modular-platform/sdk/caller"
)

// Values holds a module's loaded values. Module code reads them only through the generated
// getters, which call Value and Lookup. A pushed snapshot replaces them whole.
type Values struct {
	specs   map[string]ValueSpec
	snap    atomic.Pointer[snapshot]
	callers *callerValues // nil without a CallerSource
}

// snapshot is one complete set of values, by name.
type snapshot struct {
	values map[string]any
}

func newValues(specs []ValueSpec, values map[string]any, o options) *Values {
	v := &Values{specs: map[string]ValueSpec{}}
	for _, s := range specs {
		v.specs[s.Name] = s
	}
	v.snap.Store(&snapshot{values: values})
	if o.callers != nil {
		v.callers = &callerValues{src: o.callers, budget: o.callerBudget, cache: newLRU[callerAnswer](callerEntries, o.callerTTL)}
	}
	return v
}

// swap makes values the current snapshot and drops every per caller answer.
func (v *Values) swap(values map[string]any) {
	v.snap.Store(&snapshot{values: values})
	if v.callers != nil {
		v.callers.cache.clear()
	}
}

// Format keeps every value out of fmt output.
func (*Values) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "sdk.Values{…}") }

// LogValue keeps every value out of slog output.
func (*Values) LogValue() slog.Value { return slog.StringValue("sdk.Values{…}") }

// Value returns the value name as T. The generated getters call it for a value that is always
// set (required, or with a default). With a CallerSource and a caller in ctx, the source is
// asked first; its failure is an error wrapping ErrCallerSource.
func Value[T any](ctx context.Context, v *Values, name string) (T, error) {
	t, ok, err := Lookup[T](ctx, v, name)
	if err == nil && !ok {
		err = fmt.Errorf("sdk: value %s is not set", name)
	}
	return t, err
}

// Lookup returns the value name as T, and whether it is set. The generated getters call it for an
// optional value without a default.
func Lookup[T any](ctx context.Context, v *Values, name string) (T, bool, error) {
	var zero T
	raw, ok, err := v.lookup(ctx, name)
	if err != nil || !ok {
		return zero, false, err
	}
	t, ok := raw.(T)
	if !ok {
		return zero, false, fmt.Errorf("sdk: value %s is a %T, not a %T", name, raw, zero)
	}
	return t, true, nil
}

func (v *Values) lookup(ctx context.Context, name string) (any, bool, error) {
	s := v.snap.Load()
	if c, ok := CallerFrom(ctx); ok && v.callers != nil {
		spec, declared := v.specs[name]
		if !declared {
			return nil, false, fmt.Errorf("sdk: no value %s is declared", name)
		}
		raw, found, err := v.callers.lookup(ctx, s, c, spec)
		if err != nil || found {
			return raw, found, err
		}
	}
	raw, ok := s.values[name]
	return raw, ok, nil
}

// callerValues asks a CallerSource for one caller's values and keeps each answer for a while.
type callerValues struct {
	src    CallerSource
	budget time.Duration
	cache  *lru[callerAnswer]
}

// callerAnswer is a parsed answer, or the fact that the source had none, and the snapshot it was
// asked under: an answer from before a swap never counts after it.
type callerAnswer struct {
	raw   any
	found bool
	snap  *snapshot
}

func (cv *callerValues) lookup(ctx context.Context, s *snapshot, c Caller, spec ValueSpec) (any, bool, error) {
	key := callerKey(c) + "\x00" + spec.Name
	if a, ok := cv.cache.get(key); ok && a.snap == s {
		return a.raw, a.found, nil
	}
	text, found, err := ask(ctx, cv.budget, func(ctx context.Context) (string, bool, error) {
		return cv.src.LookupFor(ctx, c, spec.Name)
	})
	if err != nil {
		// the source's own error is never shown: its text may carry a secret
		return nil, false, fmt.Errorf("%w: value %s", ErrCallerSource, spec.Name)
	}
	a := callerAnswer{found: found && text != "", snap: s}
	if a.found {
		if a.raw, err = spec.Parse(text); err != nil {
			return nil, false, fmt.Errorf("sdk: the caller source's answer for %s: %w", spec.Name, err)
		}
	}
	cv.cache.put(key, a)
	return a.raw, a.found, nil
}

// callerKey names one caller: its issuer, user and groups in name order.
func callerKey(c Caller) string {
	groups := slices.Sorted(slices.Values(c.Groups))
	return c.Issuer + "\x00" + c.User + "\x00" + strings.Join(groups, "\x1f")
}

// ErrUnbound reports a client getter called for an optional binding the graph left unbound.
var ErrUnbound = errors.New("sdk: the binding is not bound")

// Bindings holds the clients of a module's bindings, by capability. Module code reads them only
// through the generated Uses getters, which call Client.
type Bindings struct {
	m        map[string]any // nil value: an optional binding left unbound
	resolver *resolver      // nil without a BindingResolver
}

// Format keeps every client out of fmt output.
func (*Bindings) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "sdk.Bindings{…}") }

// LogValue keeps every client out of slog output.
func (*Bindings) LogValue() slog.Value { return slog.StringValue("sdk.Bindings{…}") }

// Client returns the client of the binding to capability as T. It returns ErrUnbound for an
// optional binding left unbound. With a BindingResolver and a caller in ctx, the resolver is
// asked first; its failure is an error wrapping ErrCallerSource.
func Client[T any](ctx context.Context, b *Bindings, capability string) (T, error) {
	var zero T
	raw, ok := b.m[capability]
	if !ok {
		return zero, fmt.Errorf("sdk: no binding %s is declared", capability)
	}
	if c, has := CallerFrom(ctx); has && b.resolver != nil {
		resolved, found, err := b.resolver.client(ctx, c, capability)
		if err != nil {
			return zero, err
		}
		if found {
			raw = resolved
		}
	}
	if raw == nil {
		return zero, fmt.Errorf("%w: %s", ErrUnbound, capability)
	}
	t, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("sdk: the client of %s is a %T, not a %T", capability, raw, zero)
	}
	return t, nil
}

// CallerModule returns the consumer instance whose binding token an incoming request carried.
func CallerModule(ctx context.Context) (string, bool) {
	return auth.CallerModule(ctx)
}

// Caller is the user a call is made for, and the module that minted its MP-Caller token.
type Caller = caller.Caller

// CallerFrom returns the caller of an incoming request (from a checked MP-Caller token), or the
// one WithCaller put into ctx.
func CallerFrom(ctx context.Context) (Caller, bool) {
	return caller.From(ctx)
}

// WithCaller returns ctx carrying c. api's sign in code calls it; a module holding
// MP_CALLER_SIGNING_KEY_FILE then sends c as an MP-Caller token on its http binding calls.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return caller.With(ctx, c)
}
