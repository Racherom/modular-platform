package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/planner/internal/api"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

const sampleJSON = `{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"whoami-postgres",
"modules":{"postgres":{"module":"postgres"},"whoami":{"module":"whoami","uses":{"postgresql":"postgres"}}}}`

type planner struct {
	srv *httptest.Server
	log *bytes.Buffer
}

// start builds the planner the way sdk.Run does, with env as its whole process env.
func start(t *testing.T, env map[string]string) planner {
	t.Helper()
	h, logs, err := build(t, env, catalog.FS)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return planner{srv: srv, log: logs}
}

func build(t *testing.T, env map[string]string, catalogFS fs.FS) (http.Handler, *bytes.Buffer, error) {
	t.Helper()
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := sdk.Build(ctx, config.Module, api.Setup(catalogFS),
		sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return h, &logs, err
}

// publicEnv runs the planner in public mode with rate requests per minute.
func publicEnv(rate string) map[string]string {
	return map[string]string{"MP_INSTANCE": "planner", "MP_PLANNER_MODE": "public", "MP_PLANNER_RATE": rate}
}

// clientsDir writes one token file per consumer and returns the directory.
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

// errorLines returns the msg of every error line in logs.
func errorLines(t *testing.T, logs *bytes.Buffer) []string {
	t.Helper()
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		var entry struct{ Level, Msg string }
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Level == "ERROR" {
			lines = append(lines, entry.Msg)
		}
	}
	return lines
}

func (p planner) do(t *testing.T, method, path, body string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, p.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := p.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

var public = publicEnv("60")

func TestCatalogSorted(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	resp, body := p.do(t, http.MethodGet, "/v1/catalog", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var cat contract.Catalog
	if err := json.Unmarshal(body, &cat); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range cat.Entries {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"api", "deployer-docker", "planner", "postgres", "whoami"}) {
		t.Errorf("entries = %q", names)
	}
}

func TestPlanSample(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	resp, first := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, first)
	}
	var res contract.PlanResult
	if err := json.Unmarshal(first, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Ok || len(res.Bindings) != 1 || len(res.Problems) != 0 {
		t.Errorf("plan = %s", first)
	}
	_, again := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, nil)
	if !bytes.Equal(first, again) {
		t.Error("the same request gave different bytes (AC-8)")
	}
}

func TestPlanBadRequests(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	for name, tc := range map[string]struct{ body, code string }{
		"unknown key":   {strings.Replace(sampleJSON, `"kind"`, `"extra":1,"kind"`, 1), contract.CodeInvalidGraph},
		"duplicate key": {strings.Replace(sampleJSON, `"kind":"Graph"`, `"kind":"Graph","kind":"Graph"`, 1), contract.CodeInvalidGraph},
		"v1":            {strings.Replace(sampleJSON, "v0alpha1", "v1", 1), contract.CodeUnsupportedVersion},
		"kind":          {strings.Replace(sampleJSON, `"Graph"`, `"Module"`, 1), contract.CodeWrongKind},
		"not json":      {"{", contract.CodeInvalidGraph},
	} {
		for _, path := range []string{"/v1/plan", "/v1/render"} {
			resp, body := p.do(t, http.MethodPost, path, tc.body, nil)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"`+tc.code+`"`) {
				t.Errorf("%s %s: status %d body %s", name, path, resp.StatusCode, body)
			}
		}
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	resp, body := p.do(t, http.MethodPost, "/v1/render", sampleJSON, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="whoami-postgres.zip"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 5 {
		t.Errorf("zip has %d files", len(zr.File))
	}

	broken := strings.Replace(sampleJSON, `"module":"postgres"`, `"module":"ghost"`, 1)
	resp, body = p.do(t, http.MethodPost, "/v1/render", broken, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"unknown_module"`) {
		t.Errorf("render with an error: status %d body %s", resp.StatusCode, body)
	}
}

// TestPublicLimits covers AC-5 in public mode.
func TestPublicLimits(t *testing.T) {
	t.Parallel()
	p := start(t, publicEnv("1"))

	huge := strings.Replace(sampleJSON, `"name":"whoami-postgres"`, `"name":"`+strings.Repeat("a", 1<<20)+`"`, 1)
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", huge, nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("body over 1 MB: status %d, want 413", resp.StatusCode)
	}

	// the burst is 10; the 413 above took one token, from the peer address
	for i := range 20 {
		forged := http.Header{"X-Forwarded-For": {fmt.Sprintf("10.9.9.%d, 203.0.113.7", i)}}
		resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, forged)
		want := http.StatusOK
		if i >= 10 {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d with a forged leftmost entry: status %d, want %d", i+1, resp.StatusCode, want)
		}
		if want == http.StatusTooManyRequests && resp.Header.Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
	}
	other := http.Header{"X-Forwarded-For": {"203.0.113.8"}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, other); resp.StatusCode != http.StatusOK {
		t.Errorf("another client was limited too: status %d", resp.StatusCode)
	}
	limited := http.Header{"X-Forwarded-For": {"203.0.113.7"}}
	for _, path := range []string{"/healthz", "/readyz"} {
		if resp, _ := p.do(t, http.MethodGet, path, "", limited); resp.StatusCode != http.StatusOK {
			t.Errorf("%s is rate limited: status %d", path, resp.StatusCode)
		}
	}

	if strings.Contains(p.log.String(), "whoami-postgres") || strings.Contains(p.log.String(), "aaaa") {
		t.Errorf("a request body reached the log:\n%s", p.log)
	}
	if strings.Contains(p.log.String(), "203.0.113") {
		t.Error("a client address reached the log")
	}
}

