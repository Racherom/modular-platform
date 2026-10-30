// Command planner serves the planner contract (contracts/openapi/planner.yaml) over the built in
// catalog. `planner healthcheck` probes a running planner, for container healthchecks.
package main

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/platform"
	"github.com/racherom/modular-platform/services/planner/internal/api"
	"github.com/racherom/modular-platform/services/planner/internal/config"
)

const addr = ":8080"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(platform.Healthcheck("http://127.0.0.1:8080/readyz"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, platform.NewLogger(), catalog.FS)
	stop()
	os.Exit(code)
}

// run returns the exit code. A catalog entry that breaks a rule stops the start with one log
// line per problem (AC-11).
func run(ctx context.Context, log *slog.Logger, catalogFS fs.FS) int {
	entries, problems := contract.LoadCatalog(catalogFS)
	if len(problems) > 0 {
		for _, p := range problems {
			log.Error(p.String())
		}
		return 1
	}
	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("planner cannot start", slog.Any("error", err))
		return 1
	}
	log.Info("planner starting", slog.String("mode", string(cfg.Mode)), slog.Int("catalog_entries", len(entries)))
	if err := platform.Serve(ctx, log, addr, api.NewHandler(ctx, cfg, entries, log)); err != nil {
		log.Error("planner stopped", slog.Any("error", err))
		return 1
	}
	return 0
}
