// Package load fills a module's declared values at start (spec 0005, "Sources and precedence"):
// bootstrap names from the process env only; a value from the start source, when one is set,
// else from its env var or the file its _FILE form names, else from the env files in MP_ENV_FILE
// (a later file overriding an earlier one), else from its default. With a push source the values
// come only from pushed snapshots, which Snapshot checks. It parses each value by type, checks its
// rules and reports every problem in one pass, as lines that never carry a value.
package load

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/racherom/modular-platform/sdk/caller"
	"github.com/racherom/modular-platform/sdk/envfile"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// Problem codes of a start (spec 0005, "Problem codes").
const (
	CodeMissing         = "missing"
	CodeInvalid         = manifest.CodeInvalid
	CodeOutOfRange      = manifest.CodeOutOfRange
	CodeNoMatch         = manifest.CodeNoMatch
	CodeNotAllowed      = manifest.CodeNotAllowed
	CodeAmbiguousSource = "ambiguous_source"
	CodeSecretInEnv     = "secret_in_env"
	CodeEnvFile         = "env_file"
	CodeUnknownKey      = "unknown_key"
	CodeInvalidBinding  = "invalid_binding"
	CodeTLSConfig       = "tls_config"
	CodeDowngrade       = "downgrade_refused"
	CodeValidate        = "validate"
	CodeSourceError     = "source_error"
	CodeManagedEnv      = "managed_env"
	CodeNoSnapshot      = "no_snapshot"
)

// The fixed source_error messages: the error a start source returns is never printed, because
// its text may carry a secret.
const (
	SourceFailed   = "the start source failed"
	SourceDeadline = "the start source missed the 10 second deadline"
)

// Bootstrap names (manifest.BootstrapNames).
const (
	EnvInstance         = "MP_INSTANCE"
	EnvDomain           = "MP_DOMAIN"
	EnvEnvFile          = "MP_ENV_FILE"
	EnvACMEURL          = "MP_ACME_URL"
	EnvCAFile           = "MP_CA_FILE"
	EnvCallerKeyFile    = "MP_CALLER_KEY_FILE"
	EnvCallerSigningKey = "MP_CALLER_SIGNING_KEY_FILE"
)

// ValueSpec declares one config value; sdkgen writes it from the catalog entry.
type ValueSpec struct {
	Name        string
	Env         string
	Type        string
	Description string
	Default     string // empty: no default
	Optional    bool
	Min, Max    string
	Pattern     string
	Values      []string
	Generate    bool
}

func (v ValueSpec) rules() manifest.Rules {
	return manifest.Rules{Type: v.Type, Min: v.Min, Max: v.Max, Pattern: v.Pattern, Values: v.Values}
}

// Parse parses text by v's type and checks v's rules; a secret comes back as a Secret. A failure
// is a *manifest.RuleError, whose message never carries a secret's text.
func (v ValueSpec) Parse(text string) (any, error) {
	parsed, err := v.rules().Parse(text)
	if err != nil {
		return nil, err
	}
	if v.Type == manifest.TypeSecret {
		parsed = NewSecret(parsed.(string))
	}
	return parsed, nil
}

// BindingSpec declares one capability the module uses.
type BindingSpec struct {
	Capability string
	Client     string // empty: env mapping only
	Optional   bool
	Env        map[string]string // binding key to env var name
	Open       Opener            // opens the client; sdkgen sets it for every client binding
}

// Opener opens the client of a loaded binding. Each client package has one, so a module links
// only the clients its catalog entry uses.
type Opener func(Binding) (Opened, error)

// Opened is an open binding client.
type Opened struct {
	Client any                         // what the generated getter returns
	Ready  func(context.Context) error // polled until it answers once
	Close  func()                      // run once the server has stopped; nil when there is nothing to close
}

// ProvideSpec declares the capability the module provides.
type ProvideSpec struct {
	Capability    string
	Auth          string // manifest.AuthBindingToken or empty
	ClientsDirEnv string
}

// Problem is one line of a refused start: config: <value> (<ENV>): <code>: <message>.
type Problem struct {
	Value   string // the value name; a bootstrap or binding key's own name; module for validate
	Env     string
	Code    string
	Message string
	File    string // the env file, for sorting
	Line    int
}

