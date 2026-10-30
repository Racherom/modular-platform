// Package sdk runs a module container (spec 0005). A module declares its config values and
// bindings once, in its catalog entry; sdkgen turns that into typed getters and a Declaration;
// Run loads every value from env vars, files and env files, refuses to start on any problem,
// checks each consumer's binding token, serves health and readiness, and shuts down gracefully.
//
//	//go:generate go tool sdkgen -entry ../../../../catalog/planner.yaml -package config -out config.gen.go
//	func main() { sdk.Run(config.Module, setup) }
package sdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/racherom/modular-platform/sdk/auth"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/sdk/internal/load"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// plainAddr is where a module listens without TLS.
const plainAddr = ":8080"

type (
	// ValueSpec declares one config value.
	ValueSpec = load.ValueSpec
	// BindingSpec declares one capability the module uses.
	BindingSpec = load.BindingSpec
	// ProvideSpec declares the capability the module provides.
	ProvideSpec = load.ProvideSpec
	// Secret holds a secret value; only Reveal returns it.
	Secret = load.Secret
	// Problem is one line of a refused start.
	Problem = load.Problem
)

// ErrStart reports a refused start; Build has logged the reasons.
var ErrStart = errors.New("sdk: the module cannot start")

// Declaration is a module's catalog entry as sdkgen writes it: its values, bindings and provide,
// and how to build the generated Config and Uses types.
type Declaration[C, U any] struct {
	Name     string // the catalog entry name
	Values   []ValueSpec
	Bindings []BindingSpec
	Provides *ProvideSpec
	New      func(*Values, *Bindings) (C, U)
}

// Bindings holds the clients of a module's bindings; the generated Uses type reads it.
type Bindings struct{}

// Module is what setup gets: the module's config and bindings, its logger, and the switches it
// may turn before it returns its handler.
type Module[C, U any] struct {
	Config   C
	Uses     U
	Log      *slog.Logger
	Instance string // MP_INSTANCE

	anonymous bool
	limiter   *httpkit.RateLimiter
	hops      int
}

// AllowAnonymous turns off the per binding token check for this run (logged once as a warning).
// Without it a module that provides with binding-token auth refuses to start without a token.
func (m *Module[C, U]) AllowAnonymous() { m.anonymous = true }

// WithRateLimit limits each client before the token check, keyed by the client address found
// hops proxies from the right of X-Forwarded-For. Health probes are never limited.
func (m *Module[C, U]) WithRateLimit(l *httpkit.RateLimiter, hops int) {
	m.limiter, m.hops = l, hops
}

// Setup builds the module's handler once the config is loaded. ctx ends at shutdown. A setup that
// has logged its own reasons returns an error wrapping ErrStart, and Build adds no line of its own.
type Setup[C, U any] func(ctx context.Context, m *Module[C, U]) (http.Handler, error)

// Option changes how Run, Main and Build start a module.
type Option func(*options)

type options struct {
	lookupEnv func(string) (string, bool)
	log       *slog.Logger
}

// WithEnv replaces the process env with env, for tests.
func WithEnv(env map[string]string) Option {
	return func(o *options) {
		o.lookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	}
}

// WithLogger replaces the JSON logger on stdout, for tests.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.log = l }
}

func newOptions(opts []Option) options {
	o := options{lookupEnv: os.LookupEnv}
	for _, opt := range opts {
		opt(&o)
	}
	if o.log == nil {
		o.log = httpkit.NewLogger()
	}
	return o
}

