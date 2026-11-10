package check

import (
	"fmt"
	"slices"

	"github.com/racherom/modular-platform/libs/contract"
)

// Problem codes of install time (spec 0013).
const (
	CodeEnvNameCollision = "env_name_collision"
	CodeSetupOrder       = "setup_order"
	// CodeHostAccess warns of an instance whose module publishes ports, mounts host paths or
	// adds a Linux capability a deployed graph refuses.
	CodeHostAccess = "host_access"
)

// hostAccess warns at each instance whose module publishes a port, mounts a host path (spec
// 0013, Host access) or adds a Linux capability other than NET_BIND_SERVICE (spec 0022), in
// public and internal mode alike: a downloaded zip gives it that access, while a deployed graph
// is refused it. One warning per instance names every reason.
func (p *checking) hostAccess(instances []string) {
	for _, i := range instances {
		m, ok := p.entry(i)
		if !ok {
			continue
		}
		reasons := m.HostAccess()
		if reasons == "" {
			continue
		}
		p.add(contract.Problem{
			Severity: contract.Warning, Code: CodeHostAccess, Path: "modules." + i,
			Message: fmt.Sprintf("Instance %s %s, so it reaches the host it runs on; a deployed graph refuses this.", i, reasons),
		})
	}
}

// envNames reports two config values that become one Compose variable, possible because an
// instance or value name may hold -- (spec 0013, "Asked values"). It reports the later pair in
// instance then value order, at its instance.
func (p *checking) envNames(instances []string) {
	seen := map[string]string{} // variable to "value of instance"
	for _, i := range instances {
		m, ok := p.entry(i)
		if !ok {
			continue
		}
		for _, v := range m.ComposeValues() {
			name := contract.ComposeVar(i, v)
			this := fmt.Sprintf("config value %s of %s", v, i)
			if first, taken := seen[name]; taken {
				p.fail(CodeEnvNameCollision, "modules."+i, "The %s and the %s both become the Compose variable %s; rename one of the instances.", first, this, name)
				continue
			}
			seen[name] = this
		}
	}
}

// entry returns the catalog entry of module instance i, when i is a valid instance of a known
// module.
func (p *checking) entry(i string) (contract.Module, bool) {
	gm := p.graph.Modules[i]
	if !p.valid[i] || gm.External != nil {
		return contract.Module{}, false
	}
	m, ok := p.modules[gm.Module]
	return m, ok
}

// setupOrder reports each read of a setup step that setup.sh cannot have made when the step runs
// (spec 0013): a step reads only the files steps 2 to 4 make (asked and generated files), its
// own secrets, and the outputs of steps before it; the steps of a provider run before those of
// its consumers.
func (p *checking) setupOrder(instances []string) {
	bound := map[string]contract.Binding{}     // consumer.slot to its binding
	clients := map[string][]contract.Binding{} // provider.provide to the bindings to it
	for _, b := range p.bindings {
		bound[b.Consumer+"."+b.Slot] = b
		clients[b.Provider+"."+b.Provide] = append(clients[b.Provider+"."+b.Provide], b)
	}
	o := orderCheck{checking: p, bound: bound}
	for _, i := range instances {
		m, ok := p.entry(i)
		if !ok {
			continue
		}
		for k, step := range m.Setup {
			for _, r := range step.Reads {
				var why string
				switch {
				case r.Secret != "":
					why = ownSecret(m, k, r.Secret)
				case r.Slot != "":
					if b, ok := bound[i+"."+r.Slot]; ok {
						why = o.boundFile(b, r.Field, 0)
					}
				case r.Provide != "":
					for _, b := range clients[i+"."+r.Provide] {
						if why = o.clientFile(m, k, b, r.Field); why != "" {
							break
						}
					}
				}
				if why != "" {
					p.fail(CodeSetupOrder, "modules."+i, "Setup step %s of %s reads %s, %s.", step.Name, i, readText(r), why)
				}
			}
		}
	}
}

// readText names a setup step read the way the entry writes it.
func readText(r contract.SetupRead) string {
	switch {
	case r.Secret != "":
		return "secret " + r.Secret
	case r.Slot != "":
		return "field " + r.Field + " of slot " + r.Slot
	}
	return "field " + r.Field + " of provide " + r.Provide
}