func (p Problem) String() string {
	return fmt.Sprintf("config: %s (%s): %s: %s", p.Value, p.Env, p.Code, p.Message)
}

// SortProblems sorts by value part (byte order), then file, then line.
func SortProblems(ps []Problem) {
	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(strings.Compare(a.Value, b.Value), strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
}

// Warning is one warning line of a successful start.
type Warning struct {
	Message string
	Value   string
	At      string // file:line
}

// Input is what Load reads.
type Input struct {
	Values   []ValueSpec
	Bindings []BindingSpec
	Provides *ProvideSpec
	// LookupEnv reads the process env (os.LookupEnv outside tests).
	LookupEnv func(string) (string, bool)
	// ReadFile reads a file the env names: an env file or a _FILE value.
	ReadFile func(string) ([]byte, error)
	// Source is the start source's Lookup, asked by value name above every other level; nil
	// without one.
	Source func(ctx context.Context, value string) (string, bool, error)
	// SourceDeadline bounds all Source calls together.
	SourceDeadline time.Duration
	// Managed is set when a push source supplies the values: Load reads no value then, and a
	// declared value found in the process env or an env file is a managed_env problem. Bootstrap
	// names, binding keys and the clients directory are still read from env.
	Managed bool
}

// Bootstrap holds the bootstrap names.
type Bootstrap struct {
	Instance          string
	Domain            string // empty in plain mode without a host name suffix
	ACMEURL, CAFile   string
	Roots             *x509.CertPool // the certificates in CAFile; nil in plain mode
	CallerKeyFile     string
	CallerSigningFile string
	// CallerKeys are the keys in CallerKeyFile; empty when it is unset. Load reads them, and
	// ReadBootstrap does not.
	CallerKeys caller.Keys
	// CallerSigner mints with the key in CallerSigningFile as Instance; nil when it is unset. Load
	// reads it, and ReadBootstrap does not.
	CallerSigner *caller.Signer
}

// TLS reports whether the module runs in TLS mode: both MP_ACME_URL and MP_CA_FILE are set.
func (b Bootstrap) TLS() bool { return b.ACMEURL != "" && b.CAFile != "" }

// Host is the one name the module goes by: <MP_INSTANCE>.<MP_DOMAIN>, or MP_INSTANCE alone when
// MP_DOMAIN is unset. It names the module's certificate, its network alias and the aud it accepts.
func (b Bootstrap) Host() string {
	if b.Domain == "" {
		return b.Instance
	}
	return b.Instance + "." + b.Domain
}

// ReadBootstrap reads and checks the bootstrap names alone, for the healthcheck subcommand, which
// loads no config.
func ReadBootstrap(lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) (Bootstrap, []Problem) {
	l := loader{in: Input{LookupEnv: lookupEnv, ReadFile: readFile}}
	l.bootstrap()
	SortProblems(l.res.Problems)
	return l.res.Bootstrap, l.res.Problems
}

// Binding is a loaded binding with a client, in capability order. Its secrets hide themselves
// from fmt and slog.
type Binding struct {
	Capability string
	Client     string
	Optional   bool
	Bound      bool           // false only for an optional binding whose url is unset
	URL        *url.URL       // the provider's address
	Token      Secret         // the content of token_file; empty when token_file is unset
	Roots      *x509.CertPool // the certificates in ca_file, else MP_CA_FILE's in TLS mode; nil in plain mode without ca_file
	Caller     *caller.Signer // mints MP-Caller on calls to an http binding; nil without MP_CALLER_SIGNING_KEY_FILE

	// postgresql
	Database, User string
	Password       Secret // the content of password_file
	SSLMode        string // SSLModeDisable or SSLModeVerifyFull

	// s3
	Bucket, Region string // Region is empty when unset
	AccessKeyID    string
	SecretKey      Secret // the content of secret_access_key_file
}

// The sslmode values a postgresql binding takes; unset means SSLModeDisable, as the render writes
// it while no unchanged provider has TLS (spec 0005).
const (
	SSLModeDisable    = "disable"
	SSLModeVerifyFull = "verify-full"
)

// Result is a finished load. Values holds the parsed value of every set value, by name (a Secret
// for a secret); Sources the source label of every value.
type Result struct {
	Bootstrap  Bootstrap
	Values     map[string]any
	Sources    map[string]string
	Bindings   []Binding
	ClientsDir string // empty when unset
	Warnings   []Warning
	Problems   []Problem // sorted; Load refuses the start when any is present
}

// level is one source of env style keys: the process env or one env file.
type level struct {
	file   string // empty for the process env
	lookup func(key string) (value string, line int, ok bool)
}

// Load reads every declared value and returns the result. ctx bounds the start source's calls.
func Load(ctx context.Context, in Input) Result {
	l := loader{in: in, res: Result{Values: map[string]any{}, Sources: map[string]string{}}}
	l.bootstrap()
	l.callerKeys()
	l.envFiles()
	answers := l.askSource(ctx)
	for _, v := range in.Values {
		switch text, asked := answers[v.Name]; {
		case in.Managed:
			l.managed(v)
		case asked && text == nil:
			// the source failed for v; askSource reported it
		case asked:
			l.parse(v, *text, "source")
		default:
			l.value(v)
		}
	}
	for _, b := range in.Bindings {
		l.binding(b)
	}
	l.clientsDir()
	SortProblems(l.res.Problems)
	return l.res
}

type loader struct {
	in     Input
	res    Result
	levels []level // highest precedence first
}

func (l *loader) problem(value, env, code, format string, args ...any) {
	l.res.Problems = append(l.res.Problems, Problem{Value: value, Env: env, Code: code, Message: fmt.Sprintf(format, args...)})
}

func (l *loader) env(key string) string {
	v, _ := l.in.LookupEnv(key)
	return v
}

func (l *loader) bootstrap() {
	b := &l.res.Bootstrap
	b.Instance = l.env(EnvInstance)
	switch {
	case b.Instance == "":
		l.problem(EnvInstance, EnvInstance, CodeMissing, "the module's instance name is required")
	case !manifest.ValidName(b.Instance):
		l.problem(EnvInstance, EnvInstance, CodeInvalid, "%q must be a lowercase DNS label", b.Instance)
	}
	// checked in plain mode too: the host name feeds binding URLs and the accepted aud there
	b.Domain = l.env(EnvDomain)
	if b.Domain != "" && !manifest.ValidDomain(b.Domain) {
		l.problem(EnvDomain, EnvDomain, CodeInvalid, "%q must be lowercase DNS labels joined by dots, like graph.internal", b.Domain)
	}
	b.ACMEURL, b.CAFile = l.env(EnvACMEURL), l.env(EnvCAFile)
	switch {
	case b.ACMEURL != "" && b.CAFile == "":
		l.problem(EnvCAFile, EnvCAFile, CodeTLSConfig, "%s is set, so %s must be set too", EnvACMEURL, EnvCAFile)
	case b.ACMEURL == "" && b.CAFile != "":
		l.problem(EnvACMEURL, EnvACMEURL, CodeTLSConfig, "%s is set, so %s must be set too", EnvCAFile, EnvACMEURL)
	case b.TLS():
		if u, err := url.Parse(b.ACMEURL); err != nil || u.Scheme != "https" || u.Host == "" {
			l.problem(EnvACMEURL, EnvACMEURL, CodeInvalid, "must be the issuer's ACME directory as an absolute https URL")
		}
		b.Roots = l.caFile(b.CAFile)
		if b.Domain == "" {
			// a bare instance name would be valid in every stack an issuer serves, so TLS names
			// carry the domain to stay unique per stack
			l.problem(EnvDomain, EnvDomain, CodeTLSConfig, "TLS mode needs the host name's domain; set %s, like <graph>.internal", EnvDomain)
		}
	}
	b.CallerKeyFile, b.CallerSigningFile = l.env(EnvCallerKeyFile), l.env(EnvCallerSigningKey)

	l.levels = append(l.levels, level{lookup: func(key string) (string, int, bool) {
		v, ok := l.in.LookupEnv(key)
		return v, 0, ok && v != ""
	}})
}

// callerKeys reads the MP-Caller key files: the public keys a token may be signed with, and the
// key this module signs with. An unreadable file or one that holds no such key is invalid.
func (l *loader) callerKeys() {
	b := &l.res.Bootstrap
	if path := b.CallerKeyFile; path != "" {
		data, err := l.in.ReadFile(path)
		if err == nil {
			b.CallerKeys, err = caller.ParseKeys(data)
		}
		if err != nil {
			l.problem(EnvCallerKeyFile, EnvCallerKeyFile, CodeInvalid, "the key file %s: %s", path, keyFileReason(err))
		}
	}
	if path := b.CallerSigningFile; path != "" {
		data, err := l.in.ReadFile(path)
		var key ed25519.PrivateKey
		if err == nil {
			key, err = caller.ParseSigningKey(data)
		}
		if err != nil {
			l.problem(EnvCallerSigningKey, EnvCallerSigningKey, CodeInvalid, "the signing key file %s: %s", path, keyFileReason(err))
			return
		}
		b.CallerSigner = caller.NewSigner(key, b.Instance)
	}
}

// keyFileReason says why a key file failed, without the file's content.
func keyFileReason(err error) string {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return "it cannot be read"
	}
	return err.Error()
}

