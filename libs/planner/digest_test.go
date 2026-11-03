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
		reversed := slices.Clone(catalog)
		slices.Reverse(reversed)
		if got := planner.Plan(g, reversed).Digest; got != base.Digest {
			t.Errorf("digest = %s, want %s", got, base.Digest)
		}
	})
	t.Run("an unused catalog entry changes nothing", func(t *testing.T) {
		t.Parallel()
		extra := append(slices.Clone(catalog), contract.CatalogEntry{Name: "unused", Image: "unused:1"})
		if got := planner.Plan(g, extra).Digest; got != base.Digest {
			t.Errorf("digest = %s, want %s", got, base.Digest)
		}
	})
	t.Run("a used entry's new image changes it", func(t *testing.T) {
		t.Parallel()
		changed := slices.Clone(catalog)
		for i := range changed {
			if changed[i].Name == "whoami" {
				changed[i].Image = "traefik/whoami:v9.9.9"
			}
		}
		if got := planner.Plan(g, changed).Digest; got == base.Digest {
			t.Error("digest did not change with the image")
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
