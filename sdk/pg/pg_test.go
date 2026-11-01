package pg

import (
	"net/url"
	"testing"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/load"
)

// TestEnvIgnored covers that the binding alone decides where and how the pool connects: libpq
// env vars pgx reads on its own change none of it.
func TestEnvIgnored(t *testing.T) {
	for k, v := range map[string]string{
		"PGHOST":               "other.test",
		"PGPORT":               "6543",
		"PGDATABASE":           "other",
		"PGUSER":               "other",
		"PGPASSWORD":           "other",
		"PGSSLMODE":            "require",
		"PGAPPNAME":            "other",
		"PGTARGETSESSIONATTRS": "read-only",
		"PGSSLNEGOTIATION":     "direct",
		"PGCONNECT_TIMEOUT":    "60",
	} {
		t.Setenv(k, v)
	}
	cfg, err := config(sdk.Binding{
		URL:      &url.URL{Scheme: "postgres", Host: "db.test"},
		Database: "app",
		User:     "app",
		Password: load.NewSecret("pw"),
		SSLMode:  load.SSLModeDisable,
	})
	if err != nil {
		t.Fatal(err)
	}
	cc := cfg.ConnConfig
	if cc.Host != "db.test" || cc.Port != defaultPort || cc.Database != "app" || cc.User != "app" || cc.Password != "pw" {
		t.Errorf("target = host %q port %d database %q user %q", cc.Host, cc.Port, cc.Database, cc.User)
	}
	if cc.TLSConfig != nil || len(cc.Fallbacks) > 0 || cc.ValidateConnect != nil || cc.SSLNegotiation != "" {
		t.Errorf("tls %v, fallbacks %d, validate %v, negotiation %q", cc.TLSConfig != nil, len(cc.Fallbacks), cc.ValidateConnect != nil, cc.SSLNegotiation)
	}
	if len(cc.RuntimeParams) > 0 || cc.ConnectTimeout != connectTimeout {
		t.Errorf("runtime params %v, connect timeout %s", cc.RuntimeParams, cc.ConnectTimeout)
	}
}

func TestBadPort(t *testing.T) {
	t.Parallel()
	_, err := Open(sdk.Binding{URL: &url.URL{Scheme: "postgres", Host: "db.test:70000"}})
	if err == nil {
		t.Fatal("a port above 65535 opened a pool")
	}
}
