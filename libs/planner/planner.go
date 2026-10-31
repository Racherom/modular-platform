// Package planner wires each module's needs (uses) to another module's offers (provides) by the
// v0alpha1 matching rules of spec 0002. It is pure: no I/O, and the same input always gives the
// same result.
package planner

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/libs/contract"
)

// Problem codes of a plan.
const (
	CodeInvalidName           = "invalid_name"
	CodeUnknownModule         = "unknown_module"
	CodeUnknownUse            = "unknown_use"
	CodeUnknownProvider       = "unknown_provider"
	CodeSelfBinding           = "self_binding"
	CodeCapabilityNotProvided = "capability_not_provided"
	CodeUnboundRequired       = "unbound_required"
	CodeUnmetNeed             = "unmet_need"
	CodeAmbiguousProvider     = "ambiguous_provider"
	CodeOptionalUnbound       = "optional_unbound"
	CodeBindingCycle          = "binding_cycle"
	CodeMissingTokenMapping   = "missing_token_mapping"
)

// Plan checks g against the catalog entries and fills in the automatic bindings. The returned
// graph is a copy of g with those bindings written in and nothing else changed.
func Plan(g contract.Graph, catalog []contract.CatalogEntry) contract.PlanResult {
	p := planning{
		graph:   contract.CloneGraph(g),
		entries: make(map[string]contract.CatalogEntry, len(catalog)),
		valid:   map[string]bool{},
	}
	for _, e := range catalog {
		p.entries[e.Name] = e
	}
	p.run()

	slices.SortStableFunc(p.problems, func(a, b contract.Problem) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Code, b.Code))
	})
	slices.SortFunc(p.bindings, func(a, b contract.Binding) int {
		return cmp.Or(strings.Compare(a.Consumer, b.Consumer), strings.Compare(a.Capability, b.Capability))
	})
	ok := !slices.ContainsFunc(p.problems, func(pr contract.Problem) bool { return pr.Severity == contract.Error })
	return contract.PlanResult{
		Ok:       ok,
		Graph:    p.graph,
		Bindings: append([]contract.Binding{}, p.bindings...),
		Problems: append([]contract.Problem{}, p.problems...),
	}
}

type planning struct {
	graph    contract.Graph
	entries  map[string]contract.CatalogEntry
	valid    map[string]bool // instances with a valid name
	bindings []contract.Binding
	problems []contract.Problem
}

func (p *planning) run() {
	if !contract.ValidName(p.graph.Name) {
		p.fail(CodeInvalidName, "name", "The graph name %q must be a lowercase DNS label of at most 63 characters and not a YAML reserved word.", p.graph.Name)
	}

	instances := sortedKeys(p.graph.Modules)
	for _, i := range instances {
		if contract.ValidName(i) {
			p.valid[i] = true
			continue
		}
		p.fail(CodeInvalidName, "modules."+i, "The instance name %q must be a lowercase DNS label of at most 63 characters and not a YAML reserved word.", i)
	}

	for _, i := range instances {
		if p.valid[i] {
			p.instance(i)
		}
	}
	p.tokens()
	p.cycles(instances)
}

// tokens reports each binding to a provider that checks a token per binding (spec 0005) whose
// consumer maps no token_file, so the consumer could never prove who calls.
func (p *planning) tokens() {
	for _, b := range p.bindings {
		provider := p.entries[p.graph.Modules[b.Provider].Module]
		consumer := p.entries[p.graph.Modules[b.Consumer].Module]
		if provider.Provides[b.Capability].Auth == contract.BindingToken && consumer.Uses[b.Capability].Env.TokenFile == "" {
			p.fail(CodeMissingTokenMapping, usePath(b.Consumer, b.Capability),
				"Instance %s uses %s from %s, which checks a token per binding, but module %s maps no token_file for %s.",
				b.Consumer, b.Capability, b.Provider, consumer.Name, b.Capability)
		}
	}
}

func (p *planning) instance(i string) {
	m := p.graph.Modules[i]
	entry, ok := p.entries[m.Module]
	if !ok {
		p.fail(CodeUnknownModule, "modules."+i+".module", "The module %q of instance %s is not in the catalog.", m.Module, i)
		return
	}

	var uses contract.Uses
	if m.Uses != nil {
		uses = *m.Uses
	}
	for _, c := range sortedKeys(uses) {
		if _, declared := entry.Uses[c]; !declared {
			p.fail(CodeUnknownUse, usePath(i, c), "Instance %s binds %s, but module %s does not use that capability.", i, c, m.Module)
		}
	}

	for _, c := range sortedKeys(entry.Uses) {
		need := entry.Uses[c]
		path := usePath(i, c)
		provider, present := uses[c]
		switch {
		case present && provider != nil:
			p.explicit(i, c, *provider)
		case present && !need.Optional:
			p.fail(CodeUnboundRequired, path, "Instance %s needs %s, so it cannot be set to null.", i, c)
		case present:
			// an optional need left unbound on purpose
		default:
			p.undecided(i, c, need.Optional)
		}
	}
}

