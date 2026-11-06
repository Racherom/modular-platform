// Package planner binds each module's slots (uses) to another instance's provides by the v1
// matching rules of spec 0012: the capability, or a provisioner that yields it, and a version
// inside the slot's range. It fills automatic bindings and the default named values of bindings
// to a provisioner into the graph. It is pure: no I/O, and the same input always gives the same
// result.
package planner

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/libs/contract"
)

// Problem codes of a plan (spec 0002, grown by 0012 and 0014).
const (
	CodeInvalidName           = "invalid_name"
	CodeUnknownModule         = "unknown_module"
	CodeUnknownUse            = "unknown_use"
	CodeUnknownProvider       = "unknown_provider"
	CodeUnknownProvide        = "unknown_provide"
	CodeSelfBinding           = "self_binding"
	CodeCapabilityNotProvided = "capability_not_provided"
	CodeVersionMismatch       = "version_mismatch"
	CodeAmbiguousProvide      = "ambiguous_provide"
	CodeNamedNotAllowed       = "named_not_allowed"
	CodeUnknownNamedField     = "unknown_named_field"
	CodeInvalidNamedValue     = "invalid_named_value"
	CodeDuplicateNamedValue   = "duplicate_named_value"
	CodeDuplicateBinding      = "duplicate_binding"
	CodeUnboundRequired       = "unbound_required"
	CodeUnmetNeed             = "unmet_need"
	CodeAmbiguousProvider     = "ambiguous_provider"
	CodeOptionalUnbound       = "optional_unbound"
	CodeBindingCycle          = "binding_cycle"
	CodeMissingTokenMapping   = "missing_token_mapping"
	CodeUnknownCapability     = "unknown_capability"
	CodeInvalidVersion        = "invalid_version"
	CodeUnknownField          = "unknown_field"
	CodeExternalFieldMissing  = "external_field_missing"
)

// maxName is the longest named value a default may be, PostgreSQL's identifier limit.
const maxName = 63

// Plan checks g against the catalog and fills in the automatic bindings and default named
// values. A v0alpha1 graph is converted to v1 first and warned about. The returned graph is a
// copy of g with those changes and nothing else. An ok plan carries its Digest.
func Plan(g contract.Graph, catalog contract.Catalog) contract.PlanResult {
	graph, converted := contract.ConvertGraph(g)
	if graph.FormatVersion == "1.0" {
		graph.FormatVersion = ""
	}
	p := planning{
		graph:   graph,
		modules: make(map[string]contract.Module, len(catalog.Modules)),
		caps:    make(map[string]contract.Capability, len(catalog.Capabilities)),
		valid:   map[string]bool{},
	}
	for _, m := range catalog.Modules {
		p.modules[m.Name] = m
	}
	for _, c := range catalog.Capabilities {
		p.caps[c.Name] = c
	}
	if converted {
		p.add(contract.Problem{
			Severity: contract.Warning, Code: contract.CodeConvertedFromV0alpha1, Path: "apiVersion",
			Message: "The graph was written in modular-platform/v0alpha1 and was converted to v1; save it again to keep the v1 text.",
		})
	}
	p.run()

	slices.SortStableFunc(p.problems, func(a, b contract.Problem) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Code, b.Code))
	})
	sortBindings(p.bindings)
	ok := !slices.ContainsFunc(p.problems, func(pr contract.Problem) bool { return pr.Severity == contract.Error })
	result := contract.PlanResult{
		Ok:       ok,
		Graph:    p.graph,
		Bindings: append([]contract.Binding{}, p.bindings...),
		Problems: append([]contract.Problem{}, p.problems...),
	}
	if ok {
		result.Digest = Digest(result, catalog)
	}
	return result
}

func sortBindings(b []contract.Binding) {
	slices.SortFunc(b, func(x, y contract.Binding) int {
		return cmp.Or(strings.Compare(x.Consumer, y.Consumer), strings.Compare(x.Slot, y.Slot))
	})
}

// offer is one provide of an instance: a module's provide or an external's.
type offer struct {
	instance, provide string
	capability        string
	version           contract.Version
	versionText       string
}

// text is how a binding or candidate names the offer.
func (o offer) text(short bool) string {
	if short {
		return o.instance
	}
	return o.instance + "." + o.provide
}