// caFile reads the certificates in MP_CA_FILE, the root CA of the issuer and of every module
// certificate. An unreadable file or one without a certificate is a tls_config problem.
func (l *loader) caFile(path string) *x509.CertPool {
	data, err := l.in.ReadFile(path)
	if err != nil {
		l.problem(EnvCAFile, EnvCAFile, CodeTLSConfig, "the CA file %s cannot be read", path)
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		l.problem(EnvCAFile, EnvCAFile, CodeTLSConfig, "the CA file %s holds no PEM certificate", path)
		return nil
	}
	return pool
}

// envFiles reads the files MP_ENV_FILE lists and adds them as levels, the last file first.
func (l *loader) envFiles() {
	known := l.knownKeys()
	var files []level
	for path := range strings.SplitSeq(l.env(EnvEnvFile), ":") {
		if path == "" {
			continue
		}
		data, err := l.in.ReadFile(path)
		if err != nil {
			reason := "cannot be read"
			if errors.Is(err, fs.ErrNotExist) {
				reason = "does not exist"
			}
			l.res.Problems = append(l.res.Problems, Problem{
				Value: EnvEnvFile, Env: EnvEnvFile, Code: CodeEnvFile,
				Message: fmt.Sprintf("%s %s", path, reason), File: path,
			})
			continue
		}
		lines, errs := envfile.Parse(data)
		for _, e := range errs {
			l.res.Problems = append(l.res.Problems, Problem{
				Value: EnvEnvFile, Env: EnvEnvFile, Code: CodeEnvFile,
				Message: fmt.Sprintf("%s:%d: %s", path, e.Line, e.Message), File: path, Line: e.Line,
			})
		}
		set := map[string]envfile.Line{}
		for _, line := range lines {
			if !known[line.Key] {
				l.res.Problems = append(l.res.Problems, Problem{
					Value: line.Key, Env: line.Key, Code: CodeUnknownKey,
					Message: fmt.Sprintf("%s:%d: %s is not a declared value, a binding key or a bootstrap name", path, line.Line, line.Key),
					File:    path, Line: line.Line,
				})
				continue
			}
			if line.Value != "" {
				set[line.Key] = line
			}
		}
		files = append(files, level{file: path, lookup: func(key string) (string, int, bool) {
			line, ok := set[key]
			return line.Value, line.Line, ok
		}})
	}
	slices.Reverse(files)
	l.levels = append(l.levels, files...)
}

