package sdk

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/racherom/modular-platform/sdk/internal/load"
)

// noSnapshot is the one line of a module whose push source sent no acceptable snapshot in time.
var noSnapshot = Problem{Value: "push", Env: "push", Code: load.CodeNoSnapshot, Message: "no snapshot arrived within 30 seconds"}

// pusher takes a module's values from a PushSource (spec 0005, "Pluggable sources"). Each
// snapshot is checked whole with the start rules and Validate; a refused one is logged and the
// previous one stays. The first accepted snapshot runs setup, and the module's handler serves from
// then on.
type pusher[C, U any] struct {
	d        Declaration[C, U]
	values   *Values
	bindings *Bindings
	boot     load.Bootstrap
	log      *slog.Logger
	wait     time.Duration
	serve    func() (http.Handler, error) // setup and the middleware around it
	app      *atomic.Pointer[http.Handler]

	// first gets one result: nil once the first snapshot is accepted and setup ran; ErrStart when
	// none was accepted in time or setup failed; ctx's error when the module stopped first.
	first chan error

	mu       sync.Mutex
	state    pushState
	accepted chan struct{} // closed when the first snapshot is accepted
}

type pushState int

const (
	pushWaiting pushState = iota
	pushRunning
	pushGaveUp
)

func newPusher[C, U any](d Declaration[C, U], values *Values, bindings *Bindings, boot load.Bootstrap, o options,
	app *atomic.Pointer[http.Handler], serve func() (http.Handler, error),
) *pusher[C, U] {
	return &pusher[C, U]{
		d: d, values: values, bindings: bindings, boot: boot, log: o.log, wait: o.pushWait,
		serve: serve, app: app, first: make(chan error, 1), accepted: make(chan struct{}),
	}
}

// start watches src until ctx ends, and gives up on the first snapshot after the wait.
func (p *pusher[C, U]) start(ctx context.Context, src PushSource) {
	names := make([]string, 0, len(p.d.Values))
	for _, v := range p.d.Values {
		names = append(names, v.Name)
	}
	slices.Sort(names)
	go func() {
		// the source's own error is never shown: its text may carry a secret
		_ = src.Watch(ctx, names, func(snapshot map[string]string) error { return p.apply(ctx, snapshot) })
		if ctx.Err() == nil {
			p.log.WarnContext(ctx, "push source stopped")
		}
	}()
	go p.giveUp(ctx)
}

func (p *pusher[C, U]) giveUp(ctx context.Context) {
	t := time.NewTimer(p.wait)
	defer t.Stop()
	select {
	case <-p.accepted:
		return
	case <-t.C:
	case <-ctx.Done():
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != pushWaiting {
		return
	}
	p.state = pushGaveUp
	if err := ctx.Err(); err != nil {
		p.first <- err
		return
	}
	p.log.ErrorContext(ctx, noSnapshot.String())
	p.first <- ErrStart
}

// apply checks snapshot and, when it has no problem, makes it the module's values.
func (p *pusher[C, U]) apply(ctx context.Context, snapshot map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == pushGaveUp {
		return fmt.Errorf("%w: the module is stopping", ErrSnapshotRefused)
	}
	values, sources, problems := load.Snapshot(p.d.Values, snapshot)
	if len(problems) == 0 {
		candidate := newValues(p.d.Values, values, options{})
		cfg, _ := p.d.New(candidate, p.bindings)
		if err := validate(ctx, cfg); err != nil {
			problems = append(problems, validateProblem(err))
		}
	}
	if refuse(ctx, p.log, problems) {
		return fmt.Errorf("%w: %d problems", ErrSnapshotRefused, len(problems))
	}
	p.values.swap(values)
	configLoaded(ctx, p.log, p.d, p.boot, sources)
	if p.state != pushWaiting {
		return nil
	}
	p.state = pushRunning
	close(p.accepted)
	h, err := p.serve()
	if err != nil {
		p.first <- ErrStart
		return nil // the snapshot itself was fine
	}
	p.app.Store(&h)
	p.first <- nil
	return nil
}
