package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
)

type tmModule = sdk.Module[testmodule.Config, testmodule.Uses]

// file writes content to a new file and returns its path.
func file(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// consumerToken is the binding token of the consumer instance "caller" in baseEnv's clients dir.
const consumerToken = "caller-token-0123456789abcdef0123456789"

// baseEnv is the least a start of the test module needs: its instance, its secret and a clients
// directory with one consumer.
func baseEnv(t *testing.T) map[string]string {
	return map[string]string{
		"MP_INSTANCE":     "tm",
		"TM_API_KEY_FILE": file(t, "key-from-file\n"),
		"TM_CLIENTS_DIR":  clientsDir(t, map[string]string{"caller": consumerToken}),
	}
}

// clientsDir writes one token file per consumer into a new directory and returns its path.
func clientsDir(t *testing.T, tokens map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, token := range tokens {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// start builds the test module with env as its whole process env and returns its config, its
// logs and Build's error.
func start(t *testing.T, env map[string]string) (testmodule.Config, *bytes.Buffer, error) {
	t.Helper()
	var (
		logs bytes.Buffer
		cfg  testmodule.Config
	)
	_, err := sdk.Build(t.Context(), testmodule.Module, func(_ context.Context, m *tmModule) (http.Handler, error) {
		cfg = m.Config
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return cfg, &logs, err
}

// eventually fails the test unless cond holds within 30 seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type logLine struct {
	Level    string
	Msg      string
	Host     string            `json:"host"`
	TLS      string            `json:"tls"`
	Values   map[string]string `json:"values"`
	Bindings map[string]string `json:"bindings"`
}

func logLines(t *testing.T, logs *bytes.Buffer) []logLine {
	t.Helper()
	var lines []logLine
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var l logLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		lines = append(lines, l)
	}
	return lines
}

func loadedLine(t *testing.T, logs *bytes.Buffer) logLine {
	t.Helper()
	var found []logLine
	for _, l := range logLines(t, logs) {
		if l.Msg == "config loaded" {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d config loaded lines, want 1:\n%s", len(found), logs)
	}
	return found[0]
}

func errorMsgs(t *testing.T, logs *bytes.Buffer) []string {
	t.Helper()
	var msgs []string
	for _, l := range logLines(t, logs) {
		if l.Level == "ERROR" {
			msgs = append(msgs, l.Msg)
		}
	}
	return msgs
}

// typeCase is one value of each type in the core set, read through its generated getter.
type typeCase struct {
	name, env, text string
	def             any // nil: no default (the secret)
	want            any
	get             func(context.Context, testmodule.Config) (any, error)
}

var typeCases = []typeCase{
	{
		name: "greeting", env: "TM_GREETING", text: "howdy", def: "hello", want: "howdy",
		get: func(ctx context.Context, c testmodule.Config) (any, error) { return c.Greeting(ctx) },
	},
	{
		name: "count", env: "TM_COUNT", text: "42", def: 5, want: 42,
		get: func(ctx context.Context, c testmodule.Config) (any, error) { return c.Count(ctx) },
	},
	{
		name: "verbose", env: "TM_VERBOSE", text: "true", def: false, want: true,
		get: func(ctx context.Context, c testmodule.Config) (any, error) { return c.Verbose(ctx) },
	},
	{
		name: "timeout", env: "TM_TIMEOUT", text: "90s", def: 10 * time.Second, want: 90 * time.Second,
		get: func(ctx context.Context, c testmodule.Config) (any, error) { return c.Timeout(ctx) },
	},
	{
		name: "upstream", env: "TM_UPSTREAM", text: "https://other.test/x", def: "http://upstream.test:8080", want: "https://other.test/x",
		get: func(ctx context.Context, c testmodule.Config) (any, error) {
			u, err := c.Upstream(ctx)
			if err != nil {
				return nil, err
			}
			return u.String(), nil
		},
	},
	{
		name: "level", env: "TM_LEVEL", text: "high", def: testmodule.LevelLow, want: testmodule.LevelHigh,
		get: func(ctx context.Context, c testmodule.Config) (any, error) { return c.Level(ctx) },
	},
	{
		name: "api-key", env: "TM_API_KEY", text: "key-from-test", want: "key-from-test",
		get: func(ctx context.Context, c testmodule.Config) (any, error) {
			s, err := c.ApiKey(ctx)
			return s.Reveal(), err
		},
	},
}

// TestSources covers AC-2 and the source labels of AC-5: every type in the core set from its env
// var, from the file its _FILE form names, from an env file, and from its default.
func TestSources(t *testing.T) {
	t.Parallel()
	for _, tc := range typeCases {
		secret := tc.def == nil
		for _, source := range []string{"env", "file", "env-file", "env-file _FILE", "default"} {
			if secret && (source == "env" || source == "default") {
				continue // a secret is never a plain env var (TestSecretInEnv) and has no default
			}
			t.Run(tc.name+" from "+source, func(t *testing.T) {
				t.Parallel()
				env := baseEnv(t)
				if secret {
					delete(env, "TM_API_KEY_FILE")
				}
				var wantSource string
				want := tc.want
				switch source {
				case "env":
					env[tc.env] = tc.text
					wantSource = "env"
				case "file":
					path := file(t, tc.text+" \n")
					env[tc.env+"_FILE"] = path
					wantSource = "file:" + path
				case "env-file":
					path := file(t, "# set by the test\n"+tc.env+"="+tc.text+"\n")
					env["MP_ENV_FILE"] = path
					wantSource = "env-file:" + path + ":2"
				case "env-file _FILE":
					valuePath := file(t, tc.text)
					path := file(t, tc.env+"_FILE="+valuePath+"\n")
					env["MP_ENV_FILE"] = path
					wantSource = "file:" + valuePath
				case "default":
					want = tc.def
					wantSource = "default"
				}
				cfg, logs, err := start(t, env)
				if err != nil {
					t.Fatalf("start refused: %v\n%s", err, logs)
				}
				got, err := tc.get(t.Context(), cfg)
				if err != nil || got != want {
					t.Errorf("getter = %v, %v; want %v", got, err, want)
				}
				if src := loadedLine(t, logs).Values[tc.name]; src != wantSource {
					t.Errorf("source = %q, want %q", src, wantSource)
				}
			})
		}
	}
}

// TestPrecedence covers the order of spec 0005, "Sources and precedence": the process env, then
// the env files with a later one first, then the default; <ENV> and <ENV>_FILE share one slot per
// level, and an empty value counts as unset.
func TestPrecedence(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env   func(t *testing.T) map[string]string
		want  string
		label string // the source label, with the env file paths as $1 and $2
	}{
		"process env beats an env file": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_GREETING": "abc", "MP_ENV_FILE": file(t, "TM_GREETING=xyz\n")}
			},
			want: "abc", label: "env",
		},
		"a later env file beats an earlier one": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_GREETING=abc\n") + ":" + file(t, "TM_GREETING=xyz\n")}
			},
			want: "xyz", label: "env-file:$2:1",
		},
		"an earlier env file fills what a later one leaves out": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_GREETING=abc\n") + ":" + file(t, "TM_COUNT=7\n")}
			},
			want: "abc", label: "env-file:$1:1",
		},
		"process env _FILE beats an env file's plain line": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_GREETING_FILE": file(t, "abc"), "MP_ENV_FILE": file(t, "TM_GREETING=xyz\n")}
			},
			want: "abc",
		},
		"a later env file's _FILE beats an earlier plain line": {
			env: func(t *testing.T) map[string]string {
				value := file(t, "xyz\n")
				return map[string]string{"MP_ENV_FILE": file(t, "TM_GREETING=abc\n") + ":" + file(t, "TM_GREETING_FILE="+value+"\n")}
			},
			want: "xyz",
		},
		"an empty env var counts as unset": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_GREETING": "", "MP_ENV_FILE": file(t, "TM_GREETING=xyz\n")}
			},
			want: "xyz", label: "env-file:$1:1",
		},
		"an empty env file line counts as unset": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_GREETING=\n")}
			},
			want: "hello", label: "default",
		},
		"an empty _FILE file counts as unset": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_GREETING_FILE": file(t, " \n")}
			},
			want: "hello", label: "default",
		},
		"quotes around an env file value are removed": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, `TM_GREETING="abc"`+"\n")}
			},
			want: "abc", label: "env-file:$1:1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := baseEnv(t)
			maps.Copy(env, tc.env(t))
			cfg, logs, err := start(t, env)
			if err != nil {
				t.Fatalf("start refused: %v\n%s", err, logs)
			}
			if got, err := cfg.Greeting(t.Context()); err != nil || got != tc.want {
				t.Errorf("Greeting = %q, %v; want %q", got, err, tc.want)
			}
			if tc.label != "" {
				files := strings.Split(env["MP_ENV_FILE"], ":")
				label := strings.ReplaceAll(tc.label, "$1", files[0])
				if len(files) > 1 {
					label = strings.ReplaceAll(label, "$2", files[1])
				}
				if got := loadedLine(t, logs).Values["greeting"]; got != label {
					t.Errorf("source = %q, want %q", got, label)
				}
			}
		})
	}
}