func (l *loader) knownKeys() map[string]bool {
	known := map[string]bool{}
	for _, b := range manifest.BootstrapNames {
		known[b] = true
	}
	for _, v := range l.in.Values {
		known[v.Env], known[v.Env+"_FILE"] = true, true
	}
	for _, b := range l.in.Bindings {
		for _, env := range b.Env {
			known[env] = true
		}
	}
	if p := l.in.Provides; p != nil && p.ClientsDirEnv != "" {
		known[p.ClientsDirEnv] = true
	}
	return known
}

// askSource asks the start source for every declared value in name order, one after another,
// under one deadline shared by all calls. A found, non empty answer is returned as its text; a
// failed call, and every value once the deadline has passed, is a source_error and returned as
// nil; a value not in the map falls through to the lower levels. A call that ignores ctx holds
// the start no longer than the deadline: its answer is dropped.
func (l *loader) askSource(ctx context.Context) map[string]*string {
	if l.in.Source == nil || l.in.Managed {
		return nil
	}
	specs := slices.SortedFunc(slices.Values(l.in.Values), func(a, b ValueSpec) int { return strings.Compare(a.Name, b.Name) })
	ctx, cancel := context.WithTimeout(ctx, l.in.SourceDeadline)
	defer cancel()
	answers := map[string]*string{}
	for _, v := range specs {
		type answer struct {
			text  string
			found bool
			err   error
		}
		got := make(chan answer, 1)
		if ctx.Err() == nil {
			go func() {
				text, found, err := l.in.Source(ctx, v.Name)
				got <- answer{text, found, err}
			}()
		}
		var a answer
		select {
		case a = <-got:
		case <-ctx.Done():
			a.err = ctx.Err()
		}
		switch {
		case a.err != nil || ctx.Err() != nil:
			msg := SourceFailed
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = SourceDeadline
			}
			l.problem(v.Name, v.Env, CodeSourceError, "%s", msg)
			answers[v.Name] = nil
		case a.found && a.text != "":
			answers[v.Name] = &a.text
		}
	}
	return answers
}

