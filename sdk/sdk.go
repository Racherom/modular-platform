// Package sdk runs a module container (spec 0005). A module declares its config values and
// bindings once, in its catalog entry; sdkgen turns that into typed getters and a Declaration;
// Run loads every value from env vars, files and env files, refuses to start on any problem,
// checks each consumer's binding token, serves health and readiness, and shuts down gracefully.
// Options plug in a start source, a push source, a caller source and a binding resolver without
// changing module code.
//
//	//go:generate go tool sdkgen -entry ../../../../catalog/planner.yaml -package config -out config.gen.go
//	func main() { sdk.Run(config.Module, setup) }
package sdk

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/racherom/modular-platform/sdk/auth"
	"github.com/racherom/modular-platform/sdk/caller"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/sdk/internal/load"
	"github.com/racherom/modular-platform/sdk/manifest"
	"github.com/racherom/modular-platform/sdk/tlsacme"
)

// plainAddr is where a module listens without TLS; tlsacme.Addr is where it listens in TLS mode.
const plainAddr = ":8080"

type (
	// ValueSpec declares one config value.
	ValueSpec = load.ValueSpec
	// BindingSpec declares one capability the module uses.
	BindingSpec = load.BindingSpec
	// ProvideSpec declares the capability the module provides.
	ProvideSpec = load.ProvideSpec
	// Binding is a loaded binding: the provider's address and the credentials its client uses.
	Binding = load.Binding
	// Opener opens the client of a loaded binding; each client package has one.
	Opener = load.Opener
	// Opened is an open binding client, how readiness polls it, and how it closes.
	Opened = load.Opened
	// Secret holds a secret value; only Reveal returns it.
	Secret = load.Secret
	// Problem is one line of a refused start.
	Problem = load.Problem
)

// NewSecret wraps s, for a BindingResolver that hands the sdk a binding's credentials.
func NewSecret(s string) Secret { return load.NewSecret(s) }

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
	ready     []func() bool
}

// AllowAnonymous turns off the per binding token check for this run (logged once as a warning).
// Without it a module that provides with binding-token auth refuses to start without a token.
func (m *Module[C, U]) AllowAnonymous() { m.anonymous = true }

// WithRateLimit limits each client before the token check, keyed by the client address found
// hops proxies from the right of X-Forwarded-For. Health probes are never limited.
func (m *Module[C, U]) WithRateLimit(l *httpkit.RateLimiter, hops int) {
	m.limiter, m.hops = l, hops
}

// AddReady adds a check /readyz waits for, beside the bindings: for example a provider the module
// discovers on its own after start. check must be cheap and safe to call from any goroutine.
func (m *Module[C, U]) AddReady(check func() bool) { m.ready = append(m.ready, check) }

// moduleReady reports whether every check added with AddReady passes.
func (m *Module[C, U]) moduleReady() bool {
	for _, check := range m.ready {
		if !check() {
			return false
		}
	}
	return true
}

// Setup builds the module's handler once the config is loaded. ctx ends at shutdown. A setup that
// has logged its own reasons returns an error wrapping ErrStart, and Build adds no line of its own.
type Setup[C, U any] func(ctx context.Context, m *Module[C, U]) (http.Handler, error)

// Option changes how Run, Main and Build start a module.
type Option func(*options)

type options struct {
	lookupEnv func(string) (string, bool)
	log       *slog.Logger
	// issuer replaces the ACME client in TLS mode, for tests; nil outside them.
	issuer             tlsacme.Issuer
	plainAddr, tlsAddr string

	source   Source
	push     PushSource
	callers  CallerSource
	resolver BindingResolver
	// commands by name, each a Command of the module's own types
	commands map[string]any

	// the source limits; tests shorten them
	sourceDeadline, pushWait, callerBudget, callerTTL time.Duration
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
	o := options{
		lookupEnv: os.LookupEnv, plainAddr: plainAddr, tlsAddr: tlsacme.Addr,
		sourceDeadline: sourceDeadline, pushWait: pushWait, callerBudget: callerBudget, callerTTL: callerTTL,
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.log == nil {
		o.log = httpkit.NewLogger()
	}
	return o
}

// Run is a module's main: `<binary> healthcheck` probes the running module's /readyz, and
// `<binary> <name> [args...]` runs a command added with WithCommand; otherwise it starts the
// module and serves until SIGINT or SIGTERM. It exits 1 on a refused start.
func Run[C, U any](d Declaration[C, U], setup Setup[C, U], opts ...Option) {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(newOptions(opts)))
	}
	runCommand(d, opts)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Main(ctx, d, setup, opts...)
	stop()
	os.Exit(code)
}

