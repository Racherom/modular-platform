package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpclient"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
	"github.com/racherom/modular-platform/sdk/sdktest"
)

// valueNames are the test module's values in name order.
var valueNames = []string{"api-key", "count", "greeting", "level", "note", "timeout", "upstream", "verbose"}

// sourceMarker is in every error a fake source returns; it must never reach a log line.
const sourceMarker = "MARKER-source-s3cr3t"

// built is a test module started with sources: its config, its bindings and its logs.
type built struct {
	cfg     testmodule.Config
	uses    testmodule.Uses
	handler http.Handler
	logs    *syncBuffer
}

// buildWith builds the test module with env as its whole process env and opts added. Its handler
// answers 503 when a getter's caller source cannot answer.
func buildWith(t *testing.T, env map[string]string, opts ...sdk.Option) (*built, error) {
	t.Helper()
	b := &built{logs: &syncBuffer{}}
	opts = append([]sdk.Option{sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(b.logs, nil)))}, opts...)
	h, err := sdk.Build(t.Context(), testmodule.Module, func(_ context.Context, m *tmModule) (http.Handler, error) {
		b.cfg, b.uses = m.Config, m.Uses
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := sdk.WithCaller(r.Context(), sdk.Caller{Issuer: "api", User: r.URL.Query().Get("user")})
			g, err := m.Config.Greeting(ctx)
			switch {
			case errors.Is(err, sdk.ErrCallerSource):
				httpkit.WriteProblem(w, http.StatusServiceUnavailable, "unavailable", "try again")
			case err != nil:
				httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "bad value")
			default:
				_, _ = w.Write([]byte(g))
			}
		}), nil
	}, opts...)
	b.handler = h
	return b, err
}

func (b *built) lines(t *testing.T) []logLine {
	return logLines(t, bytes.NewBufferString(b.logs.String()))
}

func (b *built) errors(t *testing.T) []string {
	return errorMsgs(t, bytes.NewBufferString(b.logs.String()))
}

