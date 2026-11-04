// Package contract holds the modular-platform/v1 formats (spec 0011): the types generated from
// contracts/openapi/planner.yaml, the hand written union types behind them, strict decoding with
// the v0alpha1 graph converter, the canonical writer, versions and ranges, and the catalog loader
// with its rules.
package contract

import "github.com/racherom/modular-platform/sdk/manifest"

const (
	// APIVersion is the apiVersion this release reads and writes.
	APIVersion = "modular-platform/v1"
	// APIVersionV0alpha1 is the provisional apiVersion of spec 0002; a graph in it is still
	// read and converted to v1, a catalog entry in it is refused.
	APIVersionV0alpha1 = "modular-platform/v0alpha1"
	// KindGraph is the kind of a graph file.
	KindGraph = "Graph"
	// KindModule is the kind of a module's catalog entry.
	KindModule = "Module"
	// KindCapability is the kind of a capability file.
	KindCapability = "Capability"
)

// ValidName reports whether s is a DNS label and not a YAML reserved word. It applies to graph,
// instance, capability, module, slot, provide, config value, secret and setup step names.
func ValidName(s string) bool {
	return manifest.ValidName(s)
}
