package check_test

import (
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
)

// TestGraphTextRule checks the three graph text rules of spec 0025 on the exact text, nothing
// trimmed, in their order (AC-27).
func TestGraphTextRule(t *testing.T) {
	t.Parallel()
	const control, brace, quote = "control character", "{ or }", "same quote"
	tests := map[string]string{
		`"q"`: quote, `'q'`: quote, `""`: quote, `"é"`: quote, `''`: quote,
		` "q"`: "", `"q`: "", `a"b`: "", `"q'`: "", `"`: "", `'`: "", "plain": "", "é": "", "a #b": "", "$x": "",
		"a\tb": control, "a\nb": control, "\x7f": control, "\u0085": control, "a b": control, "a b": control,
		"{": brace, "a}b": brace, "${X}": brace,
		"\"{\"": brace, "\"\t\"": control,
	}
	for text, want := range tests {
		got := check.GraphTextRule(text)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("GraphTextRule(%q) = %q, want %q", text, got, want)
		}
		if got != "" && strings.Contains(got, text) && text != "{" {
			t.Errorf("GraphTextRule(%q) repeats the text: %q", text, got)
		}
	}
}

// TestConfigMessages checks which rule each config problem of the problems fixture names: a
// secret set to { gets only config_not_settable, a plain value set to { the brace message, and
// an enum miss the value's own rule (AC-27).
func TestConfigMessages(t *testing.T) {
	t.Parallel()
	got := check.Graph(readFixture(t, "config-problems"), testCatalog(t))
	want := map[string]string{
		"modules.a.config.label": "{ or }",
		"modules.a.config.mode":  "is not one of fast, slow",
		"modules.a.config.level": "above the maximum 9",
		"modules.a.config.need":  "cannot be empty",
		"modules.a.config.token": "is a secret",
		"modules.a.config.uid":   "filled at install time",
		"modules.m.config.root":  "host path",
	}
	seen := map[string]int{}
	for _, p := range got.Problems {
		seen[p.Path]++
		if w, ok := want[p.Path]; ok && !strings.Contains(p.Message, w) {
			t.Errorf("%s: message %q, want it to hold %q", p.Path, p.Message, w)
		}
	}
	for path := range want {
		if seen[path] != 1 {
			t.Errorf("%s has %d problems, want 1", path, seen[path])
		}
	}
}

// TestConfigInDigest checks that the graph's config values reach the resolved graph and change
// the digest (AC-27).
func TestConfigInDigest(t *testing.T) {
	t.Parallel()
	catalog := testCatalog(t)
	g := readFixture(t, "config-set")
	first := check.Graph(g, catalog)
	if !first.Ok || (*first.Graph.Modules["a"].Config)["level"] != "7" {
		t.Fatalf("result ok=%v, graph config %v", first.Ok, first.Graph.Modules["a"].Config)
	}
	changed := contract.CloneGraph(g)
	(*changed.Modules["a"].Config)["level"] = "8"
	if check.Graph(changed, catalog).Digest == first.Digest {
		t.Error("a changed config value keeps the digest")
	}
	if (*g.Modules["a"].Config)["level"] != "7" {
		t.Error("CloneGraph shares the config map")
	}
}
