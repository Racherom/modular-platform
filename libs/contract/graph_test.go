package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// TestCanonicalRoundTrip writes every shared fixture with the Go writer and compares it to the
// fixture text, which is canonical; the editor's test compares its writer to the same files
// (AC-19). A v0alpha1 fixture keeps its apiVersion until the planner converts it.
func TestCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../contracts/fixtures/graphs/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	old, _ := filepath.Glob("../../contracts/fixtures/graphs/v0alpha1/*.yaml")
	files = append(files, old...)
	files = append(files, "../../samples/whoami-postgres/graph.yaml")
	for _, file := range files {
		t.Run(strings.TrimPrefix(file, "../../"), func(t *testing.T) {
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

// TestMissingKeyAndNull checks that an undecided slot (no key), an unbound one (null), an empty
// uses map, a string binding and an object binding decode and write back differently, and that
// formatVersion 1.0 is left out.
func TestMissingKeyAndNull(t *testing.T) {
	t.Parallel()
	const head = "apiVersion: modular-platform/v1\nkind: Graph\nmodules:\n  a:\n    module: a\n"
	for _, text := range []string{
		head + "name: x\n",
		head + "    uses: {}\nname: x\n",
		head + "    uses:\n      c: null\nname: x\n",
		head + "    uses:\n      c: b\nname: x\n",
		head + "    uses:\n      c: b.server\nname: x\n",
		head + "    uses:\n      c:\n        database: d\n        provider: b\n        user: u\nname: x\n",
		"apiVersion: modular-platform/v1\nformatVersion: \"1.0\"\nkind: Graph\nmodules: {}\nname: x\n",
	} {
		g, p := contract.DecodeGraph([]byte(text))
		if p != nil {
			t.Fatalf("decode: %s", p.Message)
		}
		out, _ := contract.MarshalGraph(g)
		text = strings.Replace(text, "formatVersion: \"1.0\"\n", "", 1)
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
		"unknown key":     {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{},"extra":1}`, contract.CodeInvalidGraph, ""},
		"duplicate key":   {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","name":"y","modules":{}}`, contract.CodeInvalidGraph, ""},
		"nested unknown":  {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{"module":"a","x":1}}}`, contract.CodeInvalidGraph, ""},
		"not an object":   {`[1, 2]`, contract.CodeInvalidGraph, ""},
		"no modules":      {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x"}`, contract.CodeInvalidGraph, ""},
		"uses not scalar": {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{"module":"a","uses":{"c":[1]}}}}`, contract.CodeInvalidGraph, ""},
		"no provider":     {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{"module":"a","uses":{"c":{"user":"u"}}}}}`, contract.CodeInvalidGraph, ""},
		"named not text":  {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{"module":"a","uses":{"c":{"provider":"b","port":5432}}}}}`, contract.CodeInvalidGraph, ""},
		"module and ext":  {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{"module":"a","external":{"provides":{}}}}}`, contract.CodeInvalidGraph, "modules.a"},
		"neither":         {`{"apiVersion":"modular-platform/v1","kind":"Graph","name":"x","modules":{"a":{}}}`, contract.CodeInvalidGraph, "modules.a"},
		"v2":              {`{"apiVersion":"modular-platform/v2","kind":"Graph","name":"x","modules":{}}`, contract.CodeUnsupportedVersion, "apiVersion"},
		"wrong kind":      {`{"apiVersion":"modular-platform/v1","kind":"Module","name":"x","modules":{}}`, contract.CodeWrongKind, "kind"},
		"format too new":  {`{"apiVersion":"modular-platform/v1","formatVersion":"1.9","kind":"Graph","name":"x","modules":{},"later":1}`, contract.CodeFormatTooNew, "formatVersion"},
		"format not text": {`{"apiVersion":"modular-platform/v1","formatVersion":1.0,"kind":"Graph","name":"x","modules":{}}`, contract.CodeInvalidGraph, "formatVersion"},
		"format v2":       {`{"apiVersion":"modular-platform/v1","formatVersion":"2.0","kind":"Graph","name":"x","modules":{}}`, contract.CodeInvalidGraph, "formatVersion"},
		"v0alpha1 named":  {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","modules":{"a":{"module":"a","uses":{"c":{"provider":"b","user":"u"}}}}}`, contract.CodeInvalidGraph, "modules.a.uses.c"},
		"v0alpha1 format": {`{"apiVersion":"modular-platform/v0alpha1","formatVersion":"1.0","kind":"Graph","name":"x","modules":{}}`, contract.CodeInvalidGraph, "formatVersion"},
		"v0alpha1 extern": {`{"apiVersion":"modular-platform/v0alpha1","kind":"Graph","name":"x","modules":{"a":{"external":{"provides":{}}}}}`, contract.CodeInvalidGraph, "modules.a.external"},
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

// TestConvertGraph checks that conversion changes only the apiVersion (AC-14).
func TestConvertGraph(t *testing.T) {
	t.Parallel()
	g, p := contract.DecodeGraph([]byte("apiVersion: modular-platform/v0alpha1\nkind: Graph\nmodules:\n  a:\n    module: a\n    uses:\n      postgresql: b\nname: x\n"))
	if p != nil {
		t.Fatal(p.Message)
	}
	v1, converted := contract.ConvertGraph(g)
	out, _ := contract.MarshalGraph(v1)
	if !converted || string(out) != "apiVersion: modular-platform/v1\nkind: Graph\nmodules:\n  a:\n    module: a\n    uses:\n      postgresql: b\nname: x\n" {
		t.Errorf("converted=%v text:\n%s", converted, out)
	}
	if g.ApiVersion != contract.APIVersionV0alpha1 {
		t.Error("ConvertGraph changed its input")
	}
	if _, again := contract.ConvertGraph(v1); again {
		t.Error("a v1 graph reported as converted")
	}
}
