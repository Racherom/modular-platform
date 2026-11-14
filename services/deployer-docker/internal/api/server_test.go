package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/api"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

type okCompose struct{}

func (okCompose) Run(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

type noDocker struct{}

func (noDocker) Containers(context.Context, string) ([]deploy.Container, error) { return nil, nil }

func (noDocker) Events(context.Context, time.Time) (<-chan deploy.ExitEvent, <-chan error) {
	return nil, make(chan error)
}

func (noDocker) ImageInspect(context.Context, string) (deploy.Image, error) {
	return deploy.Image{}, deploy.ErrImageNotFound
}

func (noDocker) ImageTag(context.Context, string, string) error { return nil }

func (noDocker) ImageUntag(context.Context, string) error { return nil }

func (noDocker) ImageTags(context.Context, string) ([]string, error) { return nil, nil }

// DistributionDigest knows every reference except those whose tag is missing.
func (noDocker) DistributionDigest(_ context.Context, ref string) (string, error) {
	if strings.HasSuffix(ref, ":missing") {
		return "", &deploy.ResolveError{Kind: deploy.ResolveNotFound, Err: errors.New("manifest unknown")}
	}
	return "sha256:" + strings.Repeat("a", 64), nil
}

// serve runs the handler; withCaller stands in for a checked MP-Caller token.
func serve(t *testing.T, withCaller bool) (*httptest.Server, *deploy.Engine) {
	t.Helper()
	e := deploy.New(t.Context(), deploy.Config{
		Dir: t.TempDir(), UID: os.Getuid(), GID: os.Getgid(), PullTimeout: time.Minute, WaitTimeout: time.Minute, ResolveTimeout: 5 * time.Second,
		ReservedPorts: []int{80, 443},
	}, okCompose{}, noDocker{}, slog.New(slog.DiscardHandler))
	t.Cleanup(e.Wait)
	h := api.NewHandler(e, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if withCaller {
			r = r.WithContext(sdk.WithCaller(r.Context(), sdk.Caller{Issuer: "https://idp", User: "user-1"}))
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, e
}

// sampleCompose is a one service project in the shape the managed render emits.
const sampleCompose = `name: mp-sample
secrets:
  postgres_password:
    file: ./secrets/postgres/password
services:
  postgres:
    image: postgres:18.6
    labels:
      io.github.racherom.modular-platform.stack: 01960000-0000-7000-8000-000000000001
      io.github.racherom.modular-platform.graph: sample
      io.github.racherom.modular-platform.instance: postgres
      io.github.racherom.modular-platform.module: postgres
    cap_drop: [ALL]
    restart: unless-stopped
    security_opt: ["no-new-privileges:true"]
    secrets:
    - source: postgres_password
`

func applyBody(t *testing.T) string {
	t.Helper()
	return applyBodyWith(t, sampleCompose)
}

func applyBodyWith(t *testing.T, compose string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"stack":   "01960000-0000-7000-8000-000000000001",
		"files":   []map[string]any{{"path": "docker-compose.yml", "mode": 420, "data": base64.StdEncoding.EncodeToString([]byte(compose))}},
		"secrets": []map[string]any{{"path": "secrets/postgres/password", "kind": "generated", "instance": "postgres"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestApplyNeedsACaller(t *testing.T) {
	t.Parallel()
	srv, e := serve(t, false)
	resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/apply", applyBody(t))
	var problems contract.ProblemList
	_ = json.Unmarshal(data, &problems)
	if resp.StatusCode != http.StatusUnauthorized || len(problems.Problems) != 1 || problems.Problems[0].Code != "invalid_token" {
		t.Errorf("status %d: %s", resp.StatusCode, data)
	}
	if resp.Header.Get(api.BootHeader) != e.BootID() {
		t.Errorf("boot id %q on an error answer, want %q", resp.Header.Get(api.BootHeader), e.BootID())
	}
}

func TestApplyAndPoll(t *testing.T) {
	t.Parallel()
	srv, e := serve(t, true)
	resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/apply", applyBody(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	if resp.Header.Get(api.BootHeader) != e.BootID() {
		t.Errorf("boot id %q", resp.Header.Get(api.BootHeader))
	}
	var op deploytarget.Operation
	if err := json.Unmarshal(data, &op); err != nil {
		t.Fatal(err)
	}
	if op.Status != deploytarget.OperationStatusRunning || op.Stack != "mp-sample" || bytes.Contains(data, []byte("finishedAt")) {
		t.Errorf("operation %s", data)
	}
	e.Wait()
	resp, data = do(t, srv, http.MethodGet, "/v1/operations/"+op.Id, "")
	if resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte(`"status":"succeeded"`)) {
		t.Errorf("poll: status %d: %s", resp.StatusCode, data)
	}
	if resp, _ := do(t, srv, http.MethodGet, "/v1/operations/nope", ""); resp.StatusCode != http.StatusNotFound || resp.Header.Get(api.BootHeader) == "" {
		t.Errorf("unknown operation: status %d, boot id %q", resp.StatusCode, resp.Header.Get(api.BootHeader))
	}
	resp, data = do(t, srv, http.MethodGet, "/v1/stacks/mp-sample/secrets", "")
	if resp.StatusCode != http.StatusOK || string(bytes.TrimSpace(data)) != `{"present":["secrets/postgres/password"]}` {
		t.Errorf("secrets: status %d: %s", resp.StatusCode, data)
	}
}

func TestBadRequests(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, true)
	for name, tc := range map[string]struct {
		method, path, body string
		want               int
	}{
		"bad project on apply": {http.MethodPost, "/v1/stacks/modular-platform/apply", applyBody(t), http.StatusUnprocessableEntity},
		"bad project on state": {http.MethodGet, "/v1/stacks/nope/state", "", http.StatusBadRequest},
		"not json":             {http.MethodPost, "/v1/stacks/mp-sample/apply", "{", http.StatusBadRequest},
		"too large":            {http.MethodPost, "/v1/stacks/mp-sample/apply", `{"files":"` + strings.Repeat("a", 9<<20) + `"}`, http.StatusRequestEntityTooLarge},
		"refused path":         {http.MethodPost, "/v1/stacks/mp-sample/apply", strings.Replace(applyBody(t), "docker-compose.yml", "../x", 1), http.StatusUnprocessableEntity},
	} {
		if resp, data := do(t, srv, tc.method, tc.path, tc.body); resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d: %.200s", name, resp.StatusCode, tc.want, data)
		}
	}
}

// TestApplyRefusesACraftedProject sends the deployer a project no render emits, as a caller that
// bypassed the checks would: the guard refuses it with 422 host_access naming the instance and key
// (spec 0006, AC-8).
func TestApplyRefusesACraftedProject(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, true)
	crafted := strings.Replace(sampleCompose, "    restart: unless-stopped\n",
		"    restart: unless-stopped\n    privileged: true\n    volumes: [/:/host]\n", 1)
	resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/apply", applyBodyWith(t, crafted))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	var problems contract.ProblemList
	if err := json.Unmarshal(data, &problems); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, p := range problems.Problems {
		paths[p.Path] = p.Code
	}
	for _, want := range []string{"services.postgres.privileged", "services.postgres.volumes.0"} {
		if paths[want] != "host_access" {
			t.Errorf("problems %s lack host_access at %s", data, want)
		}
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, false)
	if resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/remove", `{"deleteData":false}`); resp.StatusCode != http.StatusUnauthorized ||
		!bytes.Contains(data, []byte("invalid_token")) {
		t.Errorf("without a caller: status %d: %s", resp.StatusCode, data)
	}
	srv, e := serve(t, true)
	if resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/remove", `{"deleteData":false}`); resp.StatusCode != http.StatusNotFound ||
		!bytes.Contains(data, []byte("not_found")) {
		t.Errorf("nothing there: status %d: %s", resp.StatusCode, data)
	}
	if resp, _ := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/apply", applyBody(t)); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("apply: status %d", resp.StatusCode)
	}
	e.Wait()
	resp, data := do(t, srv, http.MethodPost, "/v1/stacks/mp-sample/remove", `{"deleteData":true}`)
	if resp.StatusCode != http.StatusAccepted || !bytes.Contains(data, []byte(`"action":"remove"`)) || resp.Header.Get(api.BootHeader) != e.BootID() {
		t.Errorf("remove: status %d: %s", resp.StatusCode, data)
	}
}

