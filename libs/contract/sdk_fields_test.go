package contract_test

import (
	"os"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
)

// TestSDKFieldFixtures runs the fixtures sdk/manifest runs too (spec 0005 AC-12, spec 0011 AC-20): the catalog
// check must report exactly the same problems, so the OpenAPI schema and sdk/manifest never
// disagree about an entry.
func TestSDKFieldFixtures(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../contracts/fixtures/catalog/sdk-fields.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Capabilities []string `json:"capabilities"`
		Header       string   `json:"header"`
		Cases        []struct {
			Name        string   `json:"name"`
			Want        []string `json:"want"`
			PlannerOnly bool     `json:"planner_only"`
			Body        string   `json:"body"`
		} `json:"cases"`
	}
	if err := yaml.UnmarshalStrict(data, &f); err != nil {
		t.Fatal(err)
	}
	caps := map[string]contract.Capability{}
	for _, text := range f.Capabilities {
		c, _ := contract.CheckCapability("", []byte(text))
		caps[c.Name] = c
	}
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			_, problems := contract.CheckModule("modules/svc/module.yaml", []byte(f.Header+tc.Body), caps)
			var got []string
			for _, p := range problems {
				got = append(got, p.Path+" "+p.Code)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.Want) && (len(got) != 0 || len(tc.Want) != 0) {
				t.Errorf("problems = %q, want %q", got, tc.Want)
			}
		})
	}
}
