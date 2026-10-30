package contract_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/contract"
)

const goodEntry = `apiVersion: modular-platform/v0alpha1
kind: Module
name: svc
description: A service.
image: example.test/svc:1.0.0
provides:
  http:
    port: 8080
uses:
  db:
    optional: true
    env:
      host: DB_HOST
      port: DB_PORT
secrets:
  - name: token
    env: TOKEN_FILE
healthcheck:
  test: ["CMD", "svc", "check"]
  interval: 5s
  retries: 3
`

// TestCatalogRules breaks one catalog rule per case (AC-11).
func TestCatalogRules(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		old, new   string // replace old with new in goodEntry
		path, code string
	}{
		"bad yaml":          {"kind: Module", "kind: [", ".", contract.CodeInvalidEntry},
		"unknown key":       {"kind: Module", "kind: Module\nextra: 1", ".", contract.CodeInvalidEntry},
		"duplicate key":     {"kind: Module", "kind: Module\nkind: Module", ".", contract.CodeInvalidEntry},
		"version":           {"v0alpha1", "v1", "apiVersion", contract.CodeUnsupportedVersion},
		"kind":              {"kind: Module", "kind: Graph", "kind", contract.CodeWrongKind},
		"name pattern":      {"name: svc", "name: Svc", "name", contract.CodeInvalidName},
		"name reserved":     {"name: svc", "name: \"off\"", "name", contract.CodeInvalidName},
		"name mismatch":     {"name: svc", "name: other", "name", contract.CodeNameMismatch},
		"no description":    {"description: A service.", "description: \"\"", "description", contract.CodeMissingField},
		"no image":          {"image: example.test/svc:1.0.0", "image: \"\"", "image", contract.CodeMissingField},
		"bad image":         {"example.test/svc:1.0.0", "example.test/svc!:1.0.0", "image", contract.CodeInvalidImage},
		"untagged image":    {"example.test/svc:1.0.0", "example.test/svc", "image", contract.CodeImageUntagged},
		"digest only":       {"example.test/svc:1.0.0", "example.test/svc@sha256:" + strings.Repeat("a", 64), "image", contract.CodeImageUntagged},
		"latest image":      {"example.test/svc:1.0.0", "example.test/svc:latest", "image", contract.CodeImageLatest},
		"capability name":   {"  http:", "  HTTP:", "provides.HTTP", contract.CodeInvalidName},
		"port zero":         {"port: 8080", "port: 0", "provides.http.port", contract.CodeInvalidPort},
		"port too high":     {"port: 8080", "port: 70000", "provides.http.port", contract.CodeInvalidPort},
		"use name":          {"  db:", "  d_b:", "uses.d_b", contract.CodeInvalidName},
		"env name":          {"host: DB_HOST", "host: DB-HOST", "uses.db.env.host", contract.CodeInvalidEnvName},
		"env twice":         {"port: DB_PORT", "port: DB_HOST", "uses.db.env.port", contract.CodeDuplicateEnv},
		"env twice secret":  {"env: TOKEN_FILE", "env: DB_HOST", "secrets.0.env", contract.CodeDuplicateEnv},
		"secret name":       {"name: token", "name: To_ken", "secrets.0.name", contract.CodeInvalidName},
		"secret no env":     {"env: TOKEN_FILE", "env: \"\"", "secrets.0.env", contract.CodeMissingField},
		"secret twice":      {"    env: TOKEN_FILE", "    env: TOKEN_FILE\n  - name: token\n    env: OTHER_FILE", "secrets.1.name", contract.CodeDuplicateName},
		"healthcheck form":  {`["CMD", "svc", "check"]`, `["svc", "check"]`, "healthcheck.test", contract.CodeInvalidHealthcheck},
		"healthcheck empty": {`["CMD", "svc", "check"]`, `["CMD"]`, "healthcheck.test", contract.CodeInvalidHealthcheck},
		"duration":          {"interval: 5s", "interval: soon", "healthcheck.interval", contract.CodeInvalidDuration},
		"retries":           {"retries: 3", "retries: 0", "healthcheck.retries", contract.CodeInvalidRetries},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(goodEntry, tc.old) {
				t.Fatalf("goodEntry has no %q", tc.old)
			}
			data := strings.Replace(goodEntry, tc.old, tc.new, 1)
			_, problems := contract.CheckEntry("svc.yaml", []byte(data))
			for _, p := range problems {
				if p.Path == tc.path && p.Code == tc.code {
					return
				}
			}
			t.Errorf("problems = %v, want %s at %s", problems, tc.code, tc.path)
		})
	}
}

func TestGoodEntryAndDigest(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		goodEntry,
		strings.Replace(goodEntry, "example.test/svc:1.0.0", "registry.test:5000/team/svc:1.0.0@sha256:"+strings.Repeat("a", 64), 1),
	} {
		if _, problems := contract.CheckEntry("svc.yaml", []byte(data)); len(problems) > 0 {
			t.Errorf("problems = %v, want none", problems)
		}
	}
}

func TestLoadCatalog(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"svc.yaml": {Data: []byte(goodEntry)},
		"bad.yaml": {Data: []byte(strings.Replace(goodEntry, "name: svc", "name: bad\nextra: 1", 1))},
	}
	entries, problems := contract.LoadCatalog(fsys)
	if len(entries) != 1 || entries[0].Name != "svc" {
		t.Errorf("entries = %v, want only svc", entries)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want one", problems)
	}
	if got := problems[0].String(); !strings.HasPrefix(got, "catalog/bad.yaml: .: invalid_entry: ") {
		t.Errorf("line = %q", got)
	}
}