type planning struct {
	graph    contract.Graph
	modules  map[string]contract.Module
	caps     map[string]contract.Capability
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
		if !p.valid[i] {
			continue
		}
		if p.graph.Modules[i].External != nil {
			p.external(i)
		} else {
			p.instance(i)
		}
	}
	p.config(instances)
	p.namedValues()
	p.tokens()
	p.cycles(instances)
	p.envNames(instances)
	p.setupOrder(instances)
	p.hostAccess(instances)
}

// offers lists the provides of instance i in name order; nil for an unknown module.
func (p *planning) offers(i string) []offer {
	m := p.graph.Modules[i]
	var out []offer
	if m.External != nil {
		for _, q := range sortedKeys(m.External.Provides) {
			ep := m.External.Provides[q]
			v, _ := contract.ParseVersion(ep.Version)
			out = append(out, offer{instance: i, provide: q, capability: ep.Capability, version: v, versionText: ep.Version})
		}
		return out
	}
	entry, ok := p.modules[m.Module]
	if !ok {
		return nil
	}
	for _, q := range sortedKeys(entry.Provides) {
		pr := entry.Provides[q]
		v, _ := contract.ParseVersion(pr.Version)
		out = append(out, offer{instance: i, provide: q, capability: pr.Capability, version: v, versionText: pr.Version})
	}
	return out
}

// serves reports whether o's capability is c or yields c.
func (p *planning) serves(o offer, c string) bool {
	if o.capability == c {
		return true
	}
	k, ok := p.caps[o.capability]
	return ok && k.Yields != nil && k.Yields.Capability == c
}

// provisions reports whether o yields c rather than providing it directly.
func (p *planning) provisions(o offer, c string) bool {
	return o.capability != c && p.serves(o, c)
}

func (p *planning) instance(i string) {
	m := p.graph.Modules[i]
	entry, ok := p.modules[m.Module]
	if !ok {
		p.fail(CodeUnknownModule, "modules."+i+".module", "The module %q of instance %s is not in the catalog.", m.Module, i)
		return
	}
	slots := entry.Slots()

	var uses contract.Uses
	if m.Uses != nil {
		uses = *m.Uses
	}
	for _, s := range sortedKeys(uses) {
		if _, declared := slots[s]; !declared {
			p.fail(CodeUnknownUse, usePath(i, s), "Instance %s binds %s, but module %s has no such slot.", i, s, m.Module)
		}
	}

	bound := map[string]string{} // provide text to the first slot bound to it
	for _, s := range sortedKeys(slots) {
		slot := slots[s]
		r, _ := contract.ParseRange(slot.Versions) // the catalog check refused a bad range
		path := usePath(i, s)
		b, present := uses[s]
		var (
			o    *offer
			auto bool
		)
		switch {
		case present && b != nil:
			o = p.explicit(i, s, slot, r, b)
		case present && !slot.Optional:
			p.fail(CodeUnboundRequired, path, "Instance %s needs %s, so it cannot be set to null.", i, s)
		case present:
			// an optional slot left unbound on purpose
		default:
			o, auto = p.undecided(i, s, slot, r), true
		}
		if o == nil {
			continue
		}
		provisioned := p.provisions(*o, slot.Capability)
		// a provisioner makes something new per binding (AC-4), so only a direct provide
		// bound twice is a duplicate
		key := o.instance + "." + o.provide
		if first, dup := bound[key]; dup && !provisioned {
			p.fail(CodeDuplicateBinding, path, "Instance %s binds both %s and %s to %s; bind each slot to its own provide.", i, first, s, key)
			continue
		}
		bound[key] = s
		p.bindings = append(p.bindings, contract.Binding{
			Consumer: i, Slot: s, Capability: slot.Capability, Provider: o.instance, Provide: o.provide,
			Auto: auto, Provisioned: provisioned,
		})
	}
}

