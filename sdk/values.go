package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/racherom/modular-platform/sdk/auth"
)

// Values holds a module's loaded values. Module code reads them only through the generated
// getters, which call Value and Lookup.
type Values struct {
	m map[string]any
}

// Format keeps every value out of fmt output.
func (*Values) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "sdk.Values{…}") }

// LogValue keeps every value out of slog output.
func (*Values) LogValue() slog.Value { return slog.StringValue("sdk.Values{…}") }

// Value returns the value name as T. The generated getters call it for a value that is always
// set (required, or with a default); ctx is where a per caller source will look (spec 0005).
func Value[T any](ctx context.Context, v *Values, name string) (T, error) {
	t, ok, err := Lookup[T](ctx, v, name)
	if err == nil && !ok {
		err = fmt.Errorf("sdk: value %s is not set", name)
	}
	return t, err
}

// Lookup returns the value name as T, and whether it is set. The generated getters call it for an
// optional value without a default.
func Lookup[T any](_ context.Context, v *Values, name string) (T, bool, error) {
	var zero T
	raw, ok := v.m[name]
	if !ok {
		return zero, false, nil
	}
	t, ok := raw.(T)
	if !ok {
		return zero, false, fmt.Errorf("sdk: value %s is a %T, not a %T", name, raw, zero)
	}
	return t, true, nil
}

// ErrUnbound reports a client getter called for an optional binding the graph left unbound.
var ErrUnbound = errors.New("sdk: the binding is not bound")

// Bindings holds the clients of a module's bindings, by capability. Module code reads them only
// through the generated Uses getters, which call Client.
type Bindings struct {
	m map[string]any // nil value: an optional binding left unbound
}

// Format keeps every client out of fmt output.
func (*Bindings) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "sdk.Bindings{…}") }

// LogValue keeps every client out of slog output.
func (*Bindings) LogValue() slog.Value { return slog.StringValue("sdk.Bindings{…}") }

// Client returns the client of the binding to capability as T. It returns ErrUnbound for an
// optional binding left unbound; ctx is where a per caller binding resolver will look (spec 0005).
func Client[T any](_ context.Context, b *Bindings, capability string) (T, error) {
	var zero T
	raw, ok := b.m[capability]
	switch {
	case !ok:
		return zero, fmt.Errorf("sdk: no binding %s is declared", capability)
	case raw == nil:
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