// TestPublicClientCap pins the cap at exactly 10,000 clients (spec 0003 AC-1, AC-7). Every request
// renews its client, so a probe of the oldest one cannot then show its eviction: a and b both
// spend their burst, a is probed while the map is full, and b is the one the next client evicts.
func TestPublicClientCap(t *testing.T) {
	t.Parallel()
	p := start(t, publicEnv("1"))
	from := func(addr string) int {
		resp, _ := p.do(t, http.MethodGet, "/v1/catalog", "", http.Header{"X-Forwarded-For": {addr}})
		return resp.StatusCode
	}
	a, b := "203.0.113.1", "203.0.113.2"
	for _, addr := range []string{a, b} {
		for range 10 {
			from(addr)
		}
		if got := from(addr); got != http.StatusTooManyRequests {
			t.Fatalf("%s past its burst: status %d, want 429", addr, got)
		}
	}
	for i := range 10_000 - 2 {
		if got := from(fmt.Sprintf("10.0.%d.%d", i/256, i%256)); got != http.StatusOK {
			t.Fatalf("new client %d: status %d, want 200", i+1, got)
		}
	}
	if got := from(a); got != http.StatusTooManyRequests {
		t.Fatalf("a with 10,000 clients held: status %d, want 429 (still held)", got)
	}
	if got := from("10.1.0.0"); got != http.StatusOK {
		t.Fatalf("client 10,001: status %d, want 200", got)
	}
	if got := from(b); got != http.StatusOK {
		t.Errorf("b after client 10,001: status %d, want 200 (evicted, fresh burst)", got)
	}
	if got := from(a); got != http.StatusTooManyRequests {
		t.Errorf("a after client 10,001: status %d, want 429 (still held)", got)
	}
	if strings.Contains(p.log.String(), "203.0.113") || strings.Contains(p.log.String(), "10.0.") {
		t.Error("a client address reached the log")
	}
}

// TestInternalAuth covers AC-5 in internal mode: a call needs a consumer's binding token.
func TestInternalAuth(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("a1", 16)
	p := start(t, map[string]string{
		"MP_INSTANCE":            "planner",
		"MP_PLANNER_MODE":        "internal",
		"MP_PLANNER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	})
	if resp, _ := p.do(t, http.MethodGet, "/v1/catalog", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", resp.StatusCode)
	}
	wrong := http.Header{"Authorization": {"Bearer " + strings.Repeat("b2", 16)}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", resp.StatusCode)
	}
	right := http.Header{"Authorization": {"Bearer " + token}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, right); resp.StatusCode != http.StatusOK {
		t.Errorf("right token: status %d, want 200", resp.StatusCode)
	}
	if resp, _ := p.do(t, http.MethodGet, "/readyz", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz needs a token: status %d", resp.StatusCode)
	}
	if strings.Contains(p.log.String(), token) {
		t.Error("a token reached the log")
	}
}

// TestStartRefused covers spec 0005 AC-13: internal mode refuses to start without a consumer
// token in MP_PLANNER_CLIENTS_DIR, and no mode is no start.
func TestStartRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"internal without clients dir": {
			env:  map[string]string{"MP_INSTANCE": "planner", "MP_PLANNER_MODE": "internal"},
			want: "config: provides.planner.clients_dir (MP_PLANNER_CLIENTS_DIR): missing: ",
		},
		"internal with an empty clients dir": {
			env:  map[string]string{"MP_INSTANCE": "planner", "MP_PLANNER_MODE": "internal", "MP_PLANNER_CLIENTS_DIR": clientsDir(t, nil)},
			want: "config: provides.planner.clients_dir (MP_PLANNER_CLIENTS_DIR): missing: ",
		},
		"no mode": {
			env:  map[string]string{"MP_INSTANCE": "planner"},
			want: "config: planner-mode (MP_PLANNER_MODE): missing: ",
		},
		"a mode not in values": {
			env:  map[string]string{"MP_INSTANCE": "planner", "MP_PLANNER_MODE": "open"},
			want: "config: planner-mode (MP_PLANNER_MODE): not_allowed: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, logs, err := build(t, tc.env, catalog.FS)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want sdk.ErrStart", err)
			}
			lines := errorLines(t, logs)
			if len(lines) != 1 || !strings.HasPrefix(lines[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", lines, tc.want)
			}
		})
	}
}

