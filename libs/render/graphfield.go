package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// CodeInvalidBinding refuses a graph value the flavor does not render (spec 0026, AC-10).
const CodeInvalidBinding = "invalid_binding"

// graphFields lists the fields of capability k a graph may set on a binding: to_provider text
// fields marked graph: true (spec 0026), in name order.
func graphFields(k contract.Capability) []string {
	var out []string
	for _, f := range sortedKeys(k.Fields) {
		if field := k.Fields[f]; field.Graph && field.ToProvider() && !field.IsFile() {
			out = append(out, f)
		}
	}
	return out
}

// graphRefusals lists the graph set values of direct bindings, which only the platform flavor
// renders until the exposure spec: one invalid_binding per value, at its path.
func graphRefusals(result contract.CheckResult) []contract.Problem {
	var out []contract.Problem
	for _, b := range result.Bindings {
		if b.Provisioned {
			continue
		}
		uses := result.Graph.Modules[b.Consumer].Uses
		if uses == nil || (*uses)[b.Slot] == nil {
			continue
		}
		for _, f := range sortedKeys((*uses)[b.Slot].Values) {
			out = append(out, contract.Problem{
				Severity: contract.Error, Code: CodeInvalidBinding,
				Path:    "modules." + b.Consumer + ".uses." + b.Slot + "." + f,
				Message: fmt.Sprintf("Instance %s sets %s on %s; an ingress host is for the platform graph only until the exposure spec.", b.Consumer, f, b.Slot),
			})
		}
	}
	slices.SortFunc(out, func(a, b contract.Problem) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// refuseGraphValues refuses, in every flavor but the platform's, a value the graph sets on a
// direct binding (spec 0026, AC-10).
func refuseGraphValues(result contract.CheckResult, f flavor) error {
	if f == flavorPlatform {
		return nil
	}
	problems := graphRefusals(result)
	if len(problems) == 0 {
		return nil
	}
	name := "static"
	if f == flavorManaged {
		name = ""
	}
	return &RefusedError{Problems: problems, flavor: name}
}

// bindAll delivers every binding in delivery order, then gives each provider the aliases of the
// hosts its bindings carry.
func (r *renderer) bindAll(instances []string, bindings []contract.Binding) error {
	for _, b := range deliveryOrder(instances, bindings) {
		if err := r.binding(b); err != nil {
			return err
		}
	}
	return r.hostAliases(bindings)
}

// graphText is the unfilled text of graph field f of binding b: the graph's value, which
// replaces the consumer entry's source, else the entry's literal (spec 0026).
func (r *renderer) graphText(b contract.Binding, f string) (string, bool) {
	if uses := r.g.Modules[b.Consumer].Uses; uses != nil && (*uses)[b.Slot] != nil {
		if v, ok := (*uses)[b.Slot].Values[f]; ok {
			return v, true
		}
	}
	src, ok := r.modules[r.g.Modules[b.Consumer].Module].Slots()[b.Slot].Fields[f]
	if !ok || src.Value == "" {
		return "", false
	}
	return src.Value, true
}

// fill replaces each {field} token of a graph value with the provider's value of that field (spec
// 0026): the Compose variable of its config value, or with caddy the {$ENV} reference the
// ingress's own file reads. The result is raw (a Compose reference) when it holds a token and not
// caddy; a literal beside a reference is escaped.
func (r *renderer) fill(b contract.Binding, text string, caddy bool) (value, error) {
	tokens := usesFieldRe.FindAllStringSubmatchIndex(text, -1)
	if len(tokens) == 0 {
		return value{text: text}, nil
	}
	e := r.end(b.Provider, b.Provide)
	var out strings.Builder
	last := 0
	for _, loc := range tokens {
		name := text[loc[2]:loc[3]]
		src := e.provide.Fields[name]
		if e.external != nil || src.Config == "" || src.Format != "" {
			return value{}, fmt.Errorf("%w: token {%s} of %s.%s, which this release fills from a config value only", ErrNotRendered, name, b.Provider, b.Provide)
		}
		cv := e.module.Config[src.Config]
		if caddy {
			out.WriteString(text[last:loc[0]])
			out.WriteString("{$" + cv.Env + "}")
		} else {
			out.WriteString(escape(text[last:loc[0]]))
			out.WriteString(r.composeVar(b.Provider, src.Config, cv))
		}
		last = loc[1]
	}
	if caddy {
		out.WriteString(text[last:])
		return value{text: out.String()}, nil
	}
	out.WriteString(escape(text[last:]))
	return value{text: out.String(), raw: true}, nil
}

// toProviderValues resolves the graph fields of binding b for the consumer's own use, as Compose
// text: {<slot>.<field>} in its environment and {uses, format} sources read them.
func (r *renderer) toProviderValues(b contract.Binding, k contract.Capability) (map[string]value, error) {
	out := map[string]value{}
	for _, f := range graphFields(k) {
		text, ok := r.graphText(b, f)
		if !ok {
			continue
		}
		v, err := r.fill(b, text, false)
		if err != nil {
			return nil, err
		}
		out[f] = v
	}
	return out, nil
}

// hostAliases gives each provider instance one network alias per distinct filled host among its
// bindings, beside its entry's aliases, on every network it joins (spec 0026), so a consumer
// inside the stack reaches the host through the provider.
func (r *renderer) hostAliases(bindings []contract.Binding) error {
	for _, b := range bindings {
		if r.g.Modules[b.Provider].External != nil || b.Provisioned {
			continue
		}
		text, ok := r.graphText(b, "host")
		if !ok || !r.caps[b.Capability].Fields["host"].Graph {
			continue
		}
		v, err := r.fill(b, text, false)
		if err != nil {
			return err
		}
		svc := r.project.Services[b.Provider]
		for _, n := range sortedKeys(svc.Networks) {
			cfg := svc.Networks[n]
			if cfg == nil {
				cfg = &types.ServiceNetworkConfig{}
				svc.Networks[n] = cfg
			}
			if !slices.Contains(cfg.Aliases, v.text) {
				cfg.Aliases = append(cfg.Aliases, v.text)
			}
		}
		r.project.Services[b.Provider] = svc
	}
	return nil
}