// healthcheck probes the module's own /readyz: in TLS mode on the TLS port, trusting MP_CA_FILE
// and checking the module's host name; else in plain HTTP. It reads only the bootstrap names and
// exits 1 on a missing or bad one.
func healthcheck(o options) int {
	b, problems := load.ReadBootstrap(o.lookupEnv, os.ReadFile)
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "healthcheck: "+p.String())
		}
		return 1
	}
	if !b.TLS() {
		return httpkit.Healthcheck("http://"+loopback(o.plainAddr)+"/readyz", nil)
	}
	return httpkit.Healthcheck("https://"+loopback(o.tlsAddr)+"/readyz", tlsacme.ClientConfig(b.Roots, b.Host()))
}

// loopback turns a listen address like :8080 into one to call, 127.0.0.1:8080.
func loopback(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return net.JoinHostPort("127.0.0.1", port)
}

// Main starts the module and serves until ctx ends. It returns the exit code: 1 when the start is
// refused (nothing listens then) or serving fails. In TLS mode it listens first and then gets the
// module's certificate, so /readyz answers 503 until one is held. With a push source it listens
// with /readyz at 503 until the first snapshot is accepted, and returns 1 when none is in time.
// The binding clients close once the server has stopped, so a request still running at shutdown
// keeps its connections.
func Main[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], opts ...Option) int {
	o := newOptions(opts)
	clients, closeClients := context.WithCancel(context.WithoutCancel(ctx))
	defer closeClients()
	b, err := build(clients, d, setup, o)
	if err != nil {
		return 1
	}
	addr, cfg := o.plainAddr, (*tls.Config)(nil)
	if b.cert != nil {
		addr, cfg = o.tlsAddr, b.cert.ServerConfig()
	}
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		o.log.Error("module stopped", slog.Any("error", err))
		return 1
	}
	if b.cert != nil {
		go b.cert.Run(ctx)
	}
	serveCtx, stopServe := context.WithCancel(ctx)
	defer stopServe()
	var gaveUp atomic.Bool
	if b.first != nil {
		go func() {
			if err := <-b.first; errors.Is(err, ErrStart) {
				gaveUp.Store(true)
				stopServe()
			}
		}()
	}
	if err := httpkit.ServeListener(serveCtx, o.log, ln, b.handler, cfg); err != nil {
		o.log.Error("module stopped", slog.Any("error", err))
		return 1
	}
	if gaveUp.Load() {
		return 1
	}
	return 0
}

// Build loads and checks the config, opens the binding clients, runs setup and returns the
// module's whole handler: health and readiness, then the access log, the rate limit, the binding
// token check and the MP-Caller check in front of the module's own handler. /readyz answers 503 until every
// required binding has answered once and, in TLS mode, a certificate is held (Build gets none;
// Main does once it listens); the polls run until ctx ends, and the clients close then. With a
// push source it returns once the first snapshot is accepted. On a refused start it logs one line
// per problem and returns ErrStart.
func Build[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], opts ...Option) (http.Handler, error) {
	b, err := build(ctx, d, setup, newOptions(opts))
	if err != nil {
		return nil, err
	}
	if b.first != nil {
		if err := <-b.first; err != nil {
			return nil, err
		}
	}
	return b.handler, nil
}

// built is a module ready to serve: its handler and, in TLS mode, its certificate.
type built struct {
	handler http.Handler
	cert    *tlsacme.Manager // nil in plain mode
	first   <-chan error     // with a push source: the first snapshot's outcome; nil without one
}

// loaded is a module whose config is loaded and checked and whose binding clients are open.
type loaded[C, U any] struct {
	m        *Module[C, U]
	res      load.Result
	clients  auth.Clients
	opened   map[string]Opened
	values   *Values
	bindings *Bindings
}

