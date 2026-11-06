package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// GraphBinding is the binding of one slot in a graph (spec 0012): the provider text, `instance`
// or `instance.provide`, plus the named values of a binding to a provisioner. It is written as a
// string when it carries no named values, else as an object with the text under `provider`.
type GraphBinding struct {
	Provider string            // instance or instance.provide
	Named    map[string]string // field name to value; empty for a string binding
}

// Instance is the provider instance the binding names.
func (b GraphBinding) Instance() string {
	i, _, _ := strings.Cut(b.Provider, ".")
	return i
}

// Provide is the provide the binding names, or "" when it names only the instance.
func (b GraphBinding) Provide() string {
	_, q, _ := strings.Cut(b.Provider, ".")
	return q
}

// Clone returns a copy that shares no map with b.
func (b GraphBinding) Clone() GraphBinding {
	out := GraphBinding{Provider: b.Provider}
	if len(b.Named) > 0 {
		out.Named = maps.Clone(b.Named)
	}
	return out
}

// MarshalJSON writes the string form, or the object form when the binding has named values.
// encoding/json sorts the object's keys, so the text is canonical.
func (b GraphBinding) MarshalJSON() ([]byte, error) {
	if len(b.Named) == 0 {
		return json.Marshal(b.Provider)
	}
	m := make(map[string]string, len(b.Named)+1)
	maps.Copy(m, b.Named)
	m["provider"] = b.Provider
	return json.Marshal(m)
}

// UnmarshalJSON reads a string, or an object with a string `provider` and string named values.
func (b *GraphBinding) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*b = GraphBinding{Provider: s}
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.New("a binding must be a string like postgres or an object with a provider")
	}
	out := GraphBinding{}
	for _, k := range sortedKeys(raw) {
		var v string
		if err := json.Unmarshal(raw[k], &v); err != nil {
			return fmt.Errorf("binding field %q must be a string", k)
		}
		if k == "provider" {
			out.Provider = v
			continue
		}
		if out.Named == nil {
			out.Named = map[string]string{}
		}
		out.Named[k] = v
	}
	if out.Provider == "" {
		return errors.New("a binding object needs a provider")
	}
	*b = out
	return nil
}

// InstanceConfig is the config values a graph sets on a module instance (spec 0025): value name
// to its text. The text is never expanded; the planner checks it against the value's rules.
type InstanceConfig map[string]string

// UnmarshalJSON reads an object of strings. It refuses any other value, so a YAML number or
// boolean (5432, yes) is invalid_graph rather than quietly turned into text.
func (c *InstanceConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return errors.New("config must be a map of value names to text")
	}
	out := make(InstanceConfig, len(raw))
	for _, k := range sortedKeys(raw) {
		v := bytes.TrimSpace(raw[k])
		var s string
		if len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &s) != nil {
			return fmt.Errorf("config value %q must be text in quotes", k)
		}
		out[k] = s
	}
	*c = out
	return nil
}

// The kinds of a YieldFieldSource.
const (
	YieldNamed    = "named"    // the binding's named value from the graph
	YieldGenerate = "generate" // a per binding file of 32 random bytes in hex
	YieldOutput   = "output"   // a file the provision job writes
	YieldAdmin    = "admin"    // a field of the provisioner's own provide
)

// YieldFieldSource is where one field of a yielded capability comes from (spec 0014): `named`,
// `generate`, `output`, or `{admin: <field>}`.
type YieldFieldSource struct {
	Kind  string // YieldNamed, YieldGenerate, YieldOutput or YieldAdmin
	Admin string // the provisioner's field, for YieldAdmin
}

// MarshalJSON writes the string form, or `{"admin": <field>}`.
func (s YieldFieldSource) MarshalJSON() ([]byte, error) {
	if s.Kind == YieldAdmin {
		return json.Marshal(map[string]string{"admin": s.Admin})
	}
	return json.Marshal(s.Kind)
}

// UnmarshalJSON reads `named`, `generate`, `output` or `{admin: <field>}`.
func (s *YieldFieldSource) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		if !slices.Contains([]string{YieldNamed, YieldGenerate, YieldOutput}, text) {
			return fmt.Errorf("yield source %q is not named, generate, output or {admin: <field>}", text)
		}
		*s = YieldFieldSource{Kind: text}
		return nil
	}
	var obj struct {
		Admin string `json:"admin"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&obj); err != nil || obj.Admin == "" {
		return errors.New("a yield source must be named, generate, output or {admin: <field>}")
	}
	*s = YieldFieldSource{Kind: YieldAdmin, Admin: obj.Admin}
	return nil
}

// HostPortRef is a published host port (spec 0013): a number, or `{config: <int value>}`.
type HostPortRef struct {
	Number int    // set for the number form
	Config string // set for the config form
}

// MarshalJSON writes the number, or `{"config": <value>}`.
func (r HostPortRef) MarshalJSON() ([]byte, error) {
	if r.Config != "" {
		return json.Marshal(map[string]string{"config": r.Config})
	}
	return json.Marshal(r.Number)
}

// UnmarshalJSON reads a number or `{config: <value>}`.
func (r *HostPortRef) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*r = HostPortRef{Number: n}
		return nil
	}
	var obj struct {
		Config string `json:"config"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&obj); err != nil || obj.Config == "" {
		return errors.New("a host port must be a number or {config: <value>}")
	}
	*r = HostPortRef{Config: obj.Config}
	return nil
}

// HostPathRef is a path of a host mount (spec 0013): an absolute path, `{config: <value>}`, or
// `{install: <relative path>}`.
type HostPathRef struct {
	Path    string // set for the plain form
	Config  string // set for the config form
	Install string // set for the install form
}

// MarshalJSON writes the plain path, or the object form.
func (r HostPathRef) MarshalJSON() ([]byte, error) {
	switch {
	case r.Config != "":
		return json.Marshal(map[string]string{"config": r.Config})
	case r.Install != "":
		return json.Marshal(map[string]string{"install": r.Install})
	}
	return json.Marshal(r.Path)
}

// UnmarshalJSON reads a string, `{config: <value>}` or `{install: <relative path>}`.
func (r *HostPathRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = HostPathRef{Path: s}
		return nil
	}
	var obj struct {
		Config  string `json:"config"`
		Install string `json:"install"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&obj); err != nil || (obj.Config == "") == (obj.Install == "") {
		return errors.New("a host path must be a path, {config: <value>} or {install: <path>}")
	}
	*r = HostPathRef{Config: obj.Config, Install: obj.Install}
	return nil
}
