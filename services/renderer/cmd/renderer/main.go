// Command renderer serves the renderer contract (contracts/openapi/renderer.yaml) over the built in
// catalog. It runs on the module sdk: its config is declared in catalog/renderer.yaml, and
// `renderer healthcheck` probes a running renderer, for container healthchecks.
package main

import (
	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/renderer/internal/api"
	"github.com/racherom/modular-platform/services/renderer/internal/config"
)

// sourceRef is the git ref this build was made from, set by the Dockerfile with
// `-ldflags -X main.sourceRef=...`; empty means main (spec 0030, AC-10).
var sourceRef string

func main() { sdk.Run(config.Module, api.Setup(catalog.FS, sourceRef)) }
