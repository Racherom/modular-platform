// Package catalog embeds the built in catalog, one modular-platform/v0alpha1 entry per file.
// It lives in its own module because go:embed cannot reach above a package's own folder.
package catalog

import "embed"

// FS holds every catalog/*.yaml file at its root.
//
//go:embed *.yaml
var FS embed.FS