// TestRetiredRoutesAre404 covers spec 0027 AC-6: the old project route is gone.
func TestRetiredRoutesAre404(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, true)
	if resp, data := do(t, srv, http.MethodPost, "/v1/projects/mp-x/apply", applyBody(t)); resp.StatusCode != http.StatusNotFound { // vocabulary: allow
		t.Errorf("status %d, want 404: %.200s", resp.StatusCode, data)
	}
}

// TestImageEndpointsNeedACaller covers spec 0031: the images read and resolve need an MP-Caller
// token like apply (401 invalid_token).
func TestImageEndpointsNeedACaller(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, false)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/stacks/mp-sample/images", ""},
		{http.MethodPost, "/v1/images/resolve", `{"references":["postgres:18.6"]}`},
	} {
		resp, data := do(t, srv, c.method, c.path, c.body)
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(data), "invalid_token") {
			t.Errorf("%s %s: %d %s", c.method, c.path, resp.StatusCode, data)
		}
	}
}

// TestGetStackImages answers no lock for a new project, and 400 for a bad project name.
func TestGetStackImages(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, true)
	resp, data := do(t, srv, http.MethodGet, "/v1/stacks/mp-sample/images", "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(data)) != `{"adopted":false}` {
		t.Errorf("new project: %d %s", resp.StatusCode, data)
	}
	if resp.Header.Get("MP-Boot-Id") == "" {
		t.Error("no boot id")
	}
	if resp, data := do(t, srv, http.MethodGet, "/v1/stacks/platform/images", ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad project: %d %s", resp.StatusCode, data)
	}
}

// TestResolveImages covers AC-5 at the deployer: a found digest, image_unresolved per reference
// that failed, and invalid_reference for a reference with no tag or with a digest.
func TestResolveImages(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, true)
	resp, data := do(t, srv, http.MethodPost, "/v1/images/resolve", `{"references":["postgres:18.6","example.test/x:missing"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	var got deploytarget.ResolveResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Images) != 1 || got.Images[0].Reference != "postgres:18.6" || len(got.Problems) != 1 ||
		got.Problems[0].Code != "image_unresolved" || got.Problems[0].Path != "references[1]" {
		t.Errorf("result = %+v", got)
	}
	for _, body := range []string{
		`{"references":["postgres"]}`,
		`{"references":["postgres:18.6@sha256:` + strings.Repeat("a", 64) + `"]}`,
		`{"references":[]}`,
	} {
		resp, data := do(t, srv, http.MethodPost, "/v1/images/resolve", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, resp.StatusCode, data)
		}
	}
}