// loaded returns every config loaded line.
func (b *built) loaded(t *testing.T) []logLine {
	var found []logLine
	for _, l := range b.lines(t) {
		if l.Msg == "config loaded" {
			found = append(found, l)
		}
	}
	return found
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// TestStartSource covers the start source of AC-10: its answer beats the process env, a _FILE
// value, the env files and the default, and shows as source; it hides an ambiguous_source and a
// secret_in_env below it; not found and an empty answer fall through; every declared value is
// asked, in name order.
func TestStartSource(t *testing.T) {
	t.Parallel()
	envFile := sdktest.EnvFile(t, "TM_LEVEL=high", "TM_TIMEOUT=20s", "TM_VERBOSE=false")
	src := &sdktest.Source{Answers: map[string]string{
		"api-key":  "key-from-source",
		"count":    "9",
		"greeting": "source",
		"level":    "", // empty: falls through to the env file
		"verbose":  "true",
	}}
	b, err := buildWith(t, map[string]string{
		"MP_INSTANCE":    "tm",
		"MP_ENV_FILE":    envFile,
		"TM_CLIENTS_DIR": sdktest.ClientsDir(t, map[string]string{"caller": consumerToken}),
		"TM_API_KEY":     "plain-in-env", // secret_in_env without the source
		"TM_COUNT":       "7",
		"TM_COUNT_FILE":  file(t, "8"), // ambiguous_source without the source
		"TM_GREETING":    "env",
		"TM_NOTE":        "fromenv",
	}, sdk.WithSource(src))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, b.logs)
	}
	ctx := t.Context()
	if key := must[sdk.Secret](t)(b.cfg.ApiKey(ctx)); key.Reveal() != "key-from-source" {
		t.Errorf("api-key = %q, want key-from-source", key.Reveal())
	}
	if n := must[int](t)(b.cfg.Count(ctx)); n != 9 {
		t.Errorf("count = %d, want 9", n)
	}
	if g := must[string](t)(b.cfg.Greeting(ctx)); g != "source" {
		t.Errorf("greeting = %q, want source", g)
	}
	if l := must[testmodule.Level](t)(b.cfg.Level(ctx)); l != testmodule.LevelHigh {
		t.Errorf("level = %q, want high", l)
	}
	if v := must[bool](t)(b.cfg.Verbose(ctx)); !v {
		t.Error("verbose = false, want true from the source")
	}
	if got := src.Asked(); !slices.Equal(got, valueNames) {
		t.Errorf("asked %v, want %v", got, valueNames)
	}
	lines := b.loaded(t)
	if len(lines) != 1 {
		t.Fatalf("%d config loaded lines, want 1:\n%s", len(lines), b.logs)
	}
	want := map[string]string{
		"api-key": "source", "count": "source", "greeting": "source", "level": "env-file:" + envFile + ":1",
		"note": "env", "timeout": "env-file:" + envFile + ":2", "upstream": "default", "verbose": "source",
	}
	if got := lines[0].Values; !mapsEqual(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
	if strings.Contains(b.logs.String(), "key-from-source") {
		t.Error("the secret from the source shows in the logs")
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestStartSourceProblems covers the start source's problems (AC-10): a failing call is one
// source_error line with the fixed text for that value only, never the error's own text; a bad
// answer is checked like any value; once the 10 second deadline passes, no further value is
// asked and the value in flight and each after it get one line; and it cannot be combined with a
// push source.
func TestStartSourceProblems(t *testing.T) {
	t.Parallel()
	failed := func(v, env string) string {
		return "config: " + v + " (" + env + "): source_error: the start source failed"
	}
	late := func(v, env string) string {
		return "config: " + v + " (" + env + "): source_error: the start source missed the 10 second deadline"
	}
	tests := []struct {
		name      string
		src       *sdktest.Source
		push      bool
		want      []string
		wantAsked []string
	}{
		{
			name: "a failing call",
			src: &sdktest.Source{
				Answers: map[string]string{"greeting": "fine"},
				Errs:    map[string]error{"count": errors.New("lookup failed: " + sourceMarker)},
			},
			want:      []string{failed("count", "TM_COUNT")},
			wantAsked: valueNames,
		},
		{
			name:      "a bad answer",
			src:       &sdktest.Source{Answers: map[string]string{"count": "0", "level": "medium"}},
			want:      []string{"config: count (TM_COUNT): out_of_range: 0 is below the minimum 1", `config: level (TM_LEVEL): not_allowed: "medium" is not one of low, high`},
			wantAsked: valueNames,
		},
		{
			name: "the deadline",
			src:  &sdktest.Source{Slow: map[string]time.Duration{"count": 2 * time.Second}},
			want: []string{
				late("count", "TM_COUNT"), late("greeting", "TM_GREETING"), late("level", "TM_LEVEL"), late("note", "TM_NOTE"),
				late("timeout", "TM_TIMEOUT"), late("upstream", "TM_UPSTREAM"), late("verbose", "TM_VERBOSE"),
			},
			wantAsked: []string{"api-key", "count"},
		},
		{
			name:      "with a push source",
			src:       &sdktest.Source{},
			push:      true,
			want:      []string{"config: module (module): source_error: WithSource and WithPushSource cannot be combined"},
			wantAsked: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := []sdk.Option{sdk.WithSource(tc.src), sdk.WithSourceTimes(200*time.Millisecond, time.Second, time.Second, time.Second)}
			if tc.push {
				opts = append(opts, sdk.WithPushSource(sdktest.NewPush()))
			}
			began := time.Now()
			b, err := buildWith(t, baseEnv(t), opts...)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want ErrStart", err)
			}
			if took := time.Since(began); took > time.Second {
				t.Errorf("the start took %v; a hung source must hold it no longer than the deadline", took)
			}
			if got := b.errors(t); !slices.Equal(got, tc.want) {
				t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			if got := tc.src.Asked(); !slices.Equal(got, tc.wantAsked) {
				t.Errorf("asked %v, want %v", got, tc.wantAsked)
			}
			if strings.Contains(b.logs.String(), sourceMarker) {
				t.Error("the source's error text shows in the logs")
			}
		})
	}
}

// pushEnv is the least a start with a push source needs: no declared value in the env.
func pushEnv(t *testing.T) map[string]string {
	return map[string]string{
		"MP_INSTANCE":    "tm",
		"TM_CLIENTS_DIR": sdktest.ClientsDir(t, map[string]string{"caller": consumerToken}),
	}
}

// buildPushed builds the test module with push and sends first as the snapshot Build waits for.
func buildPushed(t *testing.T, push *sdktest.Push, first map[string]string, opts ...sdk.Option) *built {
	t.Helper()
	sent := make(chan error, 1)
	go func() { sent <- push.Send(t.Context(), first) }()
	b, err := buildWith(t, pushEnv(t), append(opts, sdk.WithPushSource(push))...)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, b.logs)
	}
	if err := <-sent; err != nil {
		t.Fatalf("first snapshot refused: %v\n%s", err, b.logs)
	}
	return b
}