// loadModule loads and checks the config and opens the binding clients, which close when ctx
// ends. On a refused start it logs one line per problem and returns ErrStart. quiet leaves out
// the config loaded line and the start warnings, for a command that does not serve.
func loadModule[C, U any](ctx context.Context, d Declaration[C, U], o options, quiet bool) (loaded[C, U], error) {
	log := o.log
	if o.source != nil && o.push != nil {
		// a pushed snapshot is complete, so nothing may answer above it
		refuse(ctx, log, []Problem{{
			Value: "module", Env: "module", Code: load.CodeSourceError,
			Message: "WithSource and WithPushSource cannot be combined",
		}})
		return loaded[C, U]{}, ErrStart
	}
	in := load.Input{
		Values:         d.Values,
		Bindings:       d.Bindings,
		Provides:       d.Provides,
		LookupEnv:      o.lookupEnv,
		ReadFile:       os.ReadFile,
		SourceDeadline: o.sourceDeadline,
		Managed:        o.push != nil,
	}
	if o.source != nil {
		in.Source = o.source.Lookup
	}
	res := load.Load(ctx, in)
	problems := res.Problems

	var clients auth.Clients
	if p := d.Provides; p != nil && p.Auth == manifest.AuthBindingToken && res.ClientsDir != "" {
		var errs []error
		clients, errs = auth.LoadClients(res.ClientsDir)
		for _, err := range errs {
			problems = append(problems, clientsProblem(p, load.CodeInvalidBinding, err.Error()))
		}
	}
	opened, openProblems := open(d.Bindings, res.Bindings)
	problems = append(problems, openProblems...)
	load.SortProblems(problems)
	if refuse(ctx, log, problems) {
		closeAll(opened)
		return loaded[C, U]{}, ErrStart
	}
	context.AfterFunc(ctx, func() { closeAll(opened) })

	values := newValues(d.Values, res.Values, o)
	bindings := &Bindings{m: map[string]any{}}
	for _, b := range res.Bindings {
		bindings.m[b.Capability] = nil // an optional binding left unbound
		if o, ok := opened[b.Capability]; ok {
			bindings.m[b.Capability] = o.Client
		}
	}
	if o.resolver != nil {
		bindings.resolver = newResolver(ctx, d.Bindings, res.Bootstrap, o)
	}
	cfg, uses := d.New(values, bindings)
	if o.push == nil {
		if err := validate(ctx, cfg); err != nil {
			refuse(ctx, log, []Problem{validateProblem(err)})
			return loaded[C, U]{}, ErrStart
		}
		if !quiet {
			configLoaded(ctx, log, d, res.Bootstrap, res.Sources)
		}
	}
	if !quiet {
		startWarnings(ctx, log, res)
	}

	m := &Module[C, U]{Config: cfg, Uses: uses, Log: log, Instance: res.Bootstrap.Instance}
	return loaded[C, U]{m: m, res: res, clients: clients, opened: opened, values: values, bindings: bindings}, nil
}

func build[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], o options) (built, error) {
	log := o.log
	l, err := loadModule(ctx, d, o, false)
	if err != nil {
		return built{}, err
	}
	m, res, clients, opened, values, bindings := l.m, l.res, l.clients, l.opened, l.values, l.bindings
	serve := func() (http.Handler, error) { return wrap(ctx, d, setup, m, clients, res.Bootstrap) }
	var (
		app   atomic.Pointer[http.Handler]
		first <-chan error
	)
	if o.push == nil {
		h, err := serve()
		if err != nil {
			return built{}, err
		}
		app.Store(&h)
	} else {
		p := newPusher(d, values, bindings, res.Bootstrap, o, &app, serve)
		p.start(ctx, o.push)
		first = p.first
	}

	var r readiness
	for _, b := range res.Bindings {
		if b.Bound && !b.Optional {
			r.wait(ctx, log, b.Capability, opened[b.Capability].Ready)
		}
	}
	var cert *tlsacme.Manager
	ready := func() bool { return r.ready() && app.Load() != nil && m.moduleReady() }
	if bs := res.Bootstrap; bs.TLS() {
		issuer := o.issuer
		if issuer == nil {
			c, err := tlsacme.NewClient(bs.ACMEURL, bs.Roots)
			if err != nil {
				log.ErrorContext(ctx, "module setup failed", slog.Any("error", err))
				return built{}, ErrStart
			}
			issuer = c
		}
		cert = tlsacme.New(issuer, bs.Host(), bs.Roots, log)
		ready = func() bool { return r.ready() && app.Load() != nil && m.moduleReady() && cert.Ready() }
	}
	health := http.NewServeMux()
	httpkit.Health(health, ready)
	return built{
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(httpkit.HealthPaths, r.URL.Path) {
				health.ServeHTTP(w, r)
				return
			}
			h := app.Load()
			if h == nil {
				httpkit.WriteProblem(w, http.StatusServiceUnavailable, "not_ready", "the module is waiting for its first config snapshot")
				return
			}
			(*h).ServeHTTP(w, r)
		}),
		cert:  cert,
		first: first,
	}, nil
}