// TestOptionalValue covers the (T, bool, error) getter of an optional value without a default.
func TestOptionalValue(t *testing.T) {
	t.Parallel()
	cfg, _, err := start(t, baseEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, err := cfg.Note(t.Context()); ok || err != nil || v != "" {
		t.Errorf("unset Note = %q, %v, %v", v, ok, err)
	}
	env := baseEnv(t)
	env["TM_NOTE"] = "hi"
	if cfg, _, err = start(t, env); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := cfg.Note(t.Context()); !ok || err != nil || v != "hi" {
		t.Errorf("set Note = %q, %v, %v", v, ok, err)
	}
}

// TestManyProblems covers AC-3: six bad values give six lines, one per value, sorted by value
// name, then file, then line, and Main exits 1 without running setup, so nothing listens.
func TestManyProblems(t *testing.T) {
	t.Parallel()
	envFile := file(t, "TM_NOTE=fine\nnot a line\n")
	env := map[string]string{
		"MP_INSTANCE":     "tm",
		"MP_ENV_FILE":     envFile,
		"TM_COUNT":        "0",
		"TM_GREETING":     "Hello",
		"TM_LEVEL":        "medium",
		"TM_TIMEOUT":      "5s",
		"TM_TIMEOUT_FILE": file(t, "5s"),
	}
	var logs bytes.Buffer
	setupRan := false
	code := sdk.Main(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
		setupRan = true
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if code != 1 || setupRan {
		t.Fatalf("Main = %d, setup ran %v; want 1 and no setup", code, setupRan)
	}
	want := []string{
		"config: MP_ENV_FILE (MP_ENV_FILE): env_file: " + envFile + ":2: ",
		"config: api-key (TM_API_KEY): missing: ",
		"config: count (TM_COUNT): out_of_range: ",
		"config: greeting (TM_GREETING): no_match: ",
		"config: level (TM_LEVEL): not_allowed: ",
		"config: timeout (TM_TIMEOUT): ambiguous_source: ",
	}
	got := errorMsgs(t, &logs)
	if len(got) != len(want) {
		t.Fatalf("error lines =\n%s\nwant %d", strings.Join(got, "\n"), len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("line %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
	if slices.ContainsFunc(logLines(t, &logs), func(l logLine) bool { return l.Msg == "config loaded" }) {
		t.Error("a refused start logged config loaded")
	}
}

// TestStartProblems covers the source and env file rules that refuse a start, one line each.
func TestStartProblems(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env  func(t *testing.T) map[string]string
		want string
	}{
		"no instance": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_INSTANCE": ""} },
			want: "config: MP_INSTANCE (MP_INSTANCE): missing: ",
		},
		"bad instance": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_INSTANCE": "Bad_Name"} },
			want: "config: MP_INSTANCE (MP_INSTANCE): invalid: ",
		},
		"an uppercase domain": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_DOMAIN": "Graph.internal"} },
			want: "config: MP_DOMAIN (MP_DOMAIN): invalid: ",
		},
		"a domain with an empty label": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_DOMAIN": "graph..internal"} },
			want: "config: MP_DOMAIN (MP_DOMAIN): invalid: ",
		},
		"a missing env file": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_ENV_FILE": "/no/such/file"} },
			want: "config: MP_ENV_FILE (MP_ENV_FILE): env_file: /no/such/file does not exist",
		},
		"an unknown env file key": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_OTHER=1\n")}
			},
			want: "config: TM_OTHER (TM_OTHER): unknown_key: ",
		},
		"a key twice in one env file": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_COUNT=1\nTM_COUNT=2\n")}
			},
			want: "config: MP_ENV_FILE (MP_ENV_FILE): env_file: ",
		},
		"both forms in one env file": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MP_ENV_FILE": file(t, "TM_COUNT=1\nTM_COUNT_FILE=/x\n")}
			},
			want: "config: count (TM_COUNT): ambiguous_source: ",
		},
		"a source rule before a type rule": {
			env: func(t *testing.T) map[string]string {
				return map[string]string{"TM_COUNT": "many", "TM_COUNT_FILE": file(t, "7")}
			},
			want: "config: count (TM_COUNT): ambiguous_source: ",
		},
		"a _FILE that cannot be read": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_COUNT_FILE": "/no/such/file"} },
			want: "config: count (TM_COUNT): invalid: ",
		},
		"not a number": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_COUNT": "many"} },
			want: "config: count (TM_COUNT): invalid: ",
		},
		"above max": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_COUNT": "101"} },
			want: "config: count (TM_COUNT): out_of_range: ",
		},
		"not a bool": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_VERBOSE": "yes"} },
			want: "config: verbose (TM_VERBOSE): invalid: ",
		},
		"a duration below min": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_TIMEOUT": "10ms"} },
			want: "config: timeout (TM_TIMEOUT): out_of_range: ",
		},
		"a relative url": {
			env:  func(*testing.T) map[string]string { return map[string]string{"TM_UPSTREAM": "/just/a/path"} },
			want: "config: upstream (TM_UPSTREAM): invalid: ",
		},
		"only one TLS setting": {
			env:  func(*testing.T) map[string]string { return map[string]string{"MP_CA_FILE": "/ca.pem"} },
			want: "config: MP_ACME_URL (MP_ACME_URL): tls_config: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := baseEnv(t)
			maps.Copy(env, tc.env(t))
			_, logs, err := start(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			got := errorMsgs(t, logs)
			if len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", got, tc.want)
			}
		})
	}
}

