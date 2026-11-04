package planner_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/planner"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestDigest(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	g := readFixture(t, "sample")
	base := planner.Plan(g, catalog)
	if !base.Ok || !digestPattern.MatchString(base.Digest) {
		t.Fatalf("ok plan: ok=%v digest=%q", base.Ok, base.Digest)
	}

	t.Run("same input, same digest, whatever the catalog order", func(t *testing.T) {
		t.Parallel()
		reversed := contract.Catalog{Modules: slices.Clone(catalog.Modules), Capabilities: slices.Clone(catalog.Capabilities)}
		slices.Reverse(reversed.Modules)
		slices.Reverse(reversed.Capabilities)
		if got := planner.Plan(g, reversed).Digest; got != base.Digest {
			t.Errorf("digest = %s, want %s", got, base.Digest)
		}
	})
	t.Run("an unused catalog entry changes nothing", func(t *testing.T) {
		t.Parallel()
		extra := contract.Catalog{
			Modules:      append(slices.Clone(catalog.Modules), contract.Module{Name: "unused", Image: "unused:1"}),
			Capabilities: append(slices.Clone(catalog.Capabilities), contract.Capability{Name: "unused"}),
		}
		if got := planner.Plan(g, extra).Digest; got != base.Digest {
			t.Errorf("digest = %s, want %s", got, base.Digest)
		}
	})
	t.Run("a used entry's new image changes it", func(t *testing.T) {
		t.Parallel()
		changed := contract.Catalog{Modules: slices.Clone(catalog.Modules), Capabilities: catalog.Capabilities}
		for i := range changed.Modules {
			if changed.Modules[i].Name == "whoami" {
				changed.Modules[i].Image = "traefik/whoami:v9.9.9"
			}
		}
		if got := planner.Plan(g, changed).Digest; got == base.Digest {
			t.Error("digest did not change with the image")
		}
	})
	t.Run("a named capability's new job changes it", func(t *testing.T) {
		t.Parallel()
		changed := contract.Catalog{Modules: catalog.Modules, Capabilities: slices.Clone(catalog.Capabilities)}
		for i, c := range changed.Capabilities {
			if c.Name == "postgresql-provisioner" {
				y := *c.Yields
				y.Job.Image = "postgres:18.7"
				changed.Capabilities[i].Yields = &y
			}
		}
		if got := planner.Plan(g, changed).Digest; got == base.Digest {
			t.Error("digest did not change with the job image")
		}
	})
	t.Run("a v0alpha1 graph digests as the same graph in v1 (AC-14)", func(t *testing.T) {
		t.Parallel()
		old := contract.CloneGraph(g)
		old.ApiVersion = contract.APIVersionV0alpha1
		got := planner.Plan(old, catalog)
		if got.Digest != base.Digest {
			t.Errorf("digest = %s, want %s", got.Digest, base.Digest)
		}
		if len(got.Problems) != 1 || got.Problems[0].Code != contract.CodeConvertedFromV0alpha1 || got.Graph.ApiVersion != contract.APIVersion {
			t.Errorf("problems = %+v, apiVersion %s; want the converted warning and v1", got.Problems, got.Graph.ApiVersion)
		}
	})
	t.Run("another graph name changes it", func(t *testing.T) {
		t.Parallel()
		renamed := contract.CloneGraph(g)
		renamed.Name = "other"
		if got := planner.Plan(renamed, catalog).Digest; got == base.Digest {
			t.Error("digest did not change with the name")
		}
	})
	t.Run("a plan with an error has none", func(t *testing.T) {
		t.Parallel()
		bad := contract.CloneGraph(g)
		bad.Name = "Not A Label"
		if got := planner.Plan(bad, catalog); got.Ok || got.Digest != "" {
			t.Errorf("ok=%v digest=%q, want no digest", got.Ok, got.Digest)
		}
	})
}
