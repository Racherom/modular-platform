package api

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/config"
	"github.com/racherom/modular-platform/services/api/internal/store"
)

// errAdminUsage is the answer to an admin command api does not know.
var errAdminUsage = errors.New("usage: api admin reset")

// adminResetDone is the one line admin reset prints.
const adminResetDone = "admin reset: the admin binding and every session are deleted; the next verified sign in with PLATFORM_ADMIN_EMAIL becomes the admin."

// adminStore is what the admin command changes.
type adminStore interface {
	ResetAdmin(ctx context.Context) error
}

// Admin is the command `api admin reset` (spec 0006 AC-6): it deletes the admin binding and
// every session, so open screens sign out and the next verified sign in with the admin email
// binds again. It runs beside a serving api, as `docker compose exec api /api admin reset`.
func Admin(ctx context.Context, m *sdk.Module[config.Config, config.Uses], args []string, out io.Writer) error {
	pool, err := m.Uses.MainDb(ctx)
	if err != nil {
		return err
	}
	return adminReset(ctx, store.New(pool), args, out)
}

func adminReset(ctx context.Context, st adminStore, args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "reset" {
		return errAdminUsage
	}
	if err := st.ResetAdmin(ctx); err != nil {
		return fmt.Errorf("admin reset: %w", err)
	}
	_, err := fmt.Fprintln(out, adminResetDone)
	return err
}