func (p *planning) explicit(i, c, provider string) {
	path := usePath(i, c)
	pm, exists := p.graph.Modules[provider]
	switch {
	case !exists:
		p.fail(CodeUnknownProvider, path, "Instance %s binds %s to %s, which is not in the graph.", i, c, provider)
	case provider == i:
		p.fail(CodeSelfBinding, path, "Instance %s binds %s to itself.", i, c)
	case !p.valid[provider]:
		// the provider already has its own invalid_name problem
	default:
		entry, known := p.entries[pm.Module]
		if !known {
			return // the provider already has its own unknown_module problem
		}
		if _, provides := entry.Provides[c]; !provides {
			p.fail(CodeCapabilityNotProvided, path, "Instance %s binds %s to %s, but module %s does not provide it.", i, c, provider, pm.Module)
			return
		}
		p.bindings = append(p.bindings, contract.Binding{Consumer: i, Capability: c, Provider: provider})
	}
}

func (p *planning) undecided(i, c string, optional bool) {
	path := usePath(i, c)
	candidates := p.candidates(i, c)
	switch {
	case optional && len(candidates) > 0:
		p.add(contract.Problem{
			Severity: contract.Warning, Code: CodeOptionalUnbound, Path: path, Candidates: candidates,
			Message: fmt.Sprintf("Instance %s can use %s from %s; bind it or set it to null.", i, c, strings.Join(candidates, ", ")),
		})
	case optional:
		// nothing could provide it, and nothing needs to
	case len(candidates) == 0:
		p.fail(CodeUnmetNeed, path, "Instance %s needs %s, and no instance in the graph provides it.", i, c)
	case len(candidates) == 1:
		m := p.graph.Modules[i]
		if m.Uses == nil {
			m.Uses = &contract.Uses{}
		}
		provider := candidates[0]
		(*m.Uses)[c] = &provider
		p.graph.Modules[i] = m
		p.bindings = append(p.bindings, contract.Binding{Consumer: i, Capability: c, Provider: provider, Auto: true})
	default:
		p.add(contract.Problem{
			Severity: contract.Error, Code: CodeAmbiguousProvider, Path: path, Candidates: candidates,
			Message: fmt.Sprintf("Instance %s needs %s, and several instances provide it: %s; bind one.", i, c, strings.Join(candidates, ", ")),
		})
	}
}

// candidates lists, sorted, every other instance with a valid name and a known module that
// provides c.
func (p *planning) candidates(i, c string) []string {
	var out []string
	for _, name := range sortedKeys(p.graph.Modules) {
		if name == i || !p.valid[name] {
			continue
		}
		entry, known := p.entries[p.graph.Modules[name].Module]
		if _, provides := entry.Provides[c]; known && provides {
			out = append(out, name)
		}
	}
	return out
}

// cycles reports each group of instances that bind each other in a circle, once, at the first
// instance of the group in name order.
func (p *planning) cycles(instances []string) {
	edges := map[string][]string{}
	for _, b := range p.bindings {
		edges[b.Consumer] = append(edges[b.Consumer], b.Provider)
	}
	for _, from := range instances {
		slices.Sort(edges[from])
		edges[from] = slices.Compact(edges[from])
	}

	for _, group := range stronglyConnected(instances, edges) {
		if len(group) < 2 {
			continue
		}
		first := slices.Min(group)
		cycle := cycleThrough(first, group, edges)
		p.fail(CodeBindingCycle, "modules."+first, "Instances bind each other in a cycle: %s.", strings.Join(cycle, " → "))
	}
}

func (p *planning) fail(code, path, format string, args ...any) {
	p.add(contract.Problem{Severity: contract.Error, Code: code, Path: path, Message: fmt.Sprintf(format, args...)})
}

func (p *planning) add(pr contract.Problem) {
	p.problems = append(p.problems, pr)
}

func usePath(i, c string) string {
	return "modules." + i + ".uses." + c
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
