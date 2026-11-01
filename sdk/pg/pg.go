// Package pg is the client of a postgresql binding (spec 0005, "Bindings, clients and
// protection"): a pgx pool that signs in with the binding's user, password file and database at
// the host and port of its url. With sslmode verify-full it speaks TLS and checks the server's
// certificate against the binding's ca_file, or the system roots when ca_file is unset.
package pg

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/load"
)

const (
	defaultPort    = 5432
	connectTimeout = 5 * time.Second
)

// Open is the sdk.Opener of a postgresql binding. The pool connects lazily, so a database that is
// down at start does not stop the module; readiness pings it until it answers.
func Open(b sdk.Binding) (sdk.Opened, error) {
	cfg, err := config(b)
	if err != nil {
		return sdk.Opened{}, err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return sdk.Opened{}, fmt.Errorf("pg: %w", err)
	}
	return sdk.Opened{Client: pool, Ready: pool.Ping, Close: pool.Close}, nil
}

// config returns the pool config of b. Every connection setting comes from the binding: pgx
// fills its config from PG* env vars too, so config overwrites each one that decides where and how
// the pool connects.
func config(b sdk.Binding) (*pgxpool.Config, error) {
	port := defaultPort
	if p := b.URL.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("pg: port %q is not a number from 0 to 65535", p)
		}
		port = int(n)
	}
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("pg: %w", err)
	}
	cc := &cfg.ConnConfig.Config
	cc.Host = b.URL.Hostname()
	cc.Port = uint16(port)
	cc.Database = b.Database
	cc.User = b.User
	cc.Password = b.Password.Reveal()
	cc.ConnectTimeout = connectTimeout
	cc.RuntimeParams = map[string]string{}
	cc.Fallbacks = nil
	cc.ValidateConnect = nil
	cc.SSLNegotiation = ""
	cc.TLSConfig = nil
	if b.SSLMode == load.SSLModeVerifyFull {
		cc.TLSConfig = &tls.Config{ServerName: cc.Host, RootCAs: b.Roots, MinVersion: tls.VersionTLS12}
	}
	return cfg, nil
}
