// Command api is the managed platform's control plane (contracts/openapi/api.yaml): it signs the
// admin in, keeps the deployments and their history in Postgres, and drives the planner and the
// deploy target. It runs on the module sdk: its config is declared in catalog/api.yaml, and
// `api healthcheck` probes a running one. `api admin reset` deletes the admin binding and every
// session, so the next verified sign in with the admin email binds again.
package main

import (
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/api"
	"github.com/racherom/modular-platform/services/api/internal/config"
)

func main() {
	sdk.Run(config.Module, api.Setup, sdk.WithCommand("admin", sdk.Command[config.Config, config.Uses](api.Admin)))
}