// TestSecretInEnv covers the first half of AC-4: a secret as a plain process env var stops the
// start, and the line does not repeat it.
func TestSecretInEnv(t *testing.T) {
	t.Parallel()
	env := baseEnv(t)
	delete(env, "TM_API_KEY_FILE")
	env["TM_API_KEY"] = "hunter2-plain"
	_, logs, err := start(t, env)
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("err = %v, want sdk.ErrStart", err)
	}
	got := errorMsgs(t, logs)
	if len(got) != 1 || !strings.HasPrefix(got[0], "config: api-key (TM_API_KEY): secret_in_env: ") {
		t.Errorf("error lines = %q", got)
	}
	if strings.Contains(logs.String(), "hunter2") {
		t.Error("the secret reached the log")
	}
}

// TestSecretNeverShows covers the second half of AC-4: a secret's value is in no log line, no fmt
// output and no slog output of the generated Config or of sdk.Secret; a secret on a plain env file
// line starts with one warning that names the line, not the value.
func TestSecretNeverShows(t *testing.T) {
	t.Parallel()
	const value = "hunter2-in-file"
	env := baseEnv(t)
	delete(env, "TM_API_KEY_FILE")
	env["MP_ENV_FILE"] = file(t, "TM_API_KEY="+value+"\n")
	cfg, logs, err := start(t, env)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	secret, err := cfg.ApiKey(t.Context())
	if err != nil || secret.Reveal() != value {
		t.Fatalf("ApiKey = %v, %v", secret.Reveal(), err)
	}
	warned := 0
	for _, l := range logLines(t, logs) {
		if l.Level == "WARN" && l.Msg == "secret on a plain env file line" {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("%d secret warnings, want 1:\n%s", warned, logs)
	}

	var out bytes.Buffer
	for _, verb := range []string{"%v", "%s", "%+v", "%#v", "%q", "%x"} {
		fmt.Fprintf(&out, verb+"\n"+verb+"\n", cfg, secret)
	}
	fmt.Fprintln(&out, cfg, secret, &secret, []sdk.Secret{secret})
	for _, h := range []slog.Handler{slog.NewJSONHandler(&out, nil), slog.NewTextHandler(&out, nil)} {
		slog.New(h).Info("dump", slog.Any("config", cfg), slog.Any("secret", secret), "plain", secret) //nolint:sloglint // both forms must redact the secret
	}
	data, _ := json.Marshal(map[string]any{"secret": secret})
	out.Write(data)

	for name, text := range map[string]string{"log": logs.String(), "fmt and slog": out.String()} {
		if strings.Contains(text, value) || strings.Contains(text, fmt.Sprintf("%x", value)) {
			t.Errorf("the secret shows in %s:\n%s", name, text)
		}
	}
}

// TestStartLog covers AC-5: one config loaded line with each value's source and no value, the
// TLS mode and each binding's client kind, and the plain HTTP warning.
func TestStartLog(t *testing.T) {
	t.Parallel()
	envFile := file(t, "TM_COUNT=42\n")
	env := baseEnv(t)
	env["TM_GREETING"] = "howdy"
	env["MP_ENV_FILE"] = envFile
	env["MP_DOMAIN"] = "sdk-test.internal"
	_, logs, err := start(t, env)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	l := loadedLine(t, logs)
	if l.Level != "INFO" || l.TLS != "plain" || l.Host != "tm.sdk-test.internal" {
		t.Errorf("level %s, tls %q, host %q", l.Level, l.TLS, l.Host)
	}
	wantValues := map[string]string{
		"api-key":  "file:" + env["TM_API_KEY_FILE"],
		"count":    "env-file:" + envFile + ":1",
		"greeting": "env",
		"level":    "default",
		"note":     "unset",
		"timeout":  "default",
		"upstream": "default",
		"verbose":  "default",
	}
	if fmt.Sprint(l.Values) != fmt.Sprint(wantValues) {
		t.Errorf("values = %v, want %v", l.Values, wantValues)
	}
	if fmt.Sprint(l.Bindings) != fmt.Sprint(map[string]string{"echo": "http", "postgresql": "postgresql", "realm": "env", "storage": "s3"}) {
		t.Errorf("bindings = %v", l.Bindings)
	}
	// timestamps and the temp files the source labels name may hold any digits
	text := regexp.MustCompile(`"time":"[^"]*"`).ReplaceAllString(logs.String(), "")
	for _, path := range []string{envFile, env["TM_API_KEY_FILE"], env["TM_CLIENTS_DIR"]} {
		text = strings.ReplaceAll(text, path, "")
	}
	for _, v := range []string{"howdy", "42", "key-from-file", "upstream.test"} {
		if strings.Contains(text, v) {
			t.Errorf("the value %q reached the log", v)
		}
	}
	if !strings.Contains(logs.String(), "serving plain HTTP: no certificate issuer bound") {
		t.Error("no plain HTTP warning")
	}
}

// checked is a Config with a Validate method, which counts its runs and refuses every start.
type checked struct{ runs *atomic.Int64 }

func (c checked) Validate(context.Context) error {
	c.runs.Add(1)
	return errors.New("limit must be even")
}

// TestValidate covers the module's optional Validate: its error is the one line
// config: module (module): validate: <error>, and it runs only once every value loaded without a
// problem.
func TestValidate(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		limit string
		runs  int64
		want  string
	}{
		"its error refuses the start": {limit: "3", runs: 1, want: "config: module (module): validate: limit must be even"},
		"not run after a problem":     {limit: "many", runs: 0, want: "config: limit (V_LIMIT): invalid: "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var runs atomic.Int64
			d := sdk.Declaration[checked, struct{}]{
				Name:   "validated",
				Values: []sdk.ValueSpec{{Name: "limit", Env: "V_LIMIT", Type: "int", Description: "A limit."}},
				New:    func(*sdk.Values, *sdk.Bindings) (checked, struct{}) { return checked{&runs}, struct{}{} },
			}
			var logs bytes.Buffer
			_, err := sdk.Build(t.Context(), d, func(context.Context, *sdk.Module[checked, struct{}]) (http.Handler, error) {
				return http.NotFoundHandler(), nil
			}, sdk.WithEnv(map[string]string{"MP_INSTANCE": "validated", "V_LIMIT": tc.limit}),
				sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			if got := errorMsgs(t, &logs); len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", got, tc.want)
			}
			if runs.Load() != tc.runs {
				t.Errorf("Validate ran %d times, want %d", runs.Load(), tc.runs)
			}
		})
	}
}

