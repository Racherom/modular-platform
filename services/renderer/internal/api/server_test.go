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
	"github.com/racherom/modular-platform/services/renderer/internal/api"
	"github.com/racherom/modular-platform/services/renderer/internal/config"
)

const sampleJSON = `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"whoami-postgres",
"modules":{"postgres":{"module":"postgres"},"whoami":{"module":"whoami","uses":{"postgresql":"postgres"}}}}`

type renderer struct {
	srv *httptest.Server
	log *bytes.Buffer
}

// start builds the renderer the way sdk.Run does, with env as its whole process env.
func start(t *testing.T, env map[string]string) renderer {
	t.Helper()
	h, logs, err := build(t, env, catalog.FS)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return renderer{srv: srv, log: logs}
}

func build(t *testing.T, env map[string]string, catalogFS fs.FS) (http.Handler, *bytes.Buffer, error) {
	t.Helper()
	return buildRef(t, env, catalogFS, "")
}

// buildRef is build for a renderer made from the git ref sourceRef.
func buildRef(t *testing.T, env map[string]string, catalogFS fs.FS, sourceRef string) (http.Handler, *bytes.Buffer, error) {
	t.Helper()
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := sdk.Build(ctx, config.Module, api.Setup(catalogFS, sourceRef),
		sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return h, &logs, err
}

// publicEnv runs the renderer in public mode with rate requests per minute.
func publicEnv(rate string) map[string]string {
	return map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "public", "MP_RENDERER_RATE": rate}
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

func (p renderer) do(t *testing.T, method, path, body string, header http.Header) (*http.Response, []byte) {
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
	var modules, capabilities []string
	for _, m := range cat.Modules {
		modules = append(modules, m.Name)
	}
	for _, c := range cat.Capabilities {
		capabilities = append(capabilities, c.Name)
	}
	if !slices.Equal(modules, []string{"api", "authelia", "caddy", "cert-issuer", "deployer-docker", "postgres", "renderer", "web", "whoami"}) {
		t.Errorf("modules = %q", modules)
	}
	if !slices.IsSorted(capabilities) || !slices.Contains(capabilities, "postgresql-provisioner") {
		t.Errorf("capabilities = %q", capabilities)
	}
}

// TestCheckConvertsV0alpha1 covers AC-14: a v0alpha1 graph is answered in v1 with a warning.
func TestCheckConvertsV0alpha1(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	resp, body := p.do(t, http.MethodPost, "/v1/check", strings.Replace(sampleJSON, "modular-platform/v1", "modular-platform/v0alpha1", 1), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var result contract.CheckResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Ok || result.Graph.ApiVersion != contract.APIVersion || len(result.Problems) != 1 || result.Problems[0].Code != contract.CodeConvertedFromV0alpha1 {
		t.Errorf("result = %+v, want an ok v1 result with the converted warning", result)
	}
	newer := strings.Replace(sampleJSON, `"kind"`, `"formatVersion":"1.7","later":1,"kind"`, 1)
	if resp, body := p.do(t, http.MethodPost, "/v1/check", newer, nil); resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), `"code":"`+contract.CodeFormatTooNew+`"`) {
		t.Errorf("newer format: status %d body %s, want 400 format_too_new", resp.StatusCode, body)
	}
}

func TestCheckSample(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	resp, first := p.do(t, http.MethodPost, "/v1/check", sampleJSON, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, first)
	}
	var res contract.CheckResult
	if err := json.Unmarshal(first, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Ok || len(res.Bindings) != 1 || len(res.Problems) != 0 {
		t.Errorf("result = %s", first)
	}
	_, again := p.do(t, http.MethodPost, "/v1/check", sampleJSON, nil)
	if !bytes.Equal(first, again) {
		t.Error("the same request gave different bytes (AC-8)")
	}
}

