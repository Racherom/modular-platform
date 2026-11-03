// Command deployer-docker serves the deploy target contract (contracts/openapi/deploy-target.yaml)
// on this host: it applies rendered projects with docker compose and reports container state. It
// is the only platform service with the Docker socket. It runs on the module sdk: its config is
// declared in catalog/deployer-docker.yaml, and `deployer-docker healthcheck` probes a running one.
package main

import (
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/api"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/config"
)

func main() { sdk.Run(config.Module, api.Setup) }
