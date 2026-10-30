// Package contract holds the provisional modular-platform/v0alpha1 formats (spec 0002):
// the types generated from contracts/openapi/planner.yaml, strict decoding, the canonical
// writer and the catalog loader with its rules.
package contract

import "regexp"

const (
	// APIVersion is the only apiVersion this release reads and writes.
	APIVersion = "modular-platform/v0alpha1"
	// KindGraph is the kind of a graph file.
	KindGraph = "Graph"
	// KindModule is the kind of a catalog entry.
	KindModule = "Module"
)

var (
	nameRe    = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// YAML 1.1 readers turn these into booleans or null, so no name may be one of them.
	reservedNames = map[string]bool{
		"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true,
		"true": true, "false": true, "null": true,
	}
)

// ValidName reports whether s is a DNS label and not a YAML reserved word. It applies to graph
// names, instance names, catalog entry names, capability names and secret names.
func ValidName(s string) bool {
	return nameRe.MatchString(s) && !reservedNames[s]
}

// ValidEnvName reports whether s may name an environment variable in a catalog entry.
func ValidEnvName(s string) bool {
	return envNameRe.MatchString(s)
}
