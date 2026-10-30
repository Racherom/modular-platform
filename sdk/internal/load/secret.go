package load

import (
	"fmt"
	"io"
	"log/slog"
)

const redacted = "[redacted]"

// Secret holds a secret value. Only Reveal returns it: fmt, slog, JSON and text output show
// [redacted]. The value sits behind a pointer, so even a struct that holds a Secret in an
// unexported field prints an address, never the value.
type Secret struct {
	p *secretValue
}

type secretValue struct{ s string }

// NewSecret wraps s.
func NewSecret(s string) Secret { return Secret{p: &secretValue{s: s}} }

// Reveal returns the secret value. Pass it straight to where it is used; never log or print it.
func (s Secret) Reveal() string {
	if s.p == nil {
		return ""
	}
	return s.p.s
}

// String returns [redacted].
func (Secret) String() string { return redacted }

// GoString returns [redacted].
func (Secret) GoString() string { return redacted }

// Format writes [redacted] for every verb.
func (Secret) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue keeps the value out of slog output.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText keeps the value out of text encodings.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// MarshalJSON keeps the value out of JSON.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