// explicit checks a binding the graph wrote and records it. It returns the offer it binds to, or
// nil when the binding has a problem.
func (p *planning) explicit(i, s string, slot contract.Slot, r contract.Range, b *contract.GraphBinding) *offer {
	path := usePath(i, s)
	pv, q := b.Instance(), b.Provide()
	if _, exists := p.graph.Modules[pv]; !exists {
		p.fail(CodeUnknownProvider, path, "Instance %s binds %s to %s, which is not in the graph.", i, s, pv)
		return nil
	}
	if pv == i {
		p.fail(CodeSelfBinding, path, "Instance %s binds %s to itself.", i, s)
		return nil
	}
	if !p.valid[pv] {
		return nil // the provider already has its own invalid_name problem
	}
	offers := p.offers(pv)
	if offers == nil && p.graph.Modules[pv].External == nil {
		return nil // the provider already has its own unknown_module problem
	}
	c := slot.Capability
	var o offer
	if q != "" {
		idx := slices.IndexFunc(offers, func(o offer) bool { return o.provide == q })
		switch {
		case idx < 0:
			p.fail(CodeUnknownProvide, path, "Instance %s binds %s to %s.%s, but %s has no provide %s.", i, s, pv, q, pv, q)
			return nil
		case !p.serves(offers[idx], c):
			p.fail(CodeCapabilityNotProvided, path, "Instance %s binds %s to %s.%s, which does not provide %s.", i, s, pv, q, c)
			return nil
		case !r.Contains(offers[idx].version):
			p.versionMismatch(path, i, slot, []offer{offers[idx]}, nil)
			return nil
		}
		o = offers[idx]
	} else {
		var serving, matching []offer
		for _, o := range offers {
			if p.serves(o, c) {
				serving = append(serving, o)
				if r.Contains(o.version) {
					matching = append(matching, o)
				}
			}
		}
		switch {
		case len(serving) == 0:
			p.fail(CodeCapabilityNotProvided, path, "Instance %s binds %s to %s, which does not provide %s.", i, s, pv, c)
			return nil
		case len(matching) == 0:
			p.versionMismatch(path, i, slot, serving, nil)
			return nil
		case len(matching) > 1:
			candidates := make([]string, len(matching))
			for n, m := range matching {
				candidates[n] = m.text(false)
			}
			p.add(contract.Problem{
				Severity: contract.Error, Code: CodeAmbiguousProvide, Path: path, Candidates: candidates,
				Message: fmt.Sprintf("Instance %s binds %s to %s, which has several provides of %s: %s; name one.", i, s, pv, c, strings.Join(candidates, ", ")),
			})
			return nil
		}
		o = matching[0]
	}

	provisioned := p.provisions(o, c)
	if len(b.Named) > 0 {
		if !provisioned {
			p.fail(CodeNamedNotAllowed, path, "Instance %s names values for %s, but %s provides %s directly, so there is nothing to make.", i, s, o.text(false), c)
			return nil
		}
		y := p.caps[o.capability].Yields
		target := p.caps[y.Capability]
		bad := false
		for _, k := range sortedKeys(b.Named) {
			switch {
			case !slices.Contains(y.Named, k):
				p.fail(CodeUnknownNamedField, path+"."+k, "Provisioner %s names only %s, not %s.", o.capability, strings.Join(y.Named, ", "), k)
				bad = true
			default:
				if msg := contract.FieldValueProblem(target.Fields[k], b.Named[k]); msg != "" {
					p.fail(CodeInvalidNamedValue, path+"."+k, "The %s of %s is invalid: %s.", k, s, msg)
					bad = true
				}
			}
		}
		if bad {
			return nil
		}
	}
	return &o
}

// versionMismatch reports offers that serve the slot's capability outside its range; short
// lists the instances whose one serving provide may go without its name.
func (p *planning) versionMismatch(path, i string, slot contract.Slot, offers []offer, short map[string]bool) {
	candidates := make([]string, len(offers))
	versions := make([]string, len(offers))
	for n, o := range offers {
		candidates[n] = o.text(short[o.instance])
		versions[n] = candidates[n] + " " + o.versionText
	}
	p.add(contract.Problem{
		Severity: contract.Error, Code: CodeVersionMismatch, Path: path, Candidates: candidates,
		Message: fmt.Sprintf("Instance %s needs %s %s, but only %s is offered.", i, slot.Capability, slot.Versions, strings.Join(versions, ", ")),
	})
}