// managed reports v as managed_env when the process env or an env file sets it, in either form:
// a push source supplies every declared value.
func (l *loader) managed(v ValueSpec) {
	for _, lv := range l.levels {
		for _, key := range []string{v.Env, v.Env + "_FILE"} {
			if _, line, ok := lv.lookup(key); ok {
				where := "the process env"
				if lv.file != "" {
					where = fmt.Sprintf("%s:%d", lv.file, line)
				}
				l.problem(v.Name, v.Env, CodeManagedEnv, "a push source supplies this value; remove %s from %s", key, where)
				return
			}
		}
	}
}

// value finds v at the highest level that sets it, parses it and checks its rules.
func (l *loader) value(v ValueSpec) {
	text, source, ok := l.find(v)
	if !ok {
		return // find reported the problem
	}
	if text == "" {
		switch {
		case v.Default != "":
			text, source = v.Default, "default"
		case v.Optional:
			l.res.Sources[v.Name] = "unset"
			return
		default:
			l.problem(v.Name, v.Env, CodeMissing, "a value is required; set %s or %s_FILE", v.Env, v.Env)
			return
		}
	}
	l.parse(v, text, source)
}

// parse parses text as v and records it with its source label, or reports its first broken rule.
func (l *loader) parse(v ValueSpec, text, source string) {
	parsed, err := v.Parse(text)
	if err != nil {
		re := err.(*manifest.RuleError) //nolint:errorlint // Parse returns only an unwrapped *RuleError
		l.problem(v.Name, v.Env, re.Code, "%s", re.Message)
		return
	}
	l.res.Values[v.Name] = parsed
	l.res.Sources[v.Name] = source
}

// Snapshot checks a pushed snapshot with the start rules and returns its values and source
// labels (push, default or unset). A snapshot is complete: a value it leaves out falls back to
// its default, or is missing. A name it carries that specs does not declare is unknown_key. Any
// problem refuses the whole snapshot.
func Snapshot(specs []ValueSpec, snapshot map[string]string) (values map[string]any, sources map[string]string, problems []Problem) {
	l := loader{res: Result{Values: map[string]any{}, Sources: map[string]string{}}}
	declared := map[string]bool{}
	for _, v := range specs {
		declared[v.Name] = true
		text, source := snapshot[v.Name], "push"
		if text == "" {
			switch {
			case v.Default != "":
				text, source = v.Default, "default"
			case v.Optional:
				l.res.Sources[v.Name] = "unset"
				continue
			default:
				l.problem(v.Name, v.Env, CodeMissing, "a value is required; the snapshot leaves it out")
				continue
			}
		}
		l.parse(v, text, source)
	}
	for name := range snapshot {
		if !declared[name] {
			l.problem(name, name, CodeUnknownKey, "the snapshot names %s, which the catalog entry does not declare", name)
		}
	}
	SortProblems(l.res.Problems)
	return l.res.Values, l.res.Sources, l.res.Problems
}