// TestPushSnapshots covers the push source of AC-10: the first snapshot gives the values, and
// later ones replace them whole while the module runs; a snapshot with a bad value, a missing
// required value or an undeclared name (unknown_key) is refused whole and the previous one
// stays; config loaded is logged for each accepted snapshot, with push, default or unset.
func TestPushSnapshots(t *testing.T) {
	t.Parallel()
	push := sdktest.NewPush()
	b := buildPushed(t, push, map[string]string{"api-key": "pushed-key-1", "greeting": "pushed"})
	ctx := t.Context()
	if got := push.Values(); !slices.Equal(got, valueNames) {
		t.Errorf("Watch got %v, want %v", got, valueNames)
	}
	if g := must[string](t)(b.cfg.Greeting(ctx)); g != "pushed" {
		t.Errorf("greeting = %q, want pushed", g)
	}
	if n := must[int](t)(b.cfg.Count(ctx)); n != 5 {
		t.Errorf("count = %d, want the default 5", n)
	}
	loaded := b.loaded(t)
	if len(loaded) != 1 {
		t.Fatalf("%d config loaded lines, want 1:\n%s", len(loaded), b.logs)
	}
	if v := loaded[0].Values; v["api-key"] != "push" || v["greeting"] != "push" || v["count"] != "default" || v["note"] != "unset" {
		t.Errorf("sources = %v", v)
	}

	refused := []struct {
		snapshot map[string]string
		want     string
	}{
		{map[string]string{"api-key": "k", "count": "0"}, "config: count (TM_COUNT): out_of_range: 0 is below the minimum 1"},
		{map[string]string{"greeting": "x"}, "config: api-key (TM_API_KEY): missing: a value is required; the snapshot leaves it out"},
		{map[string]string{"api-key": "k", "plannr-rate": "3"}, "config: plannr-rate (plannr-rate): unknown_key: the snapshot names plannr-rate, which the catalog entry does not declare"},
	}
	for _, r := range refused {
		before := len(b.errors(t))
		if err := push.Send(ctx, r.snapshot); !errors.Is(err, sdk.ErrSnapshotRefused) {
			t.Errorf("Send(%v) = %v, want ErrSnapshotRefused", r.snapshot, err)
		}
		if got := b.errors(t)[before:]; !slices.Equal(got, []string{r.want}) {
			t.Errorf("Send(%v) logged %q, want %q", r.snapshot, got, r.want)
		}
		if g := must[string](t)(b.cfg.Greeting(ctx)); g != "pushed" {
			t.Errorf("after a refused snapshot greeting = %q, want pushed kept", g)
		}
	}

	if err := push.Send(ctx, map[string]string{"api-key": "pushed-key-2", "greeting": "second", "count": "7"}); err != nil {
		t.Fatal(err)
	}
	if g := must[string](t)(b.cfg.Greeting(ctx)); g != "second" {
		t.Errorf("greeting = %q, want second", g)
	}
	if n := must[int](t)(b.cfg.Count(ctx)); n != 7 {
		t.Errorf("count = %d, want 7", n)
	}
	if n := len(b.loaded(t)); n != 2 {
		t.Errorf("%d config loaded lines, want 2", n)
	}
	if strings.Contains(b.logs.String(), "pushed-key") {
		t.Error("a pushed secret shows in the logs")
	}
}

