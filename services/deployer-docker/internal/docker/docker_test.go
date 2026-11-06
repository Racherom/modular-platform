package docker

import (
	"strings"
	"testing"
)

// TestComposeEnvHoldsNoPlatformVariable: docker compose reads its process env before the
// project's env files, so an MP_* variable here would replace the rendered default of every
// module value of that name (spec 0025, AC-25). The env is fixed, never the deployer's own.
func TestComposeEnvHoldsNoPlatformVariable(t *testing.T) {
	t.Setenv("MP_INGRESS__TLS_MODE", "acme")
	for _, kv := range composeEnv {
		if strings.HasPrefix(kv, "MP_") {
			t.Errorf("composeEnv holds %s", kv)
		}
	}
	if len(composeEnv) != 2 {
		t.Errorf("composeEnv = %v, want PATH and HOME only", composeEnv)
	}
}