// find returns the text of v at the highest level that sets it, and its source label. An empty
// text means no level sets it. ok is false when a source rule broke (the problem is reported).
func (l *loader) find(v ValueSpec) (text, source string, ok bool) {
	for _, lv := range l.levels {
		plain, line, hasPlain := lv.lookup(v.Env)
		path, _, hasFile := lv.lookup(v.Env + "_FILE")
		switch {
		case hasPlain && hasFile:
			where := "the process env"
			if lv.file != "" {
				where = lv.file
			}
			l.problem(v.Name, v.Env, CodeAmbiguousSource, "%s and %s_FILE are both set in %s", v.Env, v.Env, where)
			return "", "", false
		case hasPlain:
			if lv.file == "" {
				if v.Type == manifest.TypeSecret {
					l.problem(v.Name, v.Env, CodeSecretInEnv, "a secret must come from a file; set %s_FILE instead", v.Env)
					return "", "", false
				}
				return plain, "env", true
			}
			at := fmt.Sprintf("%s:%d", lv.file, line)
			if v.Type == manifest.TypeSecret {
				l.res.Warnings = append(l.res.Warnings, Warning{Message: "secret on a plain env file line", Value: v.Name, At: at})
			}
			return plain, "env-file:" + at, true
		case hasFile:
			data, err := l.in.ReadFile(path)
			if err != nil {
				l.problem(v.Name, v.Env, CodeInvalid, "the file %s_FILE names cannot be read: %s", v.Env, path)
				return "", "", false
			}
			if s := strings.TrimRight(string(data), " \t\r\n"); s != "" {
				return s, "file:" + path, true
			}
			// an empty file counts as unset: look further down
		}
	}
	return "", "", true
}

// clientsDir finds the directory of consumer tokens, when the module provides with
// binding-token auth.
func (l *loader) clientsDir() {
	p := l.in.Provides
	if p == nil || p.Auth != manifest.AuthBindingToken || p.ClientsDirEnv == "" {
		return
	}
	l.res.ClientsDir = l.key(p.ClientsDirEnv)
}

// urlSchemes are the schemes a binding url may have, per client kind.
var urlSchemes = map[string][]string{
	manifest.ClientHTTP:       {"http", "https"},
	manifest.ClientPostgreSQL: {"postgres", "postgresql"},
	manifest.ClientS3:         {"http", "https"},
}

// binding loads the keys of a binding with a client: its url and the files and credentials its
// kind reads. Keys are plain env style keys, found at the highest level that sets them. An
// optional binding whose url is unset is unbound.
func (l *loader) binding(b BindingSpec) {
	if b.Client == "" {
		return // env mapping only: the module reads nothing through the sdk
	}
	bl := bindingLoader{l: l, spec: b, path: "uses." + b.Capability}
	urlEnv := b.Env["url"]
	text := l.key(urlEnv)
	if text == "" {
		if b.Optional {
			l.res.Bindings = append(l.res.Bindings, Binding{Capability: b.Capability, Client: b.Client, Optional: true})
			return
		}
		l.problem(bl.path+".url", urlEnv, CodeMissing, "the provider's address is required; set %s", urlEnv)
		return
	}
	bound := Binding{Capability: b.Capability, Client: b.Client, Optional: b.Optional, Bound: true}
	bound.URL = bl.url(text)
	bound.Token = bl.file("token_file", "token")
	bound.Roots = bl.roots()
	switch b.Client {
	case manifest.ClientHTTP:
		bound.Caller = l.res.Bootstrap.CallerSigner
	case manifest.ClientPostgreSQL:
		bound.Database = bl.required("database")
		bound.User = bl.required("user")
		bound.Password = bl.requiredFile("password_file", "password")
		bound.SSLMode = bl.sslMode()
	case manifest.ClientS3:
		bound.Bucket = bl.required("bucket")
		bound.Region = bl.optional("region")
		bound.AccessKeyID = bl.required("access_key_id")
		bound.SecretKey = bl.requiredFile("secret_access_key_file", "secret key")
	}
	if !bl.failed {
		l.res.Bindings = append(l.res.Bindings, bound)
	}
}

// bindingLoader reads the keys of one bound binding and reports each bad key.
type bindingLoader struct {
	l      *loader
	spec   BindingSpec
	path   string
	failed bool
}

func (bl *bindingLoader) problem(key, code, format string, args ...any) {
	bl.failed = true
	bl.l.problem(bl.path+"."+key, bl.spec.Env[key], code, format, args...)
}