// TestPushManagedEnv covers managed_env (AC-10): with a push source, a declared value in the
// process env (either form) or on an env file line stops the start; an empty KEY= line does not.
func TestPushManagedEnv(t *testing.T) {
	t.Parallel()
	envFile := sdktest.EnvFile(t, "TM_NOTE=", "TM_LEVEL=high")
	env := pushEnv(t)
	env["MP_ENV_FILE"] = envFile
	env["TM_GREETING"] = "x"
	env["TM_COUNT_FILE"] = file(t, "3")
	push := sdktest.NewPush()
	b, err := buildWith(t, env, sdk.WithPushSource(push))
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("err = %v, want ErrStart", err)
	}
	want := []string{
		"config: count (TM_COUNT): managed_env: a push source supplies this value; remove TM_COUNT_FILE from the process env",
		"config: greeting (TM_GREETING): managed_env: a push source supplies this value; remove TM_GREETING from the process env",
		"config: level (TM_LEVEL): managed_env: a push source supplies this value; remove TM_LEVEL from " + envFile + ":2",
	}
	if got := b.errors(t); !slices.Equal(got, want) {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if push.Values() != nil {
		t.Error("the push source was watched on a refused start")
	}
}

// mainErrors returns the messages of the error lines in Main's logs, whose listening line carries
// tls as a bool.
func mainErrors(t *testing.T, logs string) []string {
	t.Helper()
	var msgs []string
	for line := range strings.SplitSeq(strings.TrimSpace(logs), "\n") {
		var l struct{ Level, Msg string }
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		if l.Level == "ERROR" {
			msgs = append(msgs, l.Msg)
		}
	}
	return msgs
}

// runMain runs Main in the background on free addresses and returns its address and exit code.
func runMain(t *testing.T, ctx context.Context, env map[string]string, logs *syncBuffer, opts ...sdk.Option) (string, <-chan int) {
	t.Helper()
	addr := freeAddr(t)
	opts = append([]sdk.Option{
		sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))), sdk.WithAddrs(addr, freeAddr(t)),
	}, opts...)
	code := make(chan int, 1)
	go func() {
		code <- sdk.Main(ctx, testmodule.Module, func(context.Context, *tmModule) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("up")) }), nil
		}, opts...)
	}()
	return "http://" + addr, code
}

