package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// TestCanonicalRoundTrip writes every shared fixture with the Go writer and compares it to the
// fixture text, which is canonical; the editor's test compares its writer to the same files (AC-9).
func TestCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../contracts/fixtures/graphs/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	files = append(files, "../../samples/whoami-postgres/graph.yaml")
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			g, p := contract.DecodeGraph(want)
			if p != nil {
				t.Fatalf("decode: %s", p.Message)
			}
			got, err := contract.MarshalGraph(g)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("canonical text differs\n--- got\n%s--- want\n%s", got, want)
			}
		})
	}
}

// TestMissingKeyAndNull checks that an undecided need (no key), an unbound one (null) and an
// empty uses map decode and write back differently.
func TestMissingKeyAndNull(t *testing.T) {
	t.Parallel()
	const head = "apiVersion: modular-platform/v0alpha1\nkind: Graph\nmodules:\n  a:\n    module: a\n"
	for _, text := range []string{
		head + "name: x\n",
		head + "    uses: {}\nname: x\n",
		head + "    uses:\n      c: null\nname: x\n",
		head + "    uses:\n      c: b\nname: x\n",
	} {
		g, p := contract.DecodeGraph([]byte(text))
		if p != nil {
			t.Fatalf("decode: %s", p.Message)
		}
		out, _ := contract.MarshalGraph(g)
		if string(out) != text {
			t.Errorf("round trip changed the text\n--- got\n%s--- want\n%s", out, text)
		}
	}
}

func TestDecodeGraphRejects(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		body, code, path string
	}{
		"unknown key":     {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","modules":{},"extra":1}`, contract.CodeInvalidGraph, ""},
		"duplicate key":   {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","name":"y","modules":{}}`, contract.CodeInvalidGraph, ""},
		"nested unknown":  {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","modules":{"a":{"module":"a","x":1}}}`, contract.CodeInvalidGraph, ""},
		"not an object":   {`[1, 2]`, contract.CodeInvalidGraph, ""},
		"no modules":      {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x"}`, contract.CodeInvalidGraph, ""},
		"uses not scalar": {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","modules":{"a":{"module":"a","uses":{"c":[1]}}}}`, contract.CodeInvalidGraph, ""},
		"v1":              {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{}}`, contract.CodeUnsupportedVersion, "apiVersion"},
		"wrong kind":      {`{"apiVersion":"modular-platform/v0alpha1","kind":"Module","name":"x","modules":{}}`, contract.CodeWrongKind, "kind"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, p := contract.DecodeGraph([]byte(tc.body))
			if p == nil {
				t.Fatal("decoded, want a problem")
			}
			if p.Code != tc.code || p.Path != tc.path || p.Severity != contract.Error {
				t.Errorf("problem = %+v, want %s at %q", *p, tc.code, tc.path)
			}
			if bytes.ContainsRune([]byte(p.Message), '\n') {
				t.Errorf("message spans lines: %q", p.Message)
			}
		})
	}
}
