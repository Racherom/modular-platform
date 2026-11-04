package sdk

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// Command runs instead of serving, as `<binary> <name> [args...]`: it gets the module with its
// config loaded and checked and its binding clients open, but never runs setup, listens or gets
// a certificate. It writes what it has to say for a person to out, and its error makes the
// process exit 1.
type Command[C, U any] func(ctx context.Context, m *Module[C, U], args []string, out io.Writer) error

// WithCommand adds the command name to Run, such as `api admin reset` for the name admin.
// healthcheck is taken; a command with that name is never run.
func WithCommand[C, U any](name string, run Command[C, U]) Option {
	return func(o *options) {
		if o.commands == nil {
			o.commands = map[string]any{}
		}
		o.commands[name] = run
	}
}

// RunCommand loads the module as Run does and runs the command name with args, writing to out.
// It returns the exit code: 0 on success, 1 on a refused start, an unknown command, or the
// command's error, which is logged. Run calls it when the first argument names a command.
func RunCommand[C, U any](ctx context.Context, d Declaration[C, U], name string, args []string, out io.Writer, opts ...Option) int {
	o := newOptions(opts)
	run, ok := o.commands[name].(Command[C, U])
	if !ok {
		o.log.ErrorContext(ctx, "no such command", slog.String("command", name))
		return 1
	}
	if o.push != nil {
		o.log.ErrorContext(ctx, "a command cannot run with a push source", slog.String("command", name))
		return 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // closes the binding clients
	l, err := loadModule(ctx, d, o, true)
	if err != nil {
		return 1
	}
	if err := run(ctx, l.m, args, out); err != nil {
		if !errors.Is(err, ErrStart) {
			o.log.ErrorContext(ctx, "command failed", slog.String("command", name), slog.Any("error", err))
		}
		return 1
	}
	return 0
}

// runCommand runs the command the process arguments name, if any, and exits with its code.
func runCommand[C, U any](d Declaration[C, U], opts []Option) {
	if len(os.Args) < 2 {
		return
	}
	if _, ok := newOptions(opts).commands[os.Args[1]]; !ok {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := RunCommand(ctx, d, os.Args[1], os.Args[2:], os.Stdout, opts...)
	stop()
	os.Exit(code)
}
