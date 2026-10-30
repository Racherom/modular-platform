package contract

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// Request level problem codes, answered with 400.
const (
	CodeInvalidGraph       = "invalid_graph"
	CodeUnsupportedVersion = "unsupported_version"
	CodeWrongKind          = "wrong_kind"
)

// DecodeGraph reads a graph from YAML or JSON (JSON is valid YAML) with strict decoding:
// unknown keys and duplicate keys are errors. It never uses plain encoding/json, which keeps the
// last of two duplicate keys. A non nil Problem means the input is not a graph this release reads.
func DecodeGraph(data []byte) (Graph, *Problem) {
	var g Graph
	if err := yaml.UnmarshalStrict(data, &g); err != nil {
		return Graph{}, requestProblem(CodeInvalidGraph, "", fmt.Sprintf("The graph could not be read: %s.", oneLine(err)))
	}
	if g.ApiVersion != APIVersion {
		return Graph{}, requestProblem(CodeUnsupportedVersion, "apiVersion",
			fmt.Sprintf("The apiVersion %q is not supported; this planner reads %s.", g.ApiVersion, APIVersion))
	}
	if g.Kind != KindGraph {
		return Graph{}, requestProblem(CodeWrongKind, "kind", fmt.Sprintf("The kind %q is not %s.", g.Kind, KindGraph))
	}
	if g.Modules == nil {
		return Graph{}, requestProblem(CodeInvalidGraph, "", "The graph has no modules map; write modules: {} for an empty graph.")
	}
	return g, nil
}

// MarshalGraph writes the canonical text of a graph: keys in byte order, two space indent,
// block style, null written as null, one trailing newline. The editor's writer matches it byte
// for byte (AC-9).
func MarshalGraph(g Graph) ([]byte, error) {
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
			for c, p := range *m.Uses {
				if p != nil {
					v := *p
					p = &v
				}
				uses[c] = p
			}
			m.Uses = &uses
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
