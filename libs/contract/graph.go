package contract

import (
	"fmt"
	"maps"
	"strings"

	"sigs.k8s.io/yaml"
)

// Request level problem codes, answered with 400.
const (
	CodeInvalidGraph       = "invalid_graph"
	CodeUnsupportedVersion = "unsupported_version"
	CodeWrongKind          = "wrong_kind"
	CodeFormatTooNew       = "format_too_new"
)

// CodeConvertedFromV0alpha1 is the warning a plan of a converted v0alpha1 graph carries.
const CodeConvertedFromV0alpha1 = "converted_from_v0alpha1"

// header is the part of a file read before strict decoding, so a newer format gets
// format_too_new instead of the vague error its new fields would cause (spec 0011).
type header struct {
	FormatVersion any `json:"formatVersion"`
}

// DecodeGraph reads a v1 or v0alpha1 graph from YAML or JSON (JSON is valid YAML) with strict
// decoding: unknown keys and duplicate keys are errors. It never uses plain encoding/json, which
// keeps the last of two duplicate keys. A v0alpha1 graph comes back as written; ConvertGraph
// turns it into v1. A non nil Problem means the input is not a graph this release reads.
func DecodeGraph(data []byte) (Graph, *Problem) {
	if p := formatProblem(data); p != nil {
		return Graph{}, p
	}
	var g Graph
	if err := yaml.UnmarshalStrict(data, &g); err != nil {
		return Graph{}, requestProblem(CodeInvalidGraph, "", fmt.Sprintf("The graph could not be read: %s.", oneLine(err)))
	}
	if g.ApiVersion != APIVersion && g.ApiVersion != APIVersionV0alpha1 {
		return Graph{}, requestProblem(CodeUnsupportedVersion, "apiVersion",
			fmt.Sprintf("The apiVersion %q is not supported; this planner reads %s and %s.", g.ApiVersion, APIVersion, APIVersionV0alpha1))
	}
	if g.Kind != KindGraph {
		return Graph{}, requestProblem(CodeWrongKind, "kind", fmt.Sprintf("The kind %q is not %s.", g.Kind, KindGraph))
	}
	if g.Modules == nil {
		return Graph{}, requestProblem(CodeInvalidGraph, "", "The graph has no modules map; write modules: {} for an empty graph.")
	}
	if g.ApiVersion == APIVersionV0alpha1 {
		if p := v0alpha1Only(g); p != nil {
			return Graph{}, p
		}
	}
	for _, i := range sortedKeys(g.Modules) {
		if m := g.Modules[i]; (m.Module == "") == (m.External == nil) {
			return Graph{}, requestProblem(CodeInvalidGraph, "modules."+i,
				fmt.Sprintf("Instance %s needs exactly one of module and external.", i))
		}
	}
	return g, nil
}

// formatProblem reads formatVersion before strict decoding: format_too_new for a minor above
// FormatMinor, invalid_graph for a value that is not 1.<minor>.
func formatProblem(data []byte) *Problem {
	var h header
	if err := yaml.Unmarshal(data, &h); err != nil || h.FormatVersion == nil {
		return nil // strict decoding reports what is wrong
	}
	s, _ := h.FormatVersion.(string)
	ok, tooNew := checkFormat(s)
	switch {
	case tooNew:
		return requestProblem(CodeFormatTooNew, "formatVersion",
			fmt.Sprintf("The graph needs a planner that reads format %s; this one reads 1.%d.", s, FormatMinor))
	case !ok || s == "":
		return requestProblem(CodeInvalidGraph, "formatVersion",
			fmt.Sprintf("The formatVersion %v is not a version like 1.0.", h.FormatVersion))
	}
	return nil
}

// v0alpha1Only refuses the v1 parts in a graph that says v0alpha1.
func v0alpha1Only(g Graph) *Problem {
	if g.FormatVersion != "" {
		return requestProblem(CodeInvalidGraph, "formatVersion", "A v0alpha1 graph has no formatVersion.")
	}
	for _, i := range sortedKeys(g.Modules) {
		m := g.Modules[i]
		if m.External != nil {
			return requestProblem(CodeInvalidGraph, "modules."+i+".external", "A v0alpha1 graph has no external instances.")
		}
		if m.Uses == nil {
			continue
		}
		for _, s := range sortedKeys(*m.Uses) {
			if b := (*m.Uses)[s]; b != nil && len(b.Named) > 0 {
				return requestProblem(CodeInvalidGraph, "modules."+i+".uses."+s, "A v0alpha1 graph writes each binding as a string.")
			}
		}
	}
	return nil
}

// ConvertGraph turns a v0alpha1 graph into v1 (spec 0012): only the apiVersion changes, since
// every uses key becomes the slot of the same name. It reports whether g was v0alpha1; a v1
// graph comes back as a copy.
func ConvertGraph(g Graph) (Graph, bool) {
	out := CloneGraph(g)
	if g.ApiVersion != APIVersionV0alpha1 {
		return out, false
	}
	out.ApiVersion = APIVersion
	return out, true
}

// MarshalGraph writes the canonical text of a graph: keys in byte order, two space indent,
// block style, null written as null, a binding as a string unless it has named values,
// formatVersion left out at 1.0, one trailing newline. The editor's writer matches it byte for
// byte (AC-19).
func MarshalGraph(g Graph) ([]byte, error) {
	if g.FormatVersion == "1.0" {
		g.FormatVersion = ""
	}
	out, err := yaml.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("marshal graph: %w", err)
	}
	return out, nil
}

// CloneGraph returns a deep copy, so a planner result never shares maps with its input.
func CloneGraph(g Graph) Graph {
	out := g
	out.Modules = make(map[string]GraphModule, len(g.Modules))
	for name, m := range g.Modules {
		if m.Uses != nil {
			uses := make(Uses, len(*m.Uses))
			for s, b := range *m.Uses {
				if b != nil {
					c := b.Clone()
					b = &c
				}
				uses[s] = b
			}
			m.Uses = &uses
		}
		if m.External != nil {
			ext := ExternalInstance{Provides: make(map[string]ExternalProvide, len(m.External.Provides))}
			for p, ep := range m.External.Provides {
				ep.Fields = maps.Clone(ep.Fields)
				ext.Provides[p] = ep
			}
			m.External = &ext
		}
		out.Modules[name] = m
	}
	return out
}

func requestProblem(code, path, message string) *Problem {
	return &Problem{Severity: Error, Code: code, Path: path, Message: message}
}

// oneLine folds a decoder error onto one line, so a message stays one sentence.
func oneLine(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
