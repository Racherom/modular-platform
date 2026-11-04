// Package catalog embeds the built in catalog (spec 0011): one modular-platform/v1 capability
// per capabilities/<name>.yaml and one module per modules/<name>/module.yaml, with the module's
// templates beside it. It lives in its own module because go:embed cannot reach above a
// package's own folder.
package catalog

import "embed"

// FS holds the capabilities and modules folders at its root.
//
//go:embed capabilities modules
var FS embed.FS
