package manifest_test

import (
	"errors"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// fixtures are shared with libs/contract, which runs the same cases through the catalog check.
type fixtures struct {
	Capabilities []string `json:"capabilities"`
	Header       string   `json:"header"`
	Cases        []struct {
		Name      string   `json:"name"`
		Want      []string `json:"want"`
		CheckOnly bool     `json:"check_only"`
		Body      string   `json:"body"`
	} `json:"cases"`
}

func TestCheckFixtures(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../contracts/fixtures/catalog/sdk-fields.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtures
	if err := yaml.UnmarshalStrict(data, &f); err != nil {
		t.Fatal(err)
	}
	caps := map[string]manifest.Capability{}
	for _, text := range f.Capabilities {
		c, err := manifest.ParseCapability([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		caps[c.Name] = c
	}
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			want := tc.Want
			if tc.CheckOnly {
				want = nil // a rule of the catalog check alone
			}
			var got []string
			e, err := manifest.Parse([]byte(f.Header + tc.Body))
			if err != nil {
				got = []string{". invalid_entry"}
			} else {
				for _, p := range manifest.Check(e, caps) {
					got = append(got, p.Path+" "+p.Code)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
				t.Errorf("problems = %q, want %q", got, want)
			}
		})
	}
}

func TestRulesParse(t *testing.T) {
	t.Parallel()
	home, _ := url.Parse("https://example.test/a")
	tests := map[string]struct {
		rules manifest.Rules
		text  string
		want  any
		code  string
	}{
		"string":             {manifest.Rules{Type: "string"}, "abc", "abc", ""},
		"string pattern":     {manifest.Rules{Type: "string", Pattern: "[a-z]+"}, "abc", "abc", ""},
		"string whole match": {manifest.Rules{Type: "string", Pattern: "[a-z]+"}, "abc1", nil, manifest.CodeNoMatch},
		"int":                {manifest.Rules{Type: "int", Min: "1", Max: "10"}, "10", 10, ""},
		"int negative":       {manifest.Rules{Type: "int"}, "-3", -3, ""},
		"int not a number":   {manifest.Rules{Type: "int"}, "1.5", nil, manifest.CodeInvalid},
		"int 64 bit":         {manifest.Rules{Type: "int"}, "9223372036854775807", 9223372036854775807, ""},
		"int past 64 bit":    {manifest.Rules{Type: "int"}, "9223372036854775808", nil, manifest.CodeInvalid},
		"int below min":      {manifest.Rules{Type: "int", Min: "1"}, "0", nil, manifest.CodeOutOfRange},
		"int above max":      {manifest.Rules{Type: "int", Max: "10"}, "11", nil, manifest.CodeOutOfRange},
		"bool true":          {manifest.Rules{Type: "bool"}, "true", true, ""},
		"bool false":         {manifest.Rules{Type: "bool"}, "false", false, ""},
		"bool yes":           {manifest.Rules{Type: "bool"}, "yes", nil, manifest.CodeInvalid},
		"bool upper":         {manifest.Rules{Type: "bool"}, "True", nil, manifest.CodeInvalid},
		"duration":           {manifest.Rules{Type: "duration", Min: "1s"}, "90s", 90 * time.Second, ""},
		"duration bad":       {manifest.Rules{Type: "duration"}, "10", nil, manifest.CodeInvalid},
		"duration below":     {manifest.Rules{Type: "duration", Min: "1s"}, "10ms", nil, manifest.CodeOutOfRange},
		"duration above":     {manifest.Rules{Type: "duration", Max: "1m"}, "2m", nil, manifest.CodeOutOfRange},
		"url":                {manifest.Rules{Type: "url"}, "https://example.test/a", *home, ""},
		"url relative":       {manifest.Rules{Type: "url"}, "/a", nil, manifest.CodeInvalid},
		"url no host":        {manifest.Rules{Type: "url"}, "mailto:a@example.test", nil, manifest.CodeInvalid},
		"enum":               {manifest.Rules{Type: "enum", Values: []string{"a", "b"}}, "b", "b", ""},
		"enum other":         {manifest.Rules{Type: "enum", Values: []string{"a", "b"}}, "c", nil, manifest.CodeNotAllowed},
		"secret":             {manifest.Rules{Type: "secret"}, "hunter2", "hunter2", ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.rules.Parse(tc.text)
			if tc.code != "" {
				re := &manifest.RuleError{}
				ok := errors.As(err, &re)
				if !ok || re.Code != tc.code {
					t.Fatalf("Parse(%q) error = %v, want code %s", tc.text, err, tc.code)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tc.text, err)
			}
			if gotURL, ok := got.(url.URL); ok {
				wantURL := tc.want.(url.URL)
				if gotURL.String() != wantURL.String() {
					t.Errorf("Parse(%q) = %v, want %v", tc.text, got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}

// TestIsBootstrap covers the bootstrap names: each one is known, and a declared value, a binding
// key, a wrong case or the empty string is not.
func TestIsBootstrap(t *testing.T) {
	t.Parallel()
	for _, name := range manifest.BootstrapNames {
		if !manifest.IsBootstrap(name) {
			t.Errorf("IsBootstrap(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "mp_instance", "MP_INSTANCE_FILE", "TM_GREETING", "MP_PORT"} {
		if manifest.IsBootstrap(name) {
			t.Errorf("IsBootstrap(%q) = true, want false", name)
		}
	}
}

func TestGoName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"renderer-rate": "RendererRate",
		"object-store":  "ObjectStore",
		"s3":            "S3",
		"a1-b":          "A1B",
		"read only":     "ReadOnly",
		"public":        "Public",
	} {
		if got := manifest.GoName(in); got != want {
			t.Errorf("GoName(%q) = %q, want %q", in, got, want)
		}
	}
}