// undecided handles a slot the graph leaves out: auto bind the one match, or report.
func (p *planning) undecided(i, s string, slot contract.Slot, r contract.Range) *offer {
	path := usePath(i, s)
	c := slot.Capability
	var matches, outside []offer
	short := map[string]bool{} // instances with exactly one provide serving c
	for _, name := range sortedKeys(p.graph.Modules) {
		if name == i || !p.valid[name] {
			continue
		}
		var serving []offer
		for _, o := range p.offers(name) {
			if !p.serves(o, c) {
				continue
			}
			serving = append(serving, o)
			if r.Contains(o.version) {
				matches = append(matches, o)
			} else {
				outside = append(outside, o)
			}
		}
		short[name] = len(serving) == 1
	}
	texts := func(os []offer) []string {
		out := make([]string, len(os))
		for n, o := range os {
			out[n] = o.text(short[o.instance])
		}
		return out
	}
	switch {
	case slot.Optional && len(matches) > 0:
		candidates := texts(matches)
		p.add(contract.Problem{
			Severity: contract.Warning, Code: CodeOptionalUnbound, Path: path, Candidates: candidates,
			Message: fmt.Sprintf("Instance %s can use %s from %s; bind it or set it to null.", i, s, strings.Join(candidates, ", ")),
		})
	case slot.Optional:
		// nothing could serve it, and nothing needs to
	case len(matches) == 0 && len(outside) > 0:
		p.versionMismatch(path, i, slot, outside, short)
	case len(matches) == 0:
		p.fail(CodeUnmetNeed, path, "Instance %s needs %s for %s, and no instance in the graph provides it.", i, c, s)
	case len(matches) == 1:
		o := matches[0]
		m := p.graph.Modules[i]
		if m.Uses == nil {
			m.Uses = &contract.Uses{}
		}
		(*m.Uses)[s] = &contract.GraphBinding{Provider: o.text(short[o.instance])}
		p.graph.Modules[i] = m
		return &o
	default:
		candidates := texts(matches)
		p.add(contract.Problem{
			Severity: contract.Error, Code: CodeAmbiguousProvider, Path: path, Candidates: candidates,
			Message: fmt.Sprintf("Instance %s needs %s for %s, and several instances provide it: %s; bind one.", i, c, s, strings.Join(candidates, ", ")),
		})
	}
	return nil
}

// namedValues fills the default named values of every binding to a provisioner into the graph
// (spec 0014), then checks that each value is unique per provisioner provide and that no login
// equals the provider's admin user.
func (p *planning) namedValues() {
	sortBindings(p.bindings)
	seen := map[string]map[string]string{} // provide text, field=value to the first binding path
	for n := range p.bindings {
		b := &p.bindings[n]
		if !b.Provisioned {
			continue
		}
		offerCap := p.offerCapability(b.Provider, b.Provide)
		y := p.caps[offerCap].Yields
		target := p.caps[y.Capability]
		m := p.graph.Modules[b.Consumer]
		gb := (*m.Uses)[b.Slot]
		path := usePath(b.Consumer, b.Slot)
		for _, f := range y.Named {
			if _, ok := gb.Named[f]; ok {
				continue
			}
			if gb.Named == nil {
				gb.Named = map[string]string{}
			}
			v := DefaultName(b.Consumer, b.Slot)
			gb.Named[f] = v
			b.Named = true
			if msg := contract.FieldValueProblem(target.Fields[f], v); msg != "" {
				p.fail(CodeInvalidNamedValue, path+"."+f, "The default %s %q of %s breaks its pattern; name it in the graph.", f, v, b.Slot)
			}
		}
		key := b.Provider + "." + b.Provide
		if seen[key] == nil {
			seen[key] = map[string]string{}
		}
		for _, f := range y.Named {
			v := gb.Named[f]
			if first, dup := seen[key][f+"="+v]; dup {
				p.fail(CodeDuplicateNamedValue, path+"."+f, "The %s %q of %s is already used by %s on %s.", f, v, b.Slot, first, key)
				continue
			}
			seen[key][f+"="+v] = path
		}
		if y.Login != "" && gb.Named[y.Login] == p.adminLogin(b.Provider, b.Provide, y.Login) {
			p.fail(CodeInvalidNamedValue, path+"."+y.Login, "The %s %q of %s is the provider's admin login; name another.", y.Login, gb.Named[y.Login], b.Slot)
		}
	}
}

