// Command api is the managed platform's control plane (contracts/openapi/api.yaml): it signs the
// admin in, keeps the stacks and their history in Postgres, and drives the renderer and the
// deploy target. It runs on the module sdk: its config is declared in catalog/modules/api, and
// `api healthcheck` probes a running one. `api admin reset` deletes the admin binding and every
// session, so the next verified sign in with the admin email binds again. `api caller-key
// generate` is the entry's setup step, which writes the MP-Caller key pair before any config
// exists.
package main

import (
	"log/slog"
	"os"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/api"
	"github.com/racherom/modular-platform/services/api/internal/callerkey"
	"github.com/racherom/modular-platform/services/api/internal/config"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == callerkey.Command {
		if err := callerkey.Run(os.Args[2:]); err != nil {
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("caller-key failed", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}
	sdk.Run(config.Module, api.Setup, sdk.WithCommand("admin", sdk.Command[config.Config, config.Uses](api.Admin)))
}
