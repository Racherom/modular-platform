package render_test

import (
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// testStack is the stack id the managed golden files carry.
const testStack = "01960000-0000-7000-8000-000000000001"

// TestRenderManagedGolden pins every managed file of the sample and of the sdk pair, plus the
// images and secrets lists as managed.json (spec 0006, AC-12). Run with -update after an
// intended change.
func TestRenderManagedGolden(t *testing.T) {
	for _, c := range []struct {
		dir    string
		result func(*testing.T) (contract.CheckResult, contract.Catalog)
	}{
		{filepath.Join("testdata", "whoami-postgres-managed"), sampleResult},
		{filepath.Join(pairDir, "golden-managed"), pairResult},
		{filepath.Join("testdata", "bindings", "golden-managed"), bindingsResult},
	} {
		t.Run(c.dir, func(t *testing.T) {
			result, catalog := c.result(t)
			m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
			if err != nil {
				t.Fatal(err)
			}
			lists, err := json.MarshalIndent(struct {
				Images     []render.Image
				References []contract.ImageReference
				Secrets    []render.Secret
			}{m.Images, m.References, m.Secrets}, "", "  ")
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
	result, catalog := pairResult(t)
	m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
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
		"      io.github.racherom.modular-platform.stack: " + testStack + "\n",
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
	if kinds["secrets/consumer/uses_greet_token"] != render.SecretGenerated {
		t.Errorf("binding token: %v, want generated", kinds["secrets/consumer/uses_greet_token"])
	}
}

// TestRenderManagedJobLabels checks that a provision job carries its provisioner's instance and
// module and the job label, and keeps restart "no", and that the wait anchor carries job: wait and
// no instance or module (spec 0013, "Labels").
func TestRenderManagedJobLabels(t *testing.T) {
	t.Parallel()
	result, catalog := sampleResult(t)
	m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
	if err != nil {
		t.Fatal(err)
	}
	var compose string
	for _, f := range m.Files {
		if f.Path == "docker-compose.yml" {
			compose = string(f.Data)
		}
	}
	job := from(t, compose, "  postgres_provision_whoami_postgresql:\n")
	job, _, _ = strings.Cut(job, "\n  whoami:\n")
	for _, want := range []string{
		"      io.github.racherom.modular-platform.instance: postgres\n",
		"      io.github.racherom.modular-platform.module: postgres\n",
		"      io.github.racherom.modular-platform.job: provision\n",
		"    restart: \"no\"\n",
	} {
		if !strings.Contains(job, want) {
			t.Errorf("job lacks %q:\n%s", want, job)
		}
	}
	// the wait anchor belongs to no instance: graph, stack and job: wait only (spec 0014)
	anchor := from(t, compose, "  mp_jobs:\n")
	anchor, _, _ = strings.Cut(anchor, "\n  postgres:\n")
	if !strings.Contains(anchor, "      io.github.racherom.modular-platform.job: wait\n") || !strings.Contains(anchor, "    restart: unless-stopped\n") {
		t.Errorf("mp_jobs lacks job: wait or restart: unless-stopped:\n%s", anchor)
	}
	for _, unwanted := range []string{"modular-platform.instance", "modular-platform.module"} {
		if strings.Contains(anchor, unwanted) {
			t.Errorf("mp_jobs carries %s:\n%s", unwanted, anchor)
		}
	}
	kinds := map[string]render.SecretKind{}
	for _, s := range m.Secrets {
		kinds[s.Path] = s.Kind
	}
	if kinds["secrets/whoami/uses_postgresql_password"] != render.SecretGenerated {
		t.Errorf("binding password: %v, want generated", kinds["secrets/whoami/uses_postgresql_password"])
	}
}

func TestRenderManagedRefusesBadGraph(t *testing.T) {
	t.Parallel()
	if _, err := render.Managed(contract.CheckResult{}, contract.Catalog{}, builtinTemplates(t), testStack, nil); err != render.ErrGraphHasErrors { //nolint:errorlint // the sentinel is returned as is
		t.Fatalf("err = %v, want ErrGraphHasErrors", err)
	}
}

// managedCases are the managed goldens whose references TestManagedReferences checks.
var managedCases = []struct {
	name   string
	result func(*testing.T) (contract.CheckResult, contract.Catalog)
}{
	{"whoami-postgres", sampleResult},
	{"sdk-pair", pairResult},
	{"bindings", bindingsResult},
}

// serviceImages is each service's image in m's docker-compose.yml.
func serviceImages(t *testing.T, m render.ManagedProject) map[string]string {
	t.Helper()
	var compose struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal(fileData(t, m.Output, "docker-compose.yml"), &compose); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for name, svc := range compose.Services {
		out[name] = svc.Image
	}
	return out
}

// TestManagedReferences checks spec 0031 AC-14 on every managed golden: references names exactly
// the distinct image: values of docker-compose.yml, every service is in exactly one usedBy, and
// every wait user shares its reference with a job.
func TestManagedReferences(t *testing.T) {
	t.Parallel()
	for _, c := range managedCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			result, catalog := c.result(t)
			m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
			if err != nil {
				t.Fatal(err)
			}
			images := serviceImages(t, m)
			want := slices.Compact(slices.Sorted(maps.Values(images)))
			var got []string
			seen := map[string]int{}
			for _, r := range m.References {
				got = append(got, r.Reference)
				var wait, job bool
				for _, u := range r.UsedBy {
					seen[u.Service]++
					if images[u.Service] != r.Reference {
						t.Errorf("%s runs %q, listed under %q", u.Service, images[u.Service], r.Reference)
					}
					wait = wait || u.Kind == contract.ImageUserKindWait
					job = job || u.Kind == contract.ImageUserKindProvision || u.Kind == contract.ImageUserKindRevoke
				}
				if wait && !job {
					t.Errorf("%s has a wait user and no job", r.Reference)
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("references = %v, want %v", got, want)
			}
			for name := range images {
				if seen[name] != 1 {
					t.Errorf("service %s is in %d usedBy lists, want 1", name, seen[name])
				}
			}
		})
	}
}

// TestManagedReferencesUsers pins the usedBy entries of the bindings graph's job image: the
// provision job with its provider, capability and consumer, the revoke job, and mp_jobs.
func TestManagedReferencesUsers(t *testing.T) {
	t.Parallel()
	result, catalog := bindingsResult(t)
	m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(m.References, func(r contract.ImageReference) bool { return r.Reference == "postgres:18.6" })
	if i < 0 {
		t.Fatalf("no postgres:18.6 in %+v", m.References)
	}
	want := []contract.ImageUser{
		{Service: "mp_jobs", Kind: contract.ImageUserKindWait},
		{Service: "shared_provision_app_audit", Kind: contract.ImageUserKindProvision, Instance: "shared", Capability: "postgresql-provisioner", Consumer: "app"},
		{Service: "shared_revoke_admin", Kind: contract.ImageUserKindRevoke, Instance: "shared", Capability: "postgresql-provisioner"},
	}
	if got := m.References[i].UsedBy; !slices.Equal(got, want) {
		t.Errorf("usedBy = %+v\nwant %+v", got, want)
	}
}

// lockFor pins every reference of m to a digest made from its position, so the lock golden is
// stable.
func lockFor(m render.ManagedProject) []contract.ImagePin {
	pins := make([]contract.ImagePin, 0, len(m.References))
	for n, r := range m.References {
		pins = append(pins, contract.ImagePin{Reference: r.Reference, Digest: "sha256:" + strings.Repeat(string(rune('a'+n)), 64)})
	}
	return pins
}

// TestRenderManagedLockGolden renders the bindings graph with a lock that pins every reference
// (spec 0031, AC-2 and AC-14): every service, the jobs and mp_jobs included, names @sha256:, and
// references keep the value before the lock.
func TestRenderManagedLockGolden(t *testing.T) {
	result, catalog := bindingsResult(t)
	plain, err := render.Managed(result, catalog, builtinTemplates(t), testStack, nil)
	if err != nil {
		t.Fatal(err)
	}
	lock := append(lockFor(plain), contract.ImagePin{Reference: "unused.test/x:1", Digest: "sha256:" + strings.Repeat("f", 64)})
	m, err := render.Managed(result, catalog, builtinTemplates(t), testStack, lock)
	if err != nil {
		t.Fatal(err)
	}
	for name, img := range serviceImages(t, m) {
		if !strings.Contains(img, "@sha256:") {
			t.Errorf("service %s runs %q, not pinned", name, img)
		}
	}
	if !slices.EqualFunc(m.References, plain.References, func(a, b contract.ImageReference) bool { return a.Reference == b.Reference }) {
		t.Errorf("references changed with the lock: %+v", m.References)
	}
	for _, img := range m.Images {
		if !strings.Contains(img.Image, "@sha256:") {
			t.Errorf("images entry %+v is not the written value", img)
		}
	}
	lists, err := json.MarshalIndent(struct {
		Images     []render.Image
		References []contract.ImageReference
	}{m.Images, m.References}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := render.Output{Files: []render.File{
		{Path: "docker-compose.yml", Data: fileData(t, m.Output, "docker-compose.yml")},
		{Path: "managed.json", Data: append(lists, '\n')},
	}}
	checkGolden(t, filepath.Join("testdata", "bindings", "golden-managed-lock"), out)
}

// TestRenderManagedInvalidLock refuses a malformed lock with invalid_lock at its entry.
func TestRenderManagedInvalidLock(t *testing.T) {
	t.Parallel()
	result, catalog := pairResult(t)
	_, err := render.Managed(result, catalog, builtinTemplates(t), testStack, []contract.ImagePin{
		{Reference: "example.test/x:1", Digest: "sha256:short"},
	})
	refused, ok := errors.AsType[*render.RefusedError](err)
	if !ok || len(refused.Problems) != 1 || refused.Problems[0].Code != contract.CodeInvalidLock || refused.Problems[0].Path != "lock[0]" {
		t.Fatalf("err = %v, want one invalid_lock at lock[0]", err)
	}
}