func TestCheckBadRequests(t *testing.T) {
	t.Parallel()
	p := start(t, public)
	for name, tc := range map[string]struct{ body, code string }{
		"unknown key":   {strings.Replace(sampleJSON, `"kind"`, `"extra":1,"kind"`, 1), contract.CodeInvalidGraph},
		"duplicate key": {strings.Replace(sampleJSON, `"kind":"Graph"`, `"kind":"Graph","kind":"Graph"`, 1), contract.CodeInvalidGraph},
		"v2":            {strings.Replace(sampleJSON, "modular-platform/v1", "modular-platform/v2", 1), contract.CodeUnsupportedVersion},
		"kind":          {strings.Replace(sampleJSON, `"Graph"`, `"Module"`, 1), contract.CodeWrongKind},
		"not json":      {"{", contract.CodeInvalidGraph},
	} {
		for _, path := range []string{"/v1/check", "/v1/render"} {
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
	// docker-compose.yml, graph.yaml, setup.sh, .env, README.md and postgres's two config files
	if len(zr.File) != 7 {
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
	if resp, _ := p.do(t, http.MethodPost, "/v1/check", huge, nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("body over 1 MB: status %d, want 413", resp.StatusCode)
	}

	// the burst is 10; the 413 above took one token, from the peer address
	for i := range 20 {
		forged := http.Header{"X-Forwarded-For": {fmt.Sprintf("10.9.9.%d, 203.0.113.7", i)}}
		resp, _ := p.do(t, http.MethodPost, "/v1/check", sampleJSON, forged)
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
	if resp, _ := p.do(t, http.MethodPost, "/v1/check", sampleJSON, other); resp.StatusCode != http.StatusOK {
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
		"MP_INSTANCE":             "renderer",
		"MP_RENDERER_MODE":        "internal",
		"MP_PLATFORM_HTTP_PORT":   "80",
		"MP_PLATFORM_HTTPS_PORT":  "443",
		"MP_RENDERER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	})
	if resp, _ := p.do(t, http.MethodGet, "/v1/catalog", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", resp.StatusCode)
	}
	wrong := http.Header{"Authorization": {"Bearer " + strings.Repeat("b2", 16)}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/check", sampleJSON, wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", resp.StatusCode)
	}
	right := http.Header{"Authorization": {"Bearer " + token}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/check", sampleJSON, right); resp.StatusCode != http.StatusOK {
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
// token in MP_RENDERER_CLIENTS_DIR, and no mode is no start; and spec 0025 AC-24: internal mode
// refuses to start without the ingress binding, in one line.
func TestStartRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"internal without clients dir": {
			env:  map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "internal", "MP_PLATFORM_HTTP_PORT": "80", "MP_PLATFORM_HTTPS_PORT": "443"},
			want: "config: provides.renderer.clients_dir (MP_RENDERER_CLIENTS_DIR): missing: ",
		},
		"internal with an empty clients dir": {
			env:  map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "internal", "MP_PLATFORM_HTTP_PORT": "80", "MP_PLATFORM_HTTPS_PORT": "443", "MP_RENDERER_CLIENTS_DIR": clientsDir(t, nil)},
			want: "config: provides.renderer.clients_dir (MP_RENDERER_CLIENTS_DIR): missing: ",
		},
		"internal without the ingress binding": {
			env:  map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "internal", "MP_RENDERER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": strings.Repeat("t", 32)})},
			want: "config: uses.ingress (MP_PLATFORM_HTTP_PORT): missing: internal mode needs the ingress binding",
		},
		"internal with half the ingress binding": {
			env:  map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "internal", "MP_PLATFORM_HTTP_PORT": "80"},
			want: "config: uses.ingress.https_port (MP_PLATFORM_HTTPS_PORT): missing: ",
		},
		"no mode": {
			env:  map[string]string{"MP_INSTANCE": "renderer"},
			want: "config: renderer-mode (MP_RENDERER_MODE): missing: ",
		},
		"a mode not in values": {
			env:  map[string]string{"MP_INSTANCE": "renderer", "MP_RENDERER_MODE": "open"},
			want: "config: renderer-mode (MP_RENDERER_MODE): not_allowed: ",
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

// TestBadCatalogStopsStart covers AC-11 of spec 0002 and AC-2 of spec 0011: a refused start and one log line per
// problem, and nothing else at error level.
func TestBadCatalogStopsStart(t *testing.T) {
	t.Parallel()
	bad := fstest.MapFS{
		"capabilities/postgresql.yaml": {Data: []byte(`apiVersion: modular-platform/v1
kind: Capability
name: postgresql
description: A database.
fields:
  host: {type: string, description: Host.}
`)},
		"modules/whoami/module.yaml": {Data: []byte(`apiVersion: modular-platform/v1
kind: Module
name: whoami
description: Demo.
image: traefik/whoami:latest
uses:
  postgresql:
    capability: postgresql
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
		"catalog/modules/whoami/module.yaml: image: image_latest: ",
		"catalog/modules/whoami/module.yaml: uses.postgresql.env.host: invalid_env_name: ",
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
	const stack = "01960000-0000-7000-8000-000000000001"
	body := `{"graph":` + sampleJSON + `,"stack":"` + stack + `"}`
	token := strings.Repeat("a1", 16)
	internal := start(t, map[string]string{
		"MP_INSTANCE":             "renderer",
		"MP_RENDERER_MODE":        "internal",
		"MP_PLATFORM_HTTP_PORT":   "80",
		"MP_PLATFORM_HTTPS_PORT":  "443",
		"MP_RENDERER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	})
	auth := http.Header{"Authorization": {"Bearer " + token}}

	resp, data := internal.do(t, http.MethodPost, "/v1/render/managed", body, auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	var got contract.RenderedStack
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ComposeProject != "mp-whoami-postgres" || !got.Check.Ok || got.Check.Digest == "" {
		t.Errorf("project %q, ok %v, digest %q", got.ComposeProject, got.Check.Ok, got.Check.Digest)
	}
	// the postgres password and the provisioned binding's password, both made by the deployer
	if len(got.Images) != 2 || len(got.Secrets) != 2 || got.Secrets[0].Kind != contract.Generated || got.Secrets[1].Kind != contract.Generated {
		t.Errorf("images %+v, secrets %+v", got.Images, got.Secrets)
	}
	var compose string
	for _, f := range got.Files {
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	if !strings.Contains(compose, "io.github.racherom.modular-platform.stack: "+stack) {
		t.Errorf("docker-compose.yml lacks the stack label:\n%s", compose)
	}

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"bare graph":         {sampleJSON, http.StatusBadRequest},
		"extra key":          {`{"graph":` + sampleJSON + `,"stack":"` + stack + `","x":1}`, http.StatusBadRequest},
		"bad uuid":           {`{"graph":` + sampleJSON + `,"stack":"nope"}`, http.StatusBadRequest},
		"graph extra key":    {`{"graph":{"apiVersion":"modular-platform/v1","kind":"Graph","name":"a","modules":{},"x":1},"stack":"` + stack + `"}`, http.StatusBadRequest},
		"result with errors": {`{"graph":{"apiVersion":"modular-platform/v1","kind":"Graph","name":"a","modules":{"w":{"module":"nope"}}},"stack":"` + stack + `"}`, http.StatusUnprocessableEntity},
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

// TestRenderManagedRefuses covers AC-11 through the contract: the check result warns of host access,
// and the managed render answers 422 with error host_access at the instance that publishes and
// error setup_required at the one that asks a value, the warning replaced by the error.
func TestRenderManagedRefuses(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"modules/edge/module.yaml": {Data: []byte(`apiVersion: modular-platform/v1
kind: Module
name: edge
description: Publishes a host port.
image: traefik/whoami:v1.11.0
publish:
  - {port: 80, host: 8080}
`)},
		"modules/asker/module.yaml": {Data: []byte(`apiVersion: modular-platform/v1
kind: Module
name: asker
description: Asks a value at setup.
image: traefik/whoami:v1.11.0
config:
  domain: {env: ASKER_DOMAIN, type: string, setup: ask, description: Domain.}
`)},
	}
	if err := fs.WalkDir(catalog.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(catalog.FS, path)
		fsys[path] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a1", 16)
	h, logs, err := build(t, map[string]string{
		"MP_INSTANCE":             "renderer",
		"MP_RENDERER_MODE":        "internal",
		"MP_PLATFORM_HTTP_PORT":   "80",
		"MP_PLATFORM_HTTPS_PORT":  "443",
		"MP_RENDERER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	}, fsys)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := renderer{srv: srv, log: logs}
	auth := http.Header{"Authorization": {"Bearer " + token}}
	graph := `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"reach","modules":{"web":{"module":"edge"},"ask":{"module":"asker"}}}`

	problems := func(data []byte) []string {
		t.Helper()
		var result contract.CheckResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		out := []string{fmt.Sprintf("ok %v, digest %v", result.Ok, result.Digest != "")}
		for _, pr := range result.Problems {
			out = append(out, string(pr.Severity)+" "+pr.Code+" "+pr.Path)
		}
		return out
	}

	resp, data := p.do(t, http.MethodPost, "/v1/check", graph, auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("result: status %d: %s", resp.StatusCode, data)
	}
	if got, want := problems(data), []string{"ok true, digest true", "warning host_access modules.web"}; !slices.Equal(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}

	resp, data = p.do(t, http.MethodPost, "/v1/render/managed", `{"graph":`+graph+`,"stack":"01960000-0000-7000-8000-000000000001"}`, auth)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("managed render: status %d, want 422: %s", resp.StatusCode, data)
	}
	if got, want := problems(data), []string{"ok false, digest false", "error setup_required modules.ask", "error host_access modules.web"}; !slices.Equal(got, want) {
		t.Errorf("managed render = %v, want %v", got, want)
	}
}

// TestRenderRefusesARecommendation covers spec 0027 AC-8: a graph with an open
// binding_recommended renders neither as a zip nor for the managed deployer; both answer 422
// with the check result, as for any other error.
func TestRenderRefusesARecommendation(t *testing.T) {
	t.Parallel()
	// caddy needs trust, which only cert-issuer provides: a recommendation, not a binding
	graph := `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"gap","modules":{"cert-issuer":{"module":"cert-issuer"},"ingress":{"module":"caddy"}}}`
	pub := start(t, public)
	resp, body := pub.do(t, http.MethodPost, "/v1/render", graph, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"code":"binding_recommended"`) {
		t.Errorf("render: status %d body %s", resp.StatusCode, body)
	}

	token := strings.Repeat("b2", 16)
	internal := start(t, map[string]string{
		"MP_INSTANCE":             "renderer",
		"MP_RENDERER_MODE":        "internal",
		"MP_PLATFORM_HTTP_PORT":   "80",
		"MP_PLATFORM_HTTPS_PORT":  "443",
		"MP_RENDERER_CLIENTS_DIR": clientsDir(t, map[string]string{"api": token}),
	})
	managed := `{"graph":` + graph + `,"stack":"01960000-0000-7000-8000-000000000001"}`
	resp, body = internal.do(t, http.MethodPost, "/v1/render/managed", managed, http.Header{"Authorization": {"Bearer " + token}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"code":"binding_recommended"`) {
		t.Errorf("managed render: status %d body %s", resp.StatusCode, body)
	}
}

// TestRetiredRoutesAre404 covers spec 0027 AC-6: the old check route is gone.
func TestRetiredRoutesAre404(t *testing.T) {
	t.Parallel()
	r := start(t, public)
	for _, path := range []string{"/v1/plan"} { // vocabulary: allow
		resp, body := r.do(t, http.MethodPost, path, sampleJSON, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s: status %d, want 404: %.200s", path, resp.StatusCode, body)
		}
	}
}

// TestSourceRef covers spec 0030 AC-10: the start logs the source ref, warns for a ref that can
// move, and refuses one of the wrong form.
func TestSourceRef(t *testing.T) {
	t.Parallel()
	const commit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	for name, tc := range map[string]struct {
		ref, logged string
		warn        bool
		refused     bool
	}{
		"full commit":  {ref: commit, logged: commit},
		"empty":        {ref: "", logged: "main", warn: true},
		"branch":       {ref: "main", logged: "main", warn: true},
		"short commit": {ref: "b5f34e9", refused: true},
		"space":        {ref: "a b", refused: true},
		"quote":        {ref: "a'b", refused: true},
		"hash":         {ref: "a#b", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, logs, err := buildRef(t, publicEnv("60"), catalog.FS, tc.ref)
			if tc.refused {
				if !errors.Is(err, sdk.ErrStart) {
					t.Fatalf("err = %v, want sdk.ErrStart", err)
				}
				if lines := errorLines(t, logs); len(lines) != 1 || !strings.Contains(lines[0], "source ref") {
					t.Errorf("error lines %q, want one naming the source ref", lines)
				}
				return
			}
			if err != nil {
				t.Fatalf("start refused: %v\n%s", err, logs)
			}
			var started, warned bool
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				var entry struct {
					Level, Msg string
					SourceRef  string `json:"source_ref"`
				}
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				switch {
				case entry.Msg == "renderer starting":
					started = entry.SourceRef == tc.logged
				case entry.Level == "WARN" && strings.Contains(entry.Msg, "source ref"):
					warned = true
				}
			}
			if !started || warned != tc.warn {
				t.Errorf("started with source_ref %q: %v, warned %v (want %v):\n%s", tc.logged, started, warned, tc.warn, logs)
			}
		})
	}
}

// TestRenderBuildsUnreleasedImages covers spec 0030 AC-1 through the handler: a zip that places
// the renderer, whose catalog image is unreleased, carries its catalog Dockerfile and builds it
// in setup.sh from the renderer's own source ref; postgres, from a registry, builds nothing.
func TestRenderBuildsUnreleasedImages(t *testing.T) {
	t.Parallel()
	const commit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	h, logs, err := buildRef(t, publicEnv("60"), catalog.FS, commit)
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	graph := `{"apiVersion":"modular-platform/v1","kind":"Graph","name":"self",
"modules":{"renderer":{"module":"renderer"},"postgres":{"module":"postgres"}}}`
	resp, body := renderer{srv: srv, log: logs}.do(t, http.MethodPost, "/v1/render", graph, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	want, err := fs.ReadFile(catalog.FS, "modules/renderer/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files["build/renderer/Dockerfile"], want) {
		t.Error("the zip's build/renderer/Dockerfile is not the catalog's")
	}
	for name := range files {
		if strings.HasPrefix(name, "build/") && name != "build/renderer/Dockerfile" {
			t.Errorf("the zip holds %s", name)
		}
	}
	setup := string(files["setup.sh"])
	for _, want := range []string{"source_ref='" + commit + "'\n", "\nbuild_image 'ghcr.io/racherom/modular-platform/renderer:0.0.0' 'renderer'\n"} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup.sh lacks %q", want)
		}
	}
	if bytes.Contains(files["docker-compose.yml"], []byte("build:")) {
		t.Error("docker-compose.yml has a build section")
	}
}
