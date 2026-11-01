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
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/racherom/modular-platform/sdk/auth"
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
	// certificates replaces autocert in TLS mode, for tests; nil outside them.
	certificates       tlsacme.Getter
	plainAddr, tlsAddr string
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
	o := options{lookupEnv: os.LookupEnv, plainAddr: plainAddr, tlsAddr: tlsacme.Addr}
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
		os.Exit(healthcheck(newOptions(opts)))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Main(ctx, d, setup, opts...)
	stop()
	os.Exit(code)
}

// healthcheck probes the module's own /readyz: in TLS mode on the TLS port, trusting MP_CA_FILE
// and checking the module's host name; else in plain HTTP. It reads only the bootstrap names and
// exits 1 on a missing or bad one.
func healthcheck(o options) int {
	b, problems := load.LoadBootstrap(o.lookupEnv, os.ReadFile)
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
// module's certificate, so /readyz answers 503 until one is held. The binding clients close once
// the server has stopped, so a request still running at shutdown keeps its connections.
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
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		o.log.Error("module stopped", slog.Any("error", err))
		return 1
	}
	if b.cert != nil {
		go b.cert.Run(ctx)
	}
	if err := httpkit.ServeListener(ctx, o.log, ln, b.handler, cfg); err != nil {
		o.log.Error("module stopped", slog.Any("error", err))
		return 1
	}
	return 0
}

// Build loads and checks the config, opens the binding clients, runs setup and returns the
// module's whole handler: health and readiness, then the access log, the rate limit and the
// binding token check in front of the module's own handler. /readyz answers 503 until every
// required binding has answered once and, in TLS mode, a certificate is held (Build gets none;
// Main does once it listens); the polls run until ctx ends, and the clients close then. On a
// refused start it logs one line per problem and returns ErrStart.
func Build[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], opts ...Option) (http.Handler, error) {
	b, err := build(ctx, d, setup, newOptions(opts))
	if err != nil {
		return nil, err
	}
	return b.handler, nil
}

// built is a module ready to serve: its handler and, in TLS mode, its certificate.
type built struct {
	handler http.Handler
	cert    *tlsacme.Manager // nil in plain mode
}

func build[C, U any](ctx context.Context, d Declaration[C, U], setup Setup[C, U], o options) (built, error) {
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
	}
	opened, openProblems := open(d.Bindings, res.Bindings)
	problems = append(problems, openProblems...)
	load.SortProblems(problems)
	if refuse(ctx, log, problems) {
		closeAll(opened)
		return built{}, ErrStart
	}
	context.AfterFunc(ctx, func() { closeAll(opened) })

	values := &Values{m: res.Values}
	bindings := &Bindings{m: map[string]any{}}
	for _, b := range res.Bindings {
		bindings.m[b.Capability] = nil // an optional binding left unbound
		if o, ok := opened[b.Capability]; ok {
			bindings.m[b.Capability] = o.Client
		}
	}
	cfg, uses := d.New(values, bindings)
	if v, ok := any(cfg).(interface{ Validate(context.Context) error }); ok {
		if err := v.Validate(ctx); err != nil {
			refuse(ctx, log, []Problem{{Value: "module", Env: "module", Code: load.CodeValidate, Message: err.Error()}})
			return built{}, ErrStart
		}
	}
	startLog(ctx, log, d, res)

	m := &Module[C, U]{Config: cfg, Uses: uses, Log: log, Instance: res.Bootstrap.Instance}
	h, err := setup(ctx, m)
	if err != nil {
		if !errors.Is(err, ErrStart) {
			log.ErrorContext(ctx, "module setup failed", slog.Any("error", err))
		}
		return built{}, ErrStart
	}
	if p := d.Provides; p != nil && p.Auth == manifest.AuthBindingToken {
		switch {
		case m.anonymous:
			log.WarnContext(ctx, "binding auth off: anonymous calls allowed")
		case clients.Len() == 0:
			refuse(ctx, log, []Problem{clientsProblem(p, load.CodeMissing,
				fmt.Sprintf("binding token auth needs at least one consumer token; set %s to the clients directory", p.ClientsDirEnv))})
			return built{}, ErrStart
		default:
			h = auth.Middleware(clients, h)
		}
	}
	if m.limiter != nil {
		h = m.limiter.Middleware(m.hops, nil, h)
	}
	h = httpkit.AccessLog(log, h)

	var r readiness
	for _, b := range res.Bindings {
		if b.Bound && !b.Optional {
			r.wait(ctx, log, b.Capability, opened[b.Capability].Ready)
		}
	}
	var cert *tlsacme.Manager
	ready := r.ready
	if bs := res.Bootstrap; bs.TLS() {
		get := o.certificates
		if get == nil {
			acme, err := tlsacme.ACME(bs.ACMEURL, bs.Roots, bs.Host())
			if err != nil {
				log.ErrorContext(ctx, "module setup failed", slog.Any("error", err))
				return built{}, ErrStart
			}
			get = acme
		}
		cert = tlsacme.New(get, bs.Host(), log)
		ready = func() bool { return r.ready() && cert.Ready() }
	}
	health := http.NewServeMux()
	httpkit.Health(health, ready)
	return built{
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(httpkit.HealthPaths, r.URL.Path) {
				health.ServeHTTP(w, r)
				return
			}
			h.ServeHTTP(w, r)
		}),
		cert: cert,
	}, nil
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
			problems = append(problems, Problem{Value: path, Env: path, Code: load.CodeInvalidBinding,
				Message: fmt.Sprintf("the declaration has no opener for the %s client; run sdkgen again", b.Client)})
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

// startLog writes the one config loaded line (the host name, the TLS mode, each value's source,
// never a value, and each binding's client kind), the warnings, and in plain mode the plain HTTP
// warning.
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
	mode := "plain"
	if res.Bootstrap.TLS() {
		mode = "tls"
	}
	log.InfoContext(ctx, "config loaded",
		slog.String("host", res.Bootstrap.Host()),
		slog.String("tls", mode),
		slog.Group("values", values...),
		slog.Group("bindings", bindings...),
	)
	for _, w := range res.Warnings {
		log.WarnContext(ctx, w.Message, slog.String("value", w.Value), slog.String("at", w.At))
	}
	if !res.Bootstrap.TLS() {
		log.WarnContext(ctx, "serving plain HTTP: no certificate issuer bound")
	}
}
