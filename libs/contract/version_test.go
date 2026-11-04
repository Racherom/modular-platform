package contract_test

import (
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// TestRange checks spec 0012's version grammar, its comparison and its ranges.
func TestRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rng, version string
		inside       bool
	}{
		{"", "1", true},
		{">=15", "18.6", true},
		{">=15", "14.2", false},
		{">=15, <19", "19", false},
		{">=15,<19", "18.99.1", true},
		{"=18", "18.0.0", true},
		{"> 18.6", "18.6.0", false},
		{"<=1.2", "1.2.0.0", true},
	}
	for _, tc := range tests {
		r, err := contract.ParseRange(tc.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tc.rng, err)
		}
		v, ok := contract.ParseVersion(tc.version)
		if !ok {
			t.Fatalf("ParseVersion(%q) failed", tc.version)
		}
		if got := r.Contains(v); got != tc.inside {
			t.Errorf("%q contains %s = %v, want %v", tc.rng, tc.version, got, tc.inside)
		}
	}
	for _, bad := range []string{"~15", ">=x", ">=19, <15", ">1, <1", "=1, =2", ">=1.2.3.4.5", ","} {
		if _, err := contract.ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) accepted", bad)
		}
	}
	for _, bad := range []string{"", "v1", "1.", "1.2.3.4.5", "-1"} {
		if _, ok := contract.ParseVersion(bad); ok {
			t.Errorf("ParseVersion(%q) accepted", bad)
		}
	}
}