func status(t *testing.T, target string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+consumerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestPushNoSnapshot covers the wait for the first snapshot (AC-10): the module listens with
// /readyz and every call at 503; a refused snapshot does not extend the wait; with no accepted
// snapshot in time it exits 1 with the no_snapshot line, after the refused snapshot's lines.
func TestPushNoSnapshot(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	push := sdktest.NewPush()
	base, code := runMain(t, t.Context(), pushEnv(t), &logs,
		sdk.WithPushSource(push), sdk.WithSourceTimes(time.Second, 1500*time.Millisecond, time.Second, time.Second))
	eventually(t, "the module listens", func() bool { return status(t, base+"/readyz") == http.StatusServiceUnavailable })
	if s := status(t, base+"/"); s != http.StatusServiceUnavailable {
		t.Errorf("a call before the first snapshot = %d, want 503", s)
	}
	if err := push.Send(t.Context(), map[string]string{"count": "0"}); !errors.Is(err, sdk.ErrSnapshotRefused) {
		t.Errorf("Send = %v, want ErrSnapshotRefused", err)
	}
	select {
	case c := <-code:
		if c != 1 {
			t.Errorf("exit code %d, want 1", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Main still runs without a snapshot")
	}
	msgs := mainErrors(t, logs.String())
	want := []string{
		"config: api-key (TM_API_KEY): missing: a value is required; the snapshot leaves it out",
		"config: count (TM_COUNT): out_of_range: 0 is below the minimum 1",
		"config: push (push): no_snapshot: no snapshot arrived within 30 seconds",
	}
	if !slices.Equal(msgs, want) {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(msgs, "\n"), strings.Join(want, "\n"))
	}
}

// TestPushReady covers readiness with a push source (AC-10): /readyz turns 200 and calls reach
// the module once the first snapshot is accepted.
func TestPushReady(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	var logs syncBuffer
	push := sdktest.NewPush()
	base, code := runMain(t, ctx, pushEnv(t), &logs, sdk.WithPushSource(push))
	eventually(t, "the module listens", func() bool { return status(t, base+"/readyz") == http.StatusServiceUnavailable })
	if err := push.Send(t.Context(), map[string]string{"api-key": "k"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the module is ready", func() bool { return status(t, base+"/readyz") == http.StatusOK })
	if s := status(t, base+"/"); s != http.StatusOK {
		t.Errorf("a call after the first snapshot = %d, want 200", s)
	}
	cancel()
	if c := <-code; c != 0 {
		t.Errorf("exit code %d, want 0\n%s", c, logs.String())
	}
}

func asCaller(ctx context.Context, user string) context.Context {
	return sdk.WithCaller(ctx, sdk.Caller{Issuer: "api", User: user})
}

// TestCallerSource covers the caller source of AC-10: it answers one value differently for one
// caller; another caller and a call without a caller get the snapshot value; an answer is kept
// for its lifetime, per caller and value; a bad answer is an error that is not ErrCallerSource.
func TestCallerSource(t *testing.T) {
	t.Parallel()
	callers := &sdktest.CallerSource{Answers: map[string]map[string]string{"alice": {"greeting": "hi", "count": "0"}}}
	b, err := buildWith(t, baseEnv(t), sdk.WithCallerSource(callers),
		sdk.WithSourceTimes(time.Second, time.Second, time.Second, 300*time.Millisecond))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, b.logs)
	}
	ctx := t.Context()
	alice, bob := asCaller(ctx, "alice"), asCaller(ctx, "bob")
	for _, tc := range []struct {
		ctx       context.Context
		want      string
		wantCalls int
	}{
		{ctx, "hello", 0},
		{alice, "hi", 1},
		{alice, "hi", 1}, // kept
		{bob, "hello", 2},
		{bob, "hello", 2}, // kept: not found is an answer too
	} {
		if g := must[string](t)(b.cfg.Greeting(tc.ctx)); g != tc.want {
			t.Errorf("greeting = %q, want %q", g, tc.want)
		}
		if n := callers.Calls(); n != tc.wantCalls {
			t.Errorf("calls = %d, want %d", n, tc.wantCalls)
		}
	}
	if _, err := b.cfg.Count(alice); err == nil || errors.Is(err, sdk.ErrCallerSource) || !strings.Contains(err.Error(), "out_of_range") {
		t.Errorf("a bad answer: err = %v, want an out_of_range error", err)
	}
	time.Sleep(400 * time.Millisecond)
	_ = must[string](t)(b.cfg.Greeting(alice))
	if n := callers.Calls(); n != 4 {
		t.Errorf("after the lifetime calls = %d, want 4 (asked again)", n)
	}
}

// TestCallerSourceFails covers a caller source that cannot answer (AC-10): past the 200 ms
// budget, or with an error, the getter returns ErrCallerSource without the source's error text
// and the module answers 503.
func TestCallerSourceFails(t *testing.T) {
	t.Parallel()
	for name, callers := range map[string]*sdktest.CallerSource{
		"slow":    {Delay: 2 * time.Second},
		"failing": {Err: errors.New("policy down: " + sourceMarker)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, err := buildWith(t, baseEnv(t), sdk.WithCallerSource(callers))
			if err != nil {
				t.Fatalf("start refused: %v\n%s", err, b.logs)
			}
			began := time.Now()
			_, err = b.cfg.Greeting(asCaller(t.Context(), "alice"))
			if !errors.Is(err, sdk.ErrCallerSource) {
				t.Errorf("err = %v, want ErrCallerSource", err)
			}
			if took := time.Since(began); took > time.Second {
				t.Errorf("the getter took %v, want about 200 ms at most", took)
			}
			if err != nil && strings.Contains(err.Error(), sourceMarker) {
				t.Error("the source's error text shows in the getter's error")
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?user=carol", nil)
			req.Header.Set("Authorization", "Bearer "+consumerToken)
			rec := httptest.NewRecorder()
			b.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("the module answered %d, want 503", rec.Code)
			}
		})
	}
}

// TestCallerSourceSwap covers a pushed snapshot clearing the per caller answers (AC-10).
func TestCallerSourceSwap(t *testing.T) {
	t.Parallel()
	callers := &sdktest.CallerSource{Answers: map[string]map[string]string{"alice": {"greeting": "hi"}}}
	push := sdktest.NewPush()
	b := buildPushed(t, push, map[string]string{"api-key": "k"}, sdk.WithCallerSource(callers))
	alice := asCaller(t.Context(), "alice")
	for range 2 {
		_ = must[string](t)(b.cfg.Greeting(alice))
	}
	if n := callers.Calls(); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
	if err := push.Send(t.Context(), map[string]string{"api-key": "k", "greeting": "new"}); err != nil {
		t.Fatal(err)
	}
	if g := must[string](t)(b.cfg.Greeting(alice)); g != "hi" {
		t.Errorf("greeting = %q, want hi", g)
	}
	if n := callers.Calls(); n != 2 {
		t.Errorf("after a swap calls = %d, want 2 (asked again)", n)
	}
	if g := must[string](t)(b.cfg.Greeting(asCaller(t.Context(), "bob"))); g != "new" {
		t.Errorf("bob's greeting = %q, want the pushed new", g)
	}
}

// TestBindingResolver covers the binding resolver (spec 0005, "Pluggable sources"): a caller the
// resolver picks a binding for reaches that provider with its credentials; others reach the
// module's own binding; one client is kept per provider binding and each answer for its lifetime;
// a resolver that cannot answer is ErrCallerSource.
func TestBindingResolver(t *testing.T) {
	t.Parallel()
	const otherToken = "other-token-0123456789abcdef0123456789"
	own := provider(t, map[string]string{"consumer": consumerToken})
	other := provider(t, map[string]string{"consumer-b": otherToken})
	otherURL := must[*url.URL](t)(url.Parse(other.URL))
	picked := sdk.Binding{URL: otherURL, Token: sdk.NewSecret(otherToken)}
	resolver := &sdktest.Resolver{Bindings: map[string]map[string]sdk.Binding{
		"alice": {"echo": picked},
		"carol": {"echo": picked},
	}}
	env := baseEnv(t)
	env["TM_ECHO_URL"] = own.URL
	env["TM_ECHO_TOKEN_FILE"] = file(t, consumerToken)
	b, err := buildWith(t, env, sdk.WithBindingResolver(resolver))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, b.logs)
	}
	ctx := t.Context()
	aliceEcho := must[*httpclient.Client](t)(b.uses.Echo(asCaller(ctx, "alice")))
	if _, body := get(t, aliceEcho, "/"); body != "consumer-b" {
		t.Errorf("alice reached %q, want the picked provider (consumer-b)", body)
	}
	bobEcho := must[*httpclient.Client](t)(b.uses.Echo(asCaller(ctx, "bob")))
	if _, body := get(t, bobEcho, "/"); body != "consumer" {
		t.Errorf("bob reached %q, want the module's own binding (consumer)", body)
	}
	if noCaller := must[*httpclient.Client](t)(b.uses.Echo(ctx)); noCaller != bobEcho {
		t.Error("a call without a caller got another client than the module's own")
	}
	carolEcho := must[*httpclient.Client](t)(b.uses.Echo(asCaller(ctx, "carol")))
	if carolEcho != aliceEcho {
		t.Error("two callers picked for one provider binding got two clients, want one")
	}
	_ = must[*httpclient.Client](t)(b.uses.Echo(asCaller(ctx, "alice")))
	if n := resolver.Calls(); n != 3 {
		t.Errorf("resolver calls = %d, want 3 (alice, bob, carol; alice's answer kept)", n)
	}
	if strings.Contains(b.logs.String(), otherToken) {
		t.Error("the picked binding's token shows in the logs")
	}

	slow, err := buildWith(t, env, sdk.WithBindingResolver(&sdktest.Resolver{Delay: 2 * time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := slow.uses.Echo(asCaller(ctx, "alice")); !errors.Is(err, sdk.ErrCallerSource) {
		t.Errorf("a slow resolver: err = %v, want ErrCallerSource", err)
	}
	noURL, err := buildWith(t, env, sdk.WithBindingResolver(&sdktest.Resolver{
		Bindings: map[string]map[string]sdk.Binding{"alice": {"echo": {}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noURL.uses.Echo(asCaller(ctx, "alice")); err == nil || errors.Is(err, sdk.ErrCallerSource) {
		t.Errorf("a binding without an address: err = %v, want an error that is not ErrCallerSource", err)
	}
}
