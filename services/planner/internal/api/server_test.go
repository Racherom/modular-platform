package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/services/planner/internal/api"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

const sampleJSON = `{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"whoami-postgres",
"modules":{"postgres":{"module":"postgres"},"whoami":{"module":"whoami","uses":{"postgresql":"postgres"}}}}`

type planner struct {
	srv *httptest.Server
	log *bytes.Buffer
}

func start(t *testing.T, cfg config.Config) planner {
	t.Helper()
	entries, problems := contract.LoadCatalog(catalog.FS)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := httptest.NewServer(api.NewHandler(ctx, cfg, entries, slog.New(slog.NewJSONHandler(&logs, nil))))
	t.Cleanup(srv.Close)
	return planner{srv: srv, log: &logs}
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

var public = config.Config{Mode: config.ModePublic, Rate: 60, ProxyHops: 1}

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
	if len(cat.Entries) != 2 || cat.Entries[0].Name != "postgres" || cat.Entries[1].Name != "whoami" {
		t.Errorf("entries = %+v", cat.Entries)
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
	p := start(t, config.Config{Mode: config.ModePublic, Rate: 1, ProxyHops: 1})

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
	p := start(t, config.Config{Mode: config.ModePublic, Rate: 1, ProxyHops: 1})
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

// TestInternalAuth covers AC-5 in internal mode.
func TestInternalAuth(t *testing.T) {
	t.Parallel()
	p := start(t, config.Config{Mode: config.ModeInternal, Token: "s3cret", ProxyHops: 1})
	if resp, _ := p.do(t, http.MethodGet, "/v1/catalog", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", resp.StatusCode)
	}
	wrong := http.Header{"Authorization": {"Bearer nope"}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", resp.StatusCode)
	}
	right := http.Header{"Authorization": {"Bearer s3cret"}}
	if resp, _ := p.do(t, http.MethodPost, "/v1/plan", sampleJSON, right); resp.StatusCode != http.StatusOK {
		t.Errorf("right token: status %d, want 200", resp.StatusCode)
	}
	if resp, _ := p.do(t, http.MethodGet, "/readyz", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz needs a token: status %d", resp.StatusCode)
	}
}

func TestInternalModeNeedsToken(t *testing.T) {
	t.Setenv("MP_PLANNER_MODE", "internal")
	t.Setenv("MP_PLANNER_TOKEN_FILE", "")
	if _, err := config.FromEnv(); err == nil {
		t.Error("internal mode started without MP_PLANNER_TOKEN_FILE")
	}
	file := t.TempDir() + "/token"
	if err := os.WriteFile(file, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MP_PLANNER_TOKEN_FILE", file)
	cfg, err := config.FromEnv()
	if err != nil || cfg.Token != "s3cret" {
		t.Errorf("FromEnv = %+v, %v", cfg, err)
	}
	t.Setenv("MP_PLANNER_MODE", "")
	if _, err := config.FromEnv(); err == nil {
		t.Error("started without a mode")
	}
}