// wrap runs setup and puts the module's middleware around its handler: the access log, the rate
// limit, the binding token check and the MP-Caller check, in that order.
func wrap[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], m *Module[C, U], clients auth.Clients, bs load.Bootstrap) (http.Handler, error) {
	log := m.Log
	h, err := setup(ctx, m)
	if err != nil {
		if !errors.Is(err, ErrStart) {
			log.ErrorContext(ctx, "module setup failed", slog.Any("error", err))
		}
		return nil, ErrStart
	}
	h = caller.Middleware(bs.CallerKeys, bs.Host(), log, h)
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
	return httpkit.AccessLog(log, h), nil
}

// validate runs the module's optional Validate on cfg.
func validate[C any](ctx context.Context, cfg C) error {
	if v, ok := any(cfg).(interface{ Validate(context.Context) error }); ok {
		return v.Validate(ctx)
	}
	return nil
}

func validateProblem(err error) Problem {
	return Problem{Value: "module", Env: "module", Code: load.CodeValidate, Message: err.Error()}
}

// open opens the client of every bound binding with its spec's Opener. A binding that cannot be
// opened is an invalid_binding problem; the clients opened so far are returned all the same, so
// the caller can close them.
func open(specs []BindingSpec, loaded []Binding) (map[string]Opened, []Problem) {
	openers := map[string]Opener{}
	for _, s := range specs {
		openers[s.Capability] = s.Open
	}
	opened := map[string]Opened{}
	var problems []Problem
	for _, b := range loaded {
		if !b.Bound {
			continue
		}
		path := "uses." + b.Capability
		openFn := openers[b.Capability]
		if openFn == nil {
			problems = append(problems, Problem{
				Value: path, Env: path, Code: load.CodeInvalidBinding,
				Message: fmt.Sprintf("the declaration has no opener for the %s client; run sdkgen again", b.Client),
			})
			continue
		}
		o, err := openFn(b)
		if err != nil {
			problems = append(problems, Problem{Value: path, Env: path, Code: load.CodeInvalidBinding, Message: err.Error()})
			continue
		}
		opened[b.Capability] = o
	}
	return opened, problems
}

func closeAll(opened map[string]Opened) {
	for _, o := range opened {
		if o.Close != nil {
			o.Close()
		}
	}
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

// configLoaded writes the one config loaded line: the host name, the TLS mode, each value's
// source (never a value) and each binding's client kind. With a push source it is written for
// every accepted snapshot.
func configLoaded[C, U any](ctx context.Context, log *slog.Logger, d Declaration[C, U], bs load.Bootstrap, sources map[string]string) {
	values := make([]any, 0, len(d.Values))
	for _, v := range d.Values {
		values = append(values, slog.String(v.Name, sources[v.Name]))
	}
	bindings := make([]any, 0, len(d.Bindings))
	for _, b := range d.Bindings {
		kind := b.Client
		if kind == "" {
			kind = "env"
		}
		bindings = append(bindings, slog.String(b.Capability, kind))
	}
	mode := "plain"
	if bs.TLS() {
		mode = "tls"
	}
	log.InfoContext(ctx, "config loaded",
		slog.String("host", bs.Host()),
		slog.String("tls", mode),
		slog.Group("values", values...),
		slog.Group("bindings", bindings...),
	)
}

// startWarnings writes the load's warnings and, in plain mode, the plain HTTP warning, once at
// start.
func startWarnings(ctx context.Context, log *slog.Logger, res load.Result) {
	for _, w := range res.Warnings {
		log.WarnContext(ctx, w.Message, slog.String("value", w.Value), slog.String("at", w.At))
	}
	if !res.Bootstrap.TLS() {
		log.WarnContext(ctx, "serving plain HTTP: no certificate issuer bound")
	}
}
