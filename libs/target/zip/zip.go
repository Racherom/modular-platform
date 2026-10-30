// Package zip is the static mode target: it writes a rendered project as a zip.
package zip

import (
	"context"
	"io"

	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/libs/target"
)

var _ target.Target = (*Target)(nil)

// Target writes each applied project to w as a zip.
type Target struct {
	w io.Writer
}

// New returns a zip target that writes to w.
func New(w io.Writer) *Target {
	return &Target{w: w}
}

// Apply writes out as a zip.
func (t *Target) Apply(ctx context.Context, out render.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return out.WriteZip(t.w)
}
