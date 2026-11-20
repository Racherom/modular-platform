package contract_test

import (
	"os"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// identityModule is a module that provides oidc with an enroll block, like the Authelia entry
// will be (spec 0035).
const identityModule = `apiVersion: modular-platform/v1
kind: Module
name: svc
description: An identity module.
image: example.test/svc:1.0.0
provides:
  idp:
    capability: oidc
    version: "1.0"
    port: 9091
    fields:
      issuer: {value: "https://auth.example.test"}
      client_id: {platform: consumer}
      client_secret: {generate: true}
      ca: {platform: ca}
secrets:
  - {name: users/, from: setup, mount: /config/users, description: The users file.}
enroll:
  entrypoint: [sh, -c]
  command: ["read user; read pass; echo $user > /out/users/users.yml"]
  asks:
    - {name: admin-user, description: User name., pattern: "[a-z0-9._-]+"}
    - {name: admin-password, description: Password., secret: true, min_length: 12}
  writes: [{name: users/users.yml}]
  restart: false
`

// oidcCaps returns the real oidc capability of the catalog.
func oidcCaps(t *testing.T) map[string]contract.Capability {
	t.Helper()
	data, err := os.ReadFile("../../catalog/capabilities/oidc.yaml")
	if err != nil {
		t.Fatalf("read oidc capability: %v", err)
	}
	c, problems := contract.CheckCapability("capabilities/oidc.yaml", data)
	if len(problems) > 0 {
		t.Fatalf("oidc capability: %v", problems)
	}
	return map[string]contract.Capability{"oidc": c}
}

// TestEnrollAccepted loads the enroll block with its ask rules (spec 0035, AC-20).
func TestEnrollAccepted(t *testing.T) {
	t.Parallel()
	m, problems := contract.CheckModule("modules/svc/module.yaml", []byte(identityModule), oidcCaps(t))
	if len(problems) > 0 {
		t.Fatalf("problems = %v", problems)
	}
	e := m.Enroll
	if e == nil {
		t.Fatal("enroll was not loaded")
	}
	if len(e.Asks) != 2 || e.Asks[0].Pattern != "[a-z0-9._-]+" || e.Asks[1].MinLength == nil || *e.Asks[1].MinLength != 12 || !e.Asks[1].Secret {
		t.Errorf("asks = %+v", e.Asks)
	}
	if len(e.Writes) != 1 || e.Writes[0].Name != "users/users.yml" {
		t.Errorf("writes = %+v", e.Writes)
	}
}

// TestEnrollRules breaks one enroll rule per case (spec 0035, AC-20).
func TestEnrollRules(t *testing.T) {
	t.Parallel()
	caps := oidcCaps(t)
	tests := map[string]ruleCase{
		"no command":         one(`command: ["read user; read pass; echo $user > /out/users/users.yml"]`, "command: []", "enroll.command", contract.CodeInvalidEnroll),
		"env MP_":            one("  restart: false\n", "  restart: false\n  env: {MP_X: y}\n", "enroll.env.MP_X", contract.CodeInvalidEnroll),
		"ask twice":          one("name: admin-password", "name: admin-user", "enroll.asks.1.name", contract.CodeInvalidEnroll),
		"ask name":           one("name: admin-password", "name: Admin_Password", "enroll.asks.1.name", contract.CodeInvalidName),
		"ask no description": one("description: Password.", `description: ""`, "enroll.asks.1.description", contract.CodeMissingField),
		"pattern":            one(`pattern: "[a-z0-9._-]+"`, `pattern: "[a-z"`, "enroll.asks.0.pattern", contract.CodeInvalidEnroll),
		"pattern lookahead":  one(`pattern: "[a-z0-9._-]+"`, `pattern: "(?=a)"`, "enroll.asks.0.pattern", contract.CodeInvalidEnroll),
		"min_length zero":    one("min_length: 12", "min_length: 0", "enroll.asks.1.min_length", contract.CodeInvalidEnroll),
		"min_length minus":   one("min_length: 12", "min_length: -3", "enroll.asks.1.min_length", contract.CodeInvalidEnroll),
		"write no secret":    one("writes: [{name: users/users.yml}]", "writes: [{name: other/users.yml}]", "enroll.writes.0.name", contract.CodeInvalidEnroll),
		"write plain":        one("writes: [{name: users/users.yml}]", "writes: [{name: users}]", "enroll.writes.0.name", contract.CodeInvalidEnroll),
		"write not setup": {[][2]string{
			{"{name: users/, from: setup,", "{name: users/, from: generate,"},
		}, "enroll.writes.0.name", contract.CodeInvalidEnroll},
		"write client": one("writes: [{name: users/users.yml}]", `writes: [{name: "users/{client}"}]`, "enroll.writes.0.name", contract.CodeInvalidEnroll),
		"read provide": one("  restart: false\n", "  restart: false\n  reads: [{provide: idp, field: client_secret}]\n", "enroll.reads.0.provide", contract.CodeInvalidEnroll),
		"for_each":     one("  restart: false\n", "  restart: false\n  for_each: idp\n", ".", contract.CodeInvalidEntry),
		"network":      one("  restart: false\n", "  restart: false\n  network: internal\n", ".", contract.CodeInvalidEntry),
		"not an oidc": {[][2]string{
			{"capability: oidc", "capability: store"},
		}, "enroll", contract.CodeInvalidEnroll},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := edit(t, identityModule, tc.edits)
			useCaps := caps
			if name == "not an oidc" {
				useCaps = testCaps(t)
				useCaps["oidc"] = caps["oidc"]
			}
			_, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), useCaps)
			wantProblem(t, problems, tc.path, tc.code)
		})
	}
}

// TestEnrollNoneByDefault leaves a module without an enroll block alone.
func TestEnrollNoneByDefault(t *testing.T) {
	t.Parallel()
	data, _, found := strings.Cut(identityModule, "enroll:")
	if !found {
		t.Fatal("the fixture has no enroll block")
	}
	m, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), oidcCaps(t))
	if len(problems) > 0 {
		t.Fatalf("problems = %v", problems)
	}
	if m.Enroll != nil {
		t.Errorf("enroll = %+v, want nil", m.Enroll)
	}
}