// Run is a module's main: `<binary> healthcheck` probes the running module's /readyz; otherwise
// it starts the module and serves until SIGINT or SIGTERM. It exits 1 on a refused start.
func Run[C, U any](d Declaration[C, U], setup Setup[C, U], opts ...Option) {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(httpkit.Healthcheck("http://127.0.0.1:8080/readyz"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Main(ctx, d, setup, opts...)
	stop()
	os.Exit(code)
}

// Main starts the module and serves until ctx ends. It returns the exit code: 1 when the start is
// refused (nothing listens then) or serving fails.
func Main[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], opts ...Option) int {
	o := newOptions(opts)
	h, err := Build(ctx, d, setup, opts...)
	if err != nil {
		return 1
	}
	if err := httpkit.Serve(ctx, o.log, plainAddr, h); err != nil {
		o.log.Error("module stopped", slog.Any("error", err))
		return 1
	}
	return 0
}

// Build loads and checks the config, runs setup and returns the module's whole handler: health
// and readiness, then the access log, the rate limit and the binding token check in front of the
// module's own handler. On a refused start it logs one line per problem and returns ErrStart.
func Build[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], opts ...Option) (http.Handler, error) {
	o := newOptions(opts)
	log := o.log
	res := load.Load(load.Input{
		Values:    d.Values,
		Bindings:  d.Bindings,
		Provides:  d.Provides,
		LookupEnv: o.lookupEnv,
		ReadFile:  os.ReadFile,
	})
	problems := res.Problems

	var clients auth.Clients
	if p := d.Provides; p != nil && p.Auth == manifest.AuthBindingToken && res.ClientsDir != "" {
		var errs []error
		clients, errs = auth.LoadClients(res.ClientsDir)
		for _, err := range errs {
			problems = append(problems, clientsProblem(p, load.CodeInvalidBinding, err.Error()))
		}
		load.SortProblems(problems)
	}
	if refuse(ctx, log, problems) {
		return nil, ErrStart
	}

	values := &Values{m: res.Values}
	cfg, uses := d.New(values, &Bindings{})
	if v, ok := any(cfg).(interface{ Validate(context.Context) error }); ok {
		if err := v.Validate(ctx); err != nil {
			refuse(ctx, log, []Problem{{Value: "module", Env: "module", Code: load.CodeValidate, Message: err.Error()}})
			return nil, ErrStart
		}
	}
	startLog(ctx, log, d, res)

	m := &Module[C, U]{Config: cfg, Uses: uses, Log: log, Instance: res.Bootstrap.Instance}
	h, err := setup(ctx, m)
	if err != nil {
		if !errors.Is(err, ErrStart) {
			log.ErrorContext(ctx, "module setup failed", slog.Any("error", err))
		}
		return nil, ErrStart
	}
	if p := d.Provides; p != nil && p.Auth == manifest.AuthBindingToken {
		switch {
		case m.anonymous:
			log.WarnContext(ctx, "binding auth off: anonymous calls allowed")
		case clients.Len() == 0:
			refuse(ctx, log, []Problem{clientsProblem(p, load.CodeMissing,
				fmt.Sprintf("binding token auth needs at least one consumer token; set %s to the clients directory", p.ClientsDirEnv))})
			return nil, ErrStart
		default:
			h = auth.Middleware(clients, h)
		}
	}
	if m.limiter != nil {
		h = m.limiter.Middleware(m.hops, nil, h)
	}
	h = httpkit.AccessLog(log, h)

	health := http.NewServeMux()
	// readiness waits on nothing yet: no binding or certificate is polled in this version
	httpkit.Health(health, func() bool { return true })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slices.Contains(httpkit.HealthPaths, r.URL.Path) {
			health.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
}

func clientsProblem(p *ProvideSpec, code, message string) Problem {
	return Problem{Value: "provides." + p.Capability + ".clients_dir", Env: p.ClientsDirEnv, Code: code, Message: message}
}

// refuse logs one error line per problem and reports whether there was any.
func refuse(ctx context.Context, log *slog.Logger, problems []Problem) bool {
	for _, p := range problems {
		log.ErrorContext(ctx, p.String())
	}
	return len(problems) > 0
}

// startLog writes the one config loaded line (each value's source, never a value), the warnings,
// and the plain HTTP warning.
func startLog[C, U any](ctx context.Context, log *slog.Logger, d Declaration[C, U], res load.Result) {
	values := make([]any, 0, len(d.Values))
	for _, v := range d.Values {
		values = append(values, slog.String(v.Name, res.Sources[v.Name]))
	}
	bindings := make([]any, 0, len(d.Bindings))
	for _, b := range d.Bindings {
		kind := b.Client
		if kind == "" {
			kind = "env"
		}
		bindings = append(bindings, slog.String(b.Capability, kind))
	}
	log.InfoContext(ctx, "config loaded",
		slog.String("tls", "plain"),
		slog.Group("values", values...),
		slog.Group("bindings", bindings...),
	)
	for _, w := range res.Warnings {
		log.WarnContext(ctx, w.Message, slog.String("value", w.Value), slog.String("at", w.At))
	}
	log.WarnContext(ctx, "serving plain HTTP: no certificate issuer bound")
}
