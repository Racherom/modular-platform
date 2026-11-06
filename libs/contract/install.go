package contract

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// ComposeVar is the Compose variable of config value v of instance i (spec 0013, "Asked
// values"): MP_<I>__<V>, both upper cased, with each - as _. setup.sh writes an asked value
// under this name into .env.
func ComposeVar(i, v string) string {
	return "MP_" + envPart(i) + "__" + envPart(v)
}

func envPart(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }

// Text is the bound as written: a whole number, or a duration like 10s.
func (b ConfigBound) Text() string {
	raw, err := b.MarshalJSON()
	if err != nil {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// Check checks text as a value of v with the parser the SDK loads it with (spec 0025), so a
// value the planner accepts is one the module accepts. A failure is a *manifest.RuleError.
func (v ConfigValue) Check(text string) error {
	mv := manifest.Value{Type: string(v.Type), Values: v.Values}
	if v.Min != nil {
		mv.Min = &manifest.Bound{Text: v.Min.Text()}
	}
	if v.Max != nil {
		mv.Max = &manifest.Bound{Text: v.Max.Text()}
	}
	if v.Pattern != "" {
		mv.Pattern = &v.Pattern
	}
	return manifest.CheckValue(mv, text)
}

// Asked reports whether setup.sh asks config value v.
func (v ConfigValue) Asked() bool { return v.Setup == ConfigValueSetupAsk }

// ComposeValues lists, in name order, the entry's config values that render as Compose
// variables (spec 0013): every asked value that is not a secret, and every other text value
// that publish, host_mounts, network.aliases, a {config} field source or an environment
// {config.<v>} placeholder names. An asked secret becomes a file, never a variable.
func (m Module) ComposeValues() []string {
	named := map[string]bool{}
	for _, p := range m.Publish {
		named[p.Host.Config] = true
	}
	for _, h := range m.HostMounts {
		named[h.Host.Config] = true
		named[h.Path.Config] = true
	}
	if m.Network != nil {
		for _, a := range m.Network.Aliases {
			named[a.Config] = true
		}
	}
	for _, p := range m.Provides {
		for _, s := range p.Fields {
			named[s.Config] = true
		}
	}
	for _, text := range m.Environment {
		for _, part := range ParseEnvironment(text) {
			if part.Kind == EnvConfig {
				named[part.Name] = true
			}
		}
	}
	var out []string
	for _, name := range sortedKeys(m.Config) {
		v := m.Config[name]
		if v.Type == ConfigValueTypeSecret {
			continue
		}
		if v.Asked() || named[name] {
			out = append(out, name)
		}
	}
	return out
}

// SetupWrites lists the secret names (a folder as <folder>/) that step k of the entry writes;
// a for_each write <folder>/{client} counts as its folder.
func (m Module) SetupWrites(k int) []string {
	var out []string
	for _, w := range m.Setup[k].Writes {
		if folder, ok := strings.CutSuffix(w.Name, "/"+clientToken); ok {
			out = append(out, folder+"/")
			continue
		}
		out = append(out, w.Name)
	}
	return out
}

// SecretEntry returns the entry's secrets entry named n (a folder as <folder>/).
func (m Module) SecretEntry(n string) (Secret, bool) {
	i := slices.IndexFunc(m.Secrets, func(s Secret) bool { return s.Name == n })
	if i < 0 {
		return Secret{}, false
	}
	return m.Secrets[i], true
}

// ClientToken is the text a for_each step's client replaces (spec 0013, "Install time").
const ClientToken = clientToken

// BindingOrder lists instances so that every provider comes before its consumers, ties by name
// (spec 0013, the order setup.sh runs setup steps in). The planner refuses a binding cycle; the
// instances of one are listed last, in name order.
func BindingOrder(instances []string, bindings []Binding) []string {
	waits := map[string]map[string]bool{} // consumer to the providers it waits for
	for _, b := range bindings {
		if b.Consumer == b.Provider {
			continue
		}
		if waits[b.Consumer] == nil {
			waits[b.Consumer] = map[string]bool{}
		}
		waits[b.Consumer][b.Provider] = true
	}
	left := slices.Sorted(slices.Values(instances))
	done := map[string]bool{}
	out := make([]string, 0, len(left))
	for len(left) > 0 {
		next := slices.IndexFunc(left, func(i string) bool {
			for p := range waits[i] {
				if !done[p] && slices.Contains(left, p) {
					return false
				}
			}
			return true
		})
		if next < 0 {
			return append(out, left...) // a cycle
		}
		done[left[next]] = true
		out = append(out, left[next])
		left = slices.Delete(left, next, next+1)
	}
	return out
}