// TestBadCatalogStopsStart covers AC-11 of spec 0002: a refused start and one log line per
// problem, and nothing else at error level.
func TestBadCatalogStopsStart(t *testing.T) {
	t.Parallel()
	bad := fstest.MapFS{
		"whoami.yaml": {Data: []byte(`apiVersion: modular-platform/v0alpha1
kind: Module
name: whoami
description: Demo.
image: traefik/whoami:latest
uses:
  postgresql:
    env:
      host: DEMO-HOST
`)},
	}
	_, logs, err := build(t, publicEnv("60"), bad)
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("err = %v, want sdk.ErrStart", err)
	}
	lines := errorLines(t, logs)
	want := []string{
		"catalog/whoami.yaml: image: image_latest: ",
		"catalog/whoami.yaml: uses.postgresql.env.host: invalid_env_name: ",
	}
	if len(lines) != len(want) {
		t.Fatalf("error lines = %q, want %d", lines, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(lines[i], want[i]) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], want[i])
		}
	}
}

// TestRenderManaged covers spec 0006 AC-18: the managed render answers in internal mode only.
func TestRenderManaged(t *testing.T) {
	t.Parallel()
	const deployment = "01960000-0000-7000-8000-000000000001"
	body := `{"graph":` + sampleJSON + `,"deployment":"` + deployment + `"}`
	token := strings.Repeat("a1", 16)
	internal := start(t, map[string]string{
		"MP_INSTANCE":            "planner",
		"MP_PLANNER_MODE":        "internal",
		"MP_PLANNER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	})
	auth := http.Header{"Authorization": {"Bearer " + token}}

	resp, data := internal.do(t, http.MethodPost, "/v1/render/managed", body, auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	var got contract.RenderedProject
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Project != "mp-whoami-postgres" || !got.Plan.Ok || got.Plan.Digest == "" {
		t.Errorf("project %q, ok %v, digest %q", got.Project, got.Plan.Ok, got.Plan.Digest)
	}
	if len(got.Images) != 2 || len(got.Secrets) != 1 || got.Secrets[0].Kind != contract.Generated {
		t.Errorf("images %+v, secrets %+v", got.Images, got.Secrets)
	}
	var compose string
	for _, f := range got.Files {
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	if !strings.Contains(compose, "io.github.racherom.modular-platform.deployment: "+deployment) {
		t.Errorf("docker-compose.yml lacks the deployment label:\n%s", compose)
	}

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"bare graph":       {sampleJSON, http.StatusBadRequest},
		"extra key":        {`{"graph":` + sampleJSON + `,"deployment":"` + deployment + `","x":1}`, http.StatusBadRequest},
		"bad uuid":         {`{"graph":` + sampleJSON + `,"deployment":"nope"}`, http.StatusBadRequest},
		"graph extra key":  {`{"graph":{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"a","modules":{},"x":1},"deployment":"` + deployment + `"}`, http.StatusBadRequest},
		"plan with errors": {`{"graph":{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"a","modules":{"w":{"module":"nope"}}},"deployment":"` + deployment + `"}`, http.StatusUnprocessableEntity},
	} {
		if resp, data := internal.do(t, http.MethodPost, "/v1/render/managed", tc.body, auth); resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d: %s", name, resp.StatusCode, tc.want, data)
		}
	}

	pub := start(t, public)
	if resp, _ := pub.do(t, http.MethodPost, "/v1/render/managed", body, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("public mode: status %d, want 404", resp.StatusCode)
	}
}
