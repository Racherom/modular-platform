package contract_test

import (
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// TestNamesNeverContainUnderscore guards the Compose secret name `<instance>_<name>` (spec 0002):
// it is collision free only while no name may contain `_`.
func TestNamesNeverContainUnderscore(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"a_b", "_a", "a_"} {
		if contract.ValidName(s) {
			t.Errorf("ValidName(%q) = true; allowing `_` brings Compose secret name collisions back", s)
		}
	}
}
