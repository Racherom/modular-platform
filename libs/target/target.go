// Package target defines where a rendered project goes. The zip target (static mode) writes it
// as a download; deployer-docker (managed mode, feature #16) applies it to a host; a cluster
// target is one more implementation.
package target

import (
	"context"

	"github.com/racherom/modular-platform/libs/render"
)

// Target applies a rendered project.
type Target interface {
	Apply(ctx context.Context, out render.Output) error
}