// ownSecret tells why step k of m cannot read its own secret n yet, or is "": a generated or
// asked secret (or secret config value) exists before any step runs; a secret a step writes
// exists only after that step.
func ownSecret(m contract.Module, k int, n string) string {
	s, ok := m.SecretEntry(n)
	if !ok {
		if v, isValue := m.Config[n]; isValue && v.Type == contract.ConfigValueTypeSecret && (v.Generate || v.Asked()) {
			return ""
		}
		return "which the module does not declare as a secret"
	}
	if s.From != contract.SecretFromSetup {
		return ""
	}
	for before := range k {
		if slices.Contains(m.SetupWrites(before), n) {
			return ""
		}
	}
	return "which no setup step before it writes"
}

// orderCheck follows a binding's field to the file behind it.
type orderCheck struct {
	*checking
	bound map[string]contract.Binding
}

// maxDepth bounds how many {uses} pass throughs a field may follow; the checks refuse cycles.
const maxDepth = 8

// boundFile tells why field f of binding b is not a file setup.sh has made before its consumer's
// steps run, or is "".
func (o orderCheck) boundFile(b contract.Binding, f string, depth int) string {
	if depth > maxDepth {
		return "which passes through too many bindings"
	}
	gm := o.graph.Modules[b.Provider]
	if gm.External != nil {
		k := o.caps[gm.External.Provides[b.Provide].Capability]
		if !k.Fields[f].IsFile() {
			return "which is text, and a step reads only files"
		}
		return "a file you place, which setup.sh checks only after the setup steps"
	}
	provider := o.modules[gm.Module]
	pr := provider.Provides[b.Provide]
	if b.Provisioned {
		y := o.caps[pr.Capability].Yields
		field := o.caps[b.Capability].Fields[f]
		src, ok := y.Fields[f]
		switch {
		case !ok:
			return "" // not yielded, so not delivered
		case !field.IsFile():
			return "which is text, and a step reads only files"
		case src.Kind == contract.YieldOutput:
			return "a file its provision job writes only at docker compose up"
		case src.Kind == contract.YieldAdmin:
			return o.providerFile(b, pr, src.Admin, depth)
		}
		return "" // generated per binding, before any step
	}
	return o.providerFile(b, pr, f, depth)
}

// providerFile follows field f of provide pr of the provider of b to its source.
func (o orderCheck) providerFile(b contract.Binding, pr contract.Provide, f string, depth int) string {
	if !o.caps[pr.Capability].Fields[f].IsFile() {
		return "which is text, and a step reads only files"
	}
	src, ok := pr.Fields[f]
	switch {
	case !ok:
		return "" // no source, so nothing is mounted
	case src.Platform == contract.FieldSourcePlatformCa:
		return "a certificate made only at docker compose up"
	case src.Volume != "":
		return "a file the provider writes into its data folder at run time"
	case src.Uses != "":
		next, bound := o.bound[b.Provider+"."+src.Uses]
		if !bound {
			return ""
		}
		return o.boundFile(next, src.Field, depth+1)
	}
	// the provider's own secret or a per binding file: the provider's steps run first
	return ""
}

// clientFile tells why the file of one client b that a for_each step k of m reads (field f of
// its provide) is not there yet, or is "".
func (o orderCheck) clientFile(m contract.Module, k int, b contract.Binding, f string) string {
	pr := m.Provides[b.Provide]
	field := o.caps[pr.Capability].Fields[f]
	if field.Direction == contract.Both {
		if src := pr.Fields[f]; src.Secret != "" {
			return ownSecret(m, k, src.Secret)
		}
		return "" // generated per binding
	}
	consumer := o.modules[o.graph.Modules[b.Consumer].Module]
	src, ok := consumer.Slots()[b.Slot].Fields[f]
	if !ok || src.Secret == "" {
		return ""
	}
	if s, declared := consumer.SecretEntry(src.Secret); declared && s.From == contract.SecretFromSetup {
		return fmt.Sprintf("which client %s writes in a setup step that runs after this one", b.Consumer)
	}
	return ""
}
