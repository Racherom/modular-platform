package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/libs/contract"
)

// digestInput is what a digest covers: the planned graph, its bindings and the catalog entries
// the graph uses, so a changed entry (a new image tag, say) changes the digest too.
type digestInput struct {
	Graph    contract.Graph          `json:"graph"`
	Bindings []contract.Binding      `json:"bindings"`
	Entries  []contract.CatalogEntry `json:"entries"`
}

// Digest is `sha256:<hex>` over the canonical JSON of plan's graph, its sorted bindings and the
// catalog entries the graph uses, sorted by name (spec 0006). Equal digests mean the same project
// is rendered; api compares the reviewed digest with a fresh one before an apply.
func Digest(plan contract.PlanResult, catalog []contract.CatalogEntry) string {
	used := map[string]bool{}
	for _, m := range plan.Graph.Modules {
		used[m.Module] = true
	}
	in := digestInput{Graph: plan.Graph, Bindings: slices.Clone(plan.Bindings), Entries: []contract.CatalogEntry{}}
	if in.Bindings == nil {
		in.Bindings = []contract.Binding{}
	}
	slices.SortFunc(in.Bindings, func(a, b contract.Binding) int {
		if c := strings.Compare(a.Consumer, b.Consumer); c != 0 {
			return c
		}
		return strings.Compare(a.Capability, b.Capability)
	})
	for _, e := range catalog {
		if used[e.Name] {
			in.Entries = append(in.Entries, e)
		}
	}
	slices.SortFunc(in.Entries, func(a, b contract.CatalogEntry) int { return strings.Compare(a.Name, b.Name) })
	// encoding/json writes map keys sorted and struct fields in order, so the bytes are canonical;
	// the contract types hold only strings, numbers, bools, slices and maps, so it cannot fail
	data, _ := json.Marshal(in) //nolint:errchkjson // see above
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