// TestSetupError covers a setup that fails: Build logs it once, unless setup wrapped ErrStart
// after logging its own reasons.
func TestSetupError(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		want []string
	}{
		"plain error":    {err: errors.New("boom"), want: []string{"module setup failed"}},
		"wraps ErrStart": {err: fmt.Errorf("logged already: %w", sdk.ErrStart), want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			_, err := sdk.Build(t.Context(), testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
				return nil, tc.err
			}, sdk.WithEnv(baseEnv(t)), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			if got := errorMsgs(t, &logs); !slices.Equal(got, tc.want) {
				t.Errorf("error lines = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAddReady covers a module's own readiness check: /readyz waits for it beside the bindings.
func TestAddReady(t *testing.T) {
	t.Parallel()
	var discovered atomic.Bool
	h, err := sdk.Build(t.Context(), testmodule.Module, func(_ context.Context, m *tmModule) (http.Handler, error) {
		m.AddReady(discovered.Load)
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(baseEnv(t)), sdk.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	status := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
		return rec.Code
	}
	if got := status(); got != http.StatusServiceUnavailable {
		t.Errorf("before the check passes: /readyz %d, want 503", got)
	}
	discovered.Store(true)
	eventually(t, "/readyz 200 once the check passes", func() bool { return status() == http.StatusOK })
}