func (p *planning) offerCapability(instance, provide string) string {
	for _, o := range p.offers(instance) {
		if o.provide == provide {
			return o.capability
		}
	}
	return ""
}

// adminLogin is the literal value of the provisioner's admin_<login> field, the login the
// provider itself signs in with, or "" when the provide does not give it as a value.
func (p *planning) adminLogin(instance, provide, login string) string {
	m := p.graph.Modules[instance]
	field := "admin_" + login
	if m.External != nil {
		return m.External.Provides[provide].Fields[field]
	}
	return p.modules[m.Module].Provides[provide].Fields[field].Value
}

// DefaultName is the named value the planner writes for a binding that leaves it out (spec
// 0014): <consumer>_<slot> with - as _, cut to 54 characters plus _ and 8 hex digits of its
// SHA-256 when longer than 63.
func DefaultName(consumer, slot string) string {
	name := strings.ReplaceAll(strings.ToLower(consumer+"_"+slot), "-", "_")
	if len(name) <= maxName {
		return name
	}
	sum := sha256.Sum256([]byte(consumer + "." + slot))
	return name[:54] + "_" + hex.EncodeToString(sum[:])[:8]
}

// external checks an external instance's provides: known capability, a version, every
// required non secret field given, no secret written in the graph, values within their rules.
func (p *planning) external(i string) {
	ext := p.graph.Modules[i].External
	for _, q := range sortedKeys(ext.Provides) {
		ep := ext.Provides[q]
		base := "modules." + i + ".external.provides." + q
		if !contract.ValidName(q) {
			p.fail(CodeInvalidName, base, "The provide name %q must be a lowercase DNS label and not a YAML reserved word.", q)
		}
		if _, ok := contract.ParseVersion(ep.Version); !ok {
			p.fail(CodeInvalidVersion, base+".version", "The version %q of %s.%s is not a version like 16.4.", ep.Version, i, q)
		}
		k, ok := p.caps[ep.Capability]
		if !ok {
			p.fail(CodeUnknownCapability, base+".capability", "The capability %q of %s.%s is not in the catalog.", ep.Capability, i, q)
			continue
		}
		for _, f := range sortedKeys(ep.Fields) {
			field, known := k.Fields[f]
			switch {
			case !known:
				p.fail(CodeUnknownField, base+".fields."+f, "Capability %s has no field %s.", ep.Capability, f)
			case field.IsFile():
				p.fail(CodeUnknownField, base+".fields."+f, "%s is a %s; place it as the file secrets/%s/%s_%s instead of writing it here.", f, field.Type, i, q, f)
			default:
				if msg := contract.FieldValueProblem(field, ep.Fields[f]); msg != "" {
					p.fail(CodeInvalidNamedValue, base+".fields."+f, "The %s of %s.%s is invalid: %s.", f, i, q, msg)
				}
			}
		}
		for _, f := range sortedKeys(k.Fields) {
			field := k.Fields[f]
			if _, given := ep.Fields[f]; !given && !field.IsFile() && !field.Optional && field.ToConsumer() {
				p.fail(CodeExternalFieldMissing, base+".fields."+f, "External %s.%s needs a value for %s (%s).", i, q, f, field.Description)
			}
		}
	}
}

// tokens reports each binding to a provide that checks a token per binding (spec 0005) whose
// consumer maps no token, so the consumer could never prove who calls.
func (p *planning) tokens() {
	for _, b := range p.bindings {
		pm := p.graph.Modules[b.Provider]
		if pm.External != nil {
			continue
		}
		provider := p.modules[pm.Module]
		consumer := p.modules[p.graph.Modules[b.Consumer].Module]
		slot := consumer.Slots()[b.Slot]
		if provider.Provides[b.Provide].Auth == contract.BindingToken && slot.Env["token"] == "" && slot.Files["token"] == "" {
			p.fail(CodeMissingTokenMapping, usePath(b.Consumer, b.Slot),
				"Instance %s uses %s from %s, which checks a token per binding, but module %s maps no token for %s.",
				b.Consumer, b.Slot, b.Provider, consumer.Name, b.Slot)
		}
	}
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

func usePath(i, s string) string {
	return "modules." + i + ".uses." + s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
