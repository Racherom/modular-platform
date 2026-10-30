// Command planner serves the planner contract (contracts/openapi/planner.yaml) over the built in
// catalog. It runs on the module sdk: its config is declared in catalog/planner.yaml, and
// `planner healthcheck` probes a running planner, for container healthchecks.
package main

import (
	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/planner/internal/api"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

func main() { sdk.Run(config.Module, api.Setup(catalog.FS)) }