func (bl *bindingLoader) url(text string) *url.URL {
	schemes := urlSchemes[bl.spec.Client]
	u, err := url.Parse(text)
	switch {
	case err != nil || !slices.Contains(schemes, u.Scheme) || u.Host == "":
		bl.problem("url", CodeInvalid, "must be an absolute %s URL with a host", strings.Join(schemes, " or "))
	case bl.downgrade(u):
		host := strings.ToLower(u.Hostname())
		bl.problem("url", CodeDowngrade, "%s is a module on the sdk, which TLS mode never calls in plain HTTP; use https://%s:8443", host, host)
	case bl.spec.Client != manifest.ClientHTTP && u.User != nil:
		bl.problem("url", CodeInvalid, "must not carry credentials; the binding's own keys hold them")
	case bl.spec.Client != manifest.ClientHTTP && strings.Trim(u.Path, "/") != "":
		bl.problem("url", CodeInvalid, "must have no path; the binding's own keys name the database or bucket")
	default:
		return u
	}
	return nil
}

// downgrade reports an http URL to a module on the sdk in TLS mode.
func (bl *bindingLoader) downgrade(u *url.URL) bool {
	return bl.l.res.Bootstrap.Downgrade(u)
}

// Downgrade reports an http URL to a module on the sdk in TLS mode: only those carry a host name
// ending in .<MP_DOMAIN>.
func (b Bootstrap) Downgrade(u *url.URL) bool {
	return b.TLS() && b.Domain != "" && u.Scheme == "http" && strings.HasSuffix(strings.ToLower(u.Hostname()), "."+b.Domain)
}

// optional returns the key's text, or "" when it is unmapped or unset.
func (bl *bindingLoader) optional(key string) string {
	return bl.l.key(bl.spec.Env[key])
}

// required returns the key's text and reports it missing when it is unset.
func (bl *bindingLoader) required(key string) string {
	text := bl.optional(key)
	if text == "" {
		bl.problem(key, CodeMissing, "a value is required; set %s", bl.spec.Env[key])
	}
	return text
}

// file reads the file a _file key names, trailing whitespace trimmed. An unset key gives an
// empty Secret; an unreadable or empty file is a problem.
func (bl *bindingLoader) file(key, what string) Secret {
	path := bl.optional(key)
	if path == "" {
		return Secret{}
	}
	data, err := bl.l.in.ReadFile(path)
	if err != nil {
		bl.problem(key, CodeInvalid, "the %s file %s cannot be read", what, path)
		return Secret{}
	}
	s := strings.TrimRight(string(data), " \t\r\n")
	if s == "" {
		bl.problem(key, CodeMissing, "the %s file %s is empty", what, path)
		return Secret{}
	}
	return NewSecret(s)
}

// requiredFile is file for a key that must be set.
func (bl *bindingLoader) requiredFile(key, what string) Secret {
	if bl.optional(key) == "" {
		bl.problem(key, CodeMissing, "a value is required; set %s to the %s file", bl.spec.Env[key], what)
		return Secret{}
	}
	return bl.file(key, what)
}

// roots reads the certificates in the file ca_file names. Unset, the binding trusts MP_CA_FILE
// in TLS mode and nothing beyond the system roots in plain mode (nil).
func (bl *bindingLoader) roots() *x509.CertPool {
	path := bl.optional("ca_file")
	if path == "" {
		return bl.l.res.Bootstrap.Roots
	}
	data, err := bl.l.in.ReadFile(path)
	if err != nil {
		bl.problem("ca_file", CodeInvalid, "the CA file %s cannot be read", path)
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		bl.problem("ca_file", CodeInvalid, "the CA file %s holds no PEM certificate", path)
		return nil
	}
	return pool
}

func (bl *bindingLoader) sslMode() string {
	switch mode := bl.optional("sslmode"); mode {
	case "":
		return SSLModeDisable
	case SSLModeDisable, SSLModeVerifyFull:
		return mode
	default:
		bl.problem("sslmode", CodeInvalidBinding, "%q is not %s or %s", mode, SSLModeDisable, SSLModeVerifyFull)
		return ""
	}
}

// key returns the text of a plain key at the highest level that sets it, or "".
func (l *loader) key(env string) string {
	if env == "" {
		return ""
	}
	for _, lv := range l.levels {
		if v, _, ok := lv.lookup(env); ok {
			return v
		}
	}
	return ""
}
