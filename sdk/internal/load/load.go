// Package load fills a module's declared values at start (spec 0005, "Sources and precedence"):
// bootstrap names from the process env only; a value from its env var or the file its _FILE form
// names, else from the env files in MP_ENV_FILE (a later file overriding an earlier one), else
// from its default. It parses each value by type, checks its rules and reports every problem in
// one pass, as lines that never carry a value.
package load

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strings"

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
	CodeValidate        = "validate"
)

// Bootstrap names (manifest.BootstrapNames).
const (
	EnvInstance         = "MP_INSTANCE"
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

// BindingSpec declares one capability the module uses.
type BindingSpec struct {
	Capability string
	Client     string // empty: env mapping only
	Optional   bool
	Env        map[string]string // binding key to env var name
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
}

// Bootstrap holds the bootstrap names.
type Bootstrap struct {
	Instance          string
	ACMEURL, CAFile   string
	CallerKeyFile     string
	CallerSigningFile string
}

// Binding is a loaded binding with a client, in capability order.
type Binding struct {
	Capability string
	Client     string
	Optional   bool
	Bound      bool     // false only for an optional binding whose url is unset
	URL        *url.URL // the provider's address
	Token      Secret   // the content of token_file; empty when token_file is unset
}

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

// Load reads every declared value and returns the result.
func Load(in Input) Result {
	l := loader{in: in, res: Result{Values: map[string]any{}, Sources: map[string]string{}}}
	l.bootstrap()
	l.envFiles()
	for _, v := range in.Values {
		l.value(v)
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
	b.ACMEURL, b.CAFile = l.env(EnvACMEURL), l.env(EnvCAFile)
	switch {
	case b.ACMEURL != "" && b.CAFile == "":
		l.problem(EnvCAFile, EnvCAFile, CodeTLSConfig, "%s is set, so %s must be set too", EnvACMEURL, EnvCAFile)
	case b.ACMEURL == "" && b.CAFile != "":
		l.problem(EnvACMEURL, EnvACMEURL, CodeTLSConfig, "%s is set, so %s must be set too", EnvCAFile, EnvACMEURL)
	case b.ACMEURL != "":
		l.problem(EnvACMEURL, EnvACMEURL, CodeTLSConfig, "TLS mode is not available in this sdk version yet; unset %s and %s", EnvACMEURL, EnvCAFile)
	}
	b.CallerKeyFile, b.CallerSigningFile = l.env(EnvCallerKeyFile), l.env(EnvCallerSigningKey)

	l.levels = append(l.levels, level{lookup: func(key string) (string, int, bool) {
		v, ok := l.in.LookupEnv(key)
		return v, 0, ok && v != ""
	}})
}

// envFiles reads the files MP_ENV_FILE lists and adds them as levels, the last file first.
func (l *loader) envFiles() {
	known := l.knownKeys()
	var files []level
	for _, path := range strings.Split(l.env(EnvEnvFile), ":") {
		if path == "" {
			continue
		}
		data, err := l.in.ReadFile(path)
		if err != nil {
			reason := "cannot be read"
			if errors.Is(err, fs.ErrNotExist) {
				reason = "does not exist"
			}
			l.res.Problems = append(l.res.Problems, Problem{Value: EnvEnvFile, Env: EnvEnvFile, Code: CodeEnvFile,
				Message: fmt.Sprintf("%s %s", path, reason), File: path})
			continue
		}
		lines, errs := envfile.Parse(data)
		for _, e := range errs {
			l.res.Problems = append(l.res.Problems, Problem{Value: EnvEnvFile, Env: EnvEnvFile, Code: CodeEnvFile,
				Message: fmt.Sprintf("%s:%d: %s", path, e.Line, e.Message), File: path, Line: e.Line})
		}
		set := map[string]envfile.Line{}
		for _, line := range lines {
			if !known[line.Key] {
				l.res.Problems = append(l.res.Problems, Problem{Value: line.Key, Env: line.Key, Code: CodeUnknownKey,
					Message: fmt.Sprintf("%s:%d: %s is not a declared value, a binding key or a bootstrap name", path, line.Line, line.Key),
					File:    path, Line: line.Line})
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
	parsed, err := v.rules().Parse(text)
	if err != nil {
		re := err.(*manifest.RuleError)
		l.problem(v.Name, v.Env, re.Code, "%s", re.Message)
		return
	}
	if v.Type == manifest.TypeSecret {
		parsed = NewSecret(parsed.(string))
	}
	l.res.Values[v.Name] = parsed
	l.res.Sources[v.Name] = source
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

// binding loads the keys of a binding with a client: its url, and the token its token_file
// names. Keys are plain env style keys, found at the highest level that sets them.
func (l *loader) binding(b BindingSpec) {
	if b.Client == "" {
		return // env mapping only: the module reads nothing through the sdk
	}
	path := "uses." + b.Capability
	if b.Client != manifest.ClientHTTP {
		l.problem(path+".client", path+".client", CodeInvalidBinding, "the %s client is not available in this sdk version yet", b.Client)
		return
	}
	urlEnv := b.Env["url"]
	text := l.key(urlEnv)
	if text == "" {
		if b.Optional {
			l.res.Bindings = append(l.res.Bindings, Binding{Capability: b.Capability, Client: b.Client, Optional: true})
			return
		}
		l.problem(path+".url", urlEnv, CodeMissing, "the provider's address is required; set %s", urlEnv)
		return
	}
	u, err := url.Parse(text)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		l.problem(path+".url", urlEnv, CodeInvalid, "must be an absolute http or https URL with a host")
		return
	}
	bound := Binding{Capability: b.Capability, Client: b.Client, Optional: b.Optional, Bound: true, URL: u}
	if env, ok := b.Env["token_file"]; ok {
		if file := l.key(env); file != "" {
			data, err := l.in.ReadFile(file)
			if err != nil {
				l.problem(path+".token_file", env, CodeInvalid, "the token file %s cannot be read", file)
				return
			}
			token := strings.TrimRight(string(data), " \t\r\n")
			if token == "" {
				l.problem(path+".token_file", env, CodeMissing, "the token file %s is empty", file)
				return
			}
			bound.Token = NewSecret(token)
		}
	}
	l.res.Bindings = append(l.res.Bindings, bound)
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
