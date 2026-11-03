package render_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// testDeployment is the deployment id the managed golden files carry.
const testDeployment = "01960000-0000-7000-8000-000000000001"

// TestRenderManagedGolden pins every managed file of the sample and of the sdk pair, plus the
// images and secrets lists as managed.json (spec 0006, AC-12). Run with -update after an
// intended change.
func TestRenderManagedGolden(t *testing.T) {
	for _, c := range []struct {
		dir  string
		plan func(*testing.T) (contract.PlanResult, []contract.CatalogEntry)
	}{
		{filepath.Join("testdata", "whoami-postgres-managed"), samplePlan},
		{filepath.Join(pairDir, "golden-managed"), pairPlan},
	} {
		t.Run(c.dir, func(t *testing.T) {
			plan, catalog := c.plan(t)
			m, err := render.Managed(plan, catalog, testDeployment)
			if err != nil {
				t.Fatal(err)
			}
			lists, err := json.MarshalIndent(struct {
				Images  []render.Image
				Secrets []render.Secret
			}{m.Images, m.Secrets}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			out := m.Output
			out.Files = append(out.Files, render.File{Path: "managed.json", Mode: 0o644, Data: append(lists, '\n')})
			checkGolden(t, c.dir, out)
		})
	}
}

// TestRenderManagedRules covers the managed additions one rule at a time.
func TestRenderManagedRules(t *testing.T) {
	t.Parallel()
	plan, catalog := pairPlan(t)
	m, err := render.Managed(plan, catalog, testDeployment)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "mp-sdk-pair" {
		t.Errorf("project = %q, want mp-sdk-pair", m.Name)
	}
	var compose string
	for _, f := range m.Files {
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	for _, want := range []string{
		"name: mp-sdk-pair\n",
		"      io.github.racherom.modular-platform.deployment: " + testDeployment + "\n",
		"      io.github.racherom.modular-platform.graph: sdk-pair\n",
		"      io.github.racherom.modular-platform.instance: consumer\n",
		"      io.github.racherom.modular-platform.module: pair-consumer\n",
		"    restart: unless-stopped\n",
		// host names keep the graph name, not the project name
		"        - consumer.sdk-pair.internal\n",
		"      MP_DOMAIN: sdk-pair.internal\n",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("docker-compose.yml lacks %q", want)
		}
	}
	if got := strings.Count(compose, "restart: unless-stopped"); got != 2 {
		t.Errorf("restart policy on %d services, want 2", got)
	}
	kinds := map[string]render.SecretKind{}
	for _, s := range m.Secrets {
		kinds[s.Path] = s.Kind
	}
	if kinds["secrets/consumer/uses_greet"] != render.SecretGenerated {
		t.Errorf("binding token: %v, want generated", kinds["secrets/consumer/uses_greet"])
	}
}

func TestRenderManagedRefusesBadPlan(t *testing.T) {
	t.Parallel()
	if _, err := render.Managed(contract.PlanResult{}, nil, testDeployment); err != render.ErrPlanHasErrors { //nolint:errorlint // the sentinel is returned as is
		t.Fatalf("err = %v, want ErrPlanHasErrors", err)
	}
}
