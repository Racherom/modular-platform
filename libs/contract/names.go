// Package contract holds the provisional modular-platform/v0alpha1 formats (spec 0002):
// the types generated from contracts/openapi/planner.yaml, strict decoding, the canonical
// writer and the catalog loader with its rules.
package contract

import "github.com/racherom/modular-platform/sdk/manifest"

const (
	// APIVersion is the only apiVersion this release reads and writes.
	APIVersion = "modular-platform/v0alpha1"
	// KindGraph is the kind of a graph file.
	KindGraph = "Graph"
	// KindModule is the kind of a catalog entry.
	KindModule = "Module"
)

// ValidName reports whether s is a DNS label and not a YAML reserved word. It applies to graph
// names, instance names, catalog entry names, capability names and secret names.
func ValidName(s string) bool {
	return manifest.ValidName(s)
}
