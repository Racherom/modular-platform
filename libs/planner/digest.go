package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/libs/contract"
)

// digestInput is what a digest covers: the planned v1 graph, its bindings, the module entries
// the graph uses and the capability files those entries name, so a changed entry or capability
// (a new image tag, a new job script) changes the digest too.
type digestInput struct {
	Graph        contract.Graph        `json:"graph"`
	Bindings     []contract.Binding    `json:"bindings"`
	Modules      []contract.Module     `json:"modules"`
	Capabilities []contract.Capability `json:"capabilities"`
}

// Digest is `sha256:<hex>` over the canonical JSON of plan's graph, its sorted bindings, the
// modules the graph uses and the capabilities they name (with the capability each yields),
// sorted by name (spec 0011). Equal digests mean the same project is rendered; api compares the
// reviewed digest with a fresh one before an apply.
func Digest(plan contract.PlanResult, catalog contract.Catalog) string {
	used := map[string]bool{}
	named := map[string]bool{}
	for _, m := range plan.Graph.Modules {
		used[m.Module] = true
		if m.External != nil {
			for _, ep := range m.External.Provides {
				named[ep.Capability] = true
			}
		}
	}
	in := digestInput{
		Graph:        plan.Graph,
		Bindings:     slices.Clone(plan.Bindings),
		Modules:      []contract.Module{},
		Capabilities: []contract.Capability{},
	}
	if in.Bindings == nil {
		in.Bindings = []contract.Binding{}
	}
	sortBindings(in.Bindings)
	for _, m := range catalog.Modules {
		if !used[m.Name] {
			continue
		}
		in.Modules = append(in.Modules, m)
		for _, pr := range m.Provides {
			named[pr.Capability] = true
		}
		for _, s := range m.Slots() {
			named[s.Capability] = true
		}
	}
	for _, c := range catalog.Capabilities {
		if named[c.Name] && c.Yields != nil {
			named[c.Yields.Capability] = true
		}
	}
	for _, c := range catalog.Capabilities {
		if named[c.Name] {
			in.Capabilities = append(in.Capabilities, c)
		}
	}
	slices.SortFunc(in.Modules, func(a, b contract.Module) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(in.Capabilities, func(a, b contract.Capability) int { return strings.Compare(a.Name, b.Name) })
	// encoding/json writes map keys sorted and struct fields in order, so the bytes are canonical;
	// the contract types hold only strings, numbers, bools, slices, maps and marshalers of those,
	// so it cannot fail
	data, _ := json.Marshal(in) //nolint:errchkjson // see above
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
