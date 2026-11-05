package contract_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/contract"
)

const goodCapability = `apiVersion: modular-platform/v1
kind: Capability
name: store
description: A store and a login for one consumer.
scheme: store
fields:
  host: {type: string, description: Host name.}
  port: {type: int, description: Port.}
  url: {type: url, description: Base URL.}
  name: {type: string, pattern: "[a-z]+", description: Store name.}
  password: {type: secret, description: Password.}
  mode: {type: enum, values: [fast, slow], description: Mode.}
  ca: {type: certificate, optional: true, description: Root CA.}
  token: {type: secret, direction: both, optional: true, description: Binding token.}
  owner: {type: string, direction: to_provider, optional: true, description: Owner the consumer names.}
`

const goodProvisioner = `apiVersion: modular-platform/v1
kind: Capability
name: store-admin
description: Admin access that makes a store per consumer.
scheme: store
fields:
  host: {type: string, description: Host name.}
  port: {type: int, description: Port.}
  url: {type: url, description: Base URL.}
  admin_password: {type: secret, description: Admin password.}
  mode: {type: enum, values: [fast, slow], description: Mode.}
yields:
  capability: store
  named: [name]
  login: name
  fields:
    host: {admin: host}
    port: {admin: port}
    url: {admin: url}
    name: named
    password: generate
    mode: {admin: mode}
    ca: output
    token: generate
  job:
    image: example.test/job:1.0.0
    provision: echo provision
    revoke: echo revoke
`

const goodModule = `apiVersion: modular-platform/v1
kind: Module
name: svc
description: A service.
image: example.test/svc:1.0.0
provides:
  web:
    capability: store
    version: "1.0"
    port: 8080
    fields:
      host: {platform: host}
      port: {platform: port}
      url: {platform: url, path: /api}
      name: {value: main}
      password: {secret: token}
      mode: {value: fast}
      ca: {platform: ca}
uses:
  db:
    capability: store
    versions: ">=15, <19"
    optional: true
    env:
      host: DB_HOST
      port: DB_PORT
      password: DB_PASSWORD_FILE
    files:
      ca: /run/ca.pem
    fields:
      owner: {value: svc}
secrets:
  - name: token
    env: TOKEN_FILE
    description: A token.
  - {name: clients/, from: setup, mount: /run/clients, description: One file per client.}
volumes:
  data: /var/lib/svc
hardening:
  tmpfs: [/tmp]
templates:
  - file: svc.conf.tmpl
    path: /etc/svc.conf
setup:
  - name: init
    command: [svc, init]
    writes: [{name: token}]
  - name: client-file
    entrypoint: [sh, -c]
    command: ["svc digest \"$(cat /in/web/token)\" ${SALT:-x} > /out/clients/$CLIENT"]
    env: {CLIENT: "{client}"}
    for_each: web
    reads: [{provide: web, field: token}]
    writes: [{name: "clients/{client}"}]
healthcheck:
  test: ["CMD", "svc", "check"]
  interval: 5s
  retries: 3
`

// testCaps checks the good capabilities and returns them by name.
func testCaps(t *testing.T) map[string]contract.Capability {
	t.Helper()
	out := map[string]contract.Capability{}
	for _, text := range []string{goodCapability, goodProvisioner} {
		name := "store"
		if strings.Contains(text, "name: store-admin") {
			name = "store-admin"
		}
		c, problems := contract.CheckCapability("capabilities/"+name+".yaml", []byte(text))
		if len(problems) > 0 {
			t.Fatalf("%s: %v", name, problems)
		}
		out[name] = c
	}
	return out
}

type ruleCase struct {
	edits      [][2]string // replace each old with new, once, in order
	path, code string
}

func edit(t *testing.T, text string, edits [][2]string) string {
	t.Helper()
	for _, e := range edits {
		if !strings.Contains(text, e[0]) {
			t.Fatalf("text has no %q", e[0])
		}
		text = strings.Replace(text, e[0], e[1], 1)
	}
	return text
}

func one(old, replacement, path, code string) ruleCase {
	return ruleCase{[][2]string{{old, replacement}}, path, code}
}

func wantProblem(t *testing.T, problems []contract.CatalogProblem, path, code string) {
	t.Helper()
	for _, p := range problems {
		if p.Path == path && p.Code == code {
			if p.Message == "" {
				t.Errorf("%s at %s has no message", code, path)
			}
			return
		}
	}
	t.Errorf("problems = %v, want %s at %s", problems, code, path)
}

// TestModuleRules breaks one module rule per case (AC-2, spec 0013's rule list and the 0002 and
// 0005 rules still in force).
func TestModuleRules(t *testing.T) {
	t.Parallel()
	caps := testCaps(t)
	sdk := [2]string{"image: example.test/svc:1.0.0\n", "image: example.test/svc:1.0.0\nruntime: sdk\n"}
	noPort := [2]string{"    port: 8080\n", ""}
	tests := map[string]ruleCase{
		"bad yaml":            one("kind: Module", "kind: [", ".", contract.CodeInvalidEntry),
		"unknown key":         one("kind: Module", "kind: Module\nextra: 1", ".", contract.CodeInvalidEntry),
		"duplicate key":       one("kind: Module", "kind: Module\nkind: Module", ".", contract.CodeInvalidEntry),
		"v0alpha1 entry":      one("modular-platform/v1", "modular-platform/v0alpha1", "apiVersion", contract.CodeUnsupportedVersion),
		"format too new":      one("kind: Module", "kind: Module\nformatVersion: \"1.9\"\nlater: 1", "formatVersion", contract.CodeFormatTooNew),
		"kind":                one("kind: Module", "kind: Graph", "kind", contract.CodeWrongKind),
		"name pattern":        one("name: svc", "name: Svc", "name", contract.CodeInvalidName),
		"name reserved":       one("name: svc", "name: \"off\"", "name", contract.CodeInvalidName),
		"name mismatch":       one("name: svc", "name: other", "name", contract.CodeNameMismatch),
		"no description":      one("description: A service.", "description: \"\"", "description", contract.CodeMissingField),
		"no image":            one("image: example.test/svc:1.0.0", "image: \"\"", "image", contract.CodeMissingField),
		"bad image":           one("example.test/svc:1.0.0", "example.test/svc!:1.0.0", "image", contract.CodeInvalidImage),
		"untagged image":      one("example.test/svc:1.0.0", "example.test/svc", "image", contract.CodeImageUntagged),
		"latest image":        one("example.test/svc:1.0.0", "example.test/svc:latest", "image", contract.CodeImageLatest),
		"provide name":        one("  web:", "  Web:", "provides.Web", contract.CodeInvalidName),
		"unknown capability":  one("    capability: store\n    version", "    capability: nope\n    version", "provides.web.capability", contract.CodeUnknownCapability),
		"invalid version":     one(`version: "1.0"`, `version: "one"`, "provides.web.version", contract.CodeInvalidVersion),
		"port zero":           one("port: 8080", "port: 0", "provides.web.port", contract.CodeInvalidPort),
		"port too high":       one("port: 8080", "port: 70000", "provides.web.port", contract.CodeInvalidPort),
		"port on sdk":         {[][2]string{sdk}, "provides.web.port", contract.CodePortNotAllowed},
		"sdk two provides":    {[][2]string{sdk, noPort, {"uses:\n", "  more:\n    capability: store\n    version: \"1.0\"\n    fields: {host: {platform: host}, port: {platform: port}, url: {platform: url}, name: {value: a}, password: {secret: token}, mode: {value: fast}}\nuses:\n"}}, "provides", contract.CodeTooManyProvides},
		"unknown field":       one("      mode: {value: fast}", "      mode: {value: fast}\n      speed: {value: x}", "provides.web.fields.speed", contract.CodeUnknownField),
		"missing source":      one("      host: {platform: host}\n", "", "provides.web.fields.host", contract.CodeMissingFieldSource),
		"source two kinds":    one("{value: main}", "{value: main, secret: token}", "provides.web.fields.name", contract.CodeInvalidFieldSource),
		"source bad enum":     one("{value: fast}", "{value: medium}", "provides.web.fields.mode", contract.CodeInvalidFieldSource),
		"source bad pattern":  one("{value: main}", "{value: Main}", "provides.web.fields.name", contract.CodeInvalidFieldSource),
		"source file as text": one("{secret: token}", "{value: x}", "provides.web.fields.password", contract.CodeInvalidFieldSource),
		"source no secret":    one("{secret: token}", "{secret: nope}", "provides.web.fields.password", contract.CodeInvalidFieldSource),
		"source to_provider":  one("      ca: {platform: ca}\n", "      ca: {platform: ca}\n      owner: {value: x}\n", "provides.web.fields.owner", contract.CodeInvalidFieldSource),
		"source no config":    one("{value: main}", "{config: nope}", "provides.web.fields.name", contract.CodeInvalidFieldSource),
		"source no slot":      one("{value: main}", "{uses: nope, field: host}", "provides.web.fields.name", contract.CodeInvalidFieldSource),
		"source no volume":    one("{secret: token}", "{volume: nope, path: x}", "provides.web.fields.password", contract.CodeInvalidFieldSource),
		"source tls no off":   one("{value: fast}", "{platform: tls, \"on\": fast}", "provides.web.fields.mode", contract.CodeInvalidFieldSource),
		"source path no url":  one("{platform: host}", "{platform: host, path: /x}", "provides.web.fields.host", contract.CodeInvalidFieldSource),
		"auth without token":  one("    port: 8080\n", "    port: 8080\n    auth: binding-token\n    env: {token: SVC_CLIENTS_DIR}\n", "provides.web.auth", contract.CodeInvalidFieldSource),
		"provide env field":   one("    port: 8080\n", "    port: 8080\n    env: {host: SVC_HOST}\n", "provides.web.env.host", contract.CodeInvalidRule),
		"slot name":           one("  db:", "  d_b:", "uses.d_b", contract.CodeInvalidName),
		"slot capability":     one("    capability: store\n    versions", "    capability: nope\n    versions", "uses.db.capability", contract.CodeUnknownCapability),
		"invalid range":       one(`">=15, <19"`, `">=19, <15"`, "uses.db.versions", contract.CodeInvalidRange),
		"range syntax":        one(`">=15, <19"`, `"~15"`, "uses.db.versions", contract.CodeInvalidRange),
		"env unknown field":   one("host: DB_HOST", "nope: DB_HOST", "uses.db.env.nope", contract.CodeUnknownField),
		"env to_provider":     one("host: DB_HOST", "owner: DB_HOST", "uses.db.env.owner", contract.CodeInvalidRule),
		"env name":            one("host: DB_HOST", "host: DB-HOST", "uses.db.env.host", contract.CodeInvalidEnvName),
		"env twice":           one("port: DB_PORT", "port: DB_HOST", "uses.db.env.port", contract.CodeDuplicateEnv),
		"files text field":    one("ca: /run/ca.pem", "host: /run/host", "uses.db.files.host", contract.CodeInvalidFieldSource),
		"files relative":      one("ca: /run/ca.pem", "ca: run/ca.pem", "uses.db.files.ca", contract.CodeInvalidMount),
		"mount twice":         one("tmpfs: [/tmp]", "tmpfs: [/var/lib/svc]", "hardening.tmpfs.0", contract.CodeInvalidMount),
		"cap lower case":      one("tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: [net_bind_service]", "hardening.cap_add.0", contract.CodeInvalidHardening),
		"cap prefix":          one("tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: [CAP_NET_BIND_SERVICE]", "hardening.cap_add.0", contract.CodeInvalidHardening),
		"cap all":             one("tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: [ALL]", "hardening.cap_add.0", contract.CodeInvalidHardening),
		"cap twice":           one("tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: [NET_BIND_SERVICE, NET_BIND_SERVICE]", "hardening.cap_add.1", contract.CodeInvalidHardening),
		"cap unknown":         one("tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: [NOT_A_CAP]", "hardening.cap_add.0", contract.CodeInvalidHardening),
		"consumer source":     one("owner: {value: svc}", "owner: {generate: true}", "uses.db.fields.owner", contract.CodeInvalidFieldSource),
		"consumer to_cons":    one("owner: {value: svc}", "host: {value: svc}", "uses.db.fields.host", contract.CodeInvalidFieldSource),
		"per caller":          one("    optional: true\n", "    optional: true\n    per_caller: true\n", "uses.db.per_caller", contract.CodePerCallerNotAllowed),
		"client not sdk":      one("    optional: true\n", "    optional: true\n    client: http\n", "uses.db.client", contract.CodeClientNotAllowed),
		"client field":        {[][2]string{sdk, noPort, {"    optional: true\n", "    optional: true\n    client: postgresql\n"}}, "uses.db.env.url", contract.CodeMissingField},
		"reserved slot":       {[][2]string{sdk, noPort, {"  db:\n", "  acme:\n"}}, "uses.acme", contract.CodeReservedSlot},
		"secret name":         one("name: token", "name: To_ken", "secrets.0.name", contract.CodeInvalidName),
		"secret no target":    one("    env: TOKEN_FILE\n", "", "secrets.0", contract.CodeMissingField),
		"secret description":  one("description: A token.", "description: \"\"", "secrets.0.description", contract.CodeMissingField),
		"secret from":         one("    env: TOKEN_FILE\n", "    env: TOKEN_FILE\n    from: nowhere\n", "secrets.0.from", contract.CodeInvalidRule),
		"secret twice":        one("    description: A token.\n", "    description: A token.\n  - name: token\n    env: OTHER_FILE\n    description: Again.\n", "secrets.1.name", contract.CodeDuplicateName),
		"secret folder":       one("name: token\n", "name: certs/\n", "secrets.0", contract.CodeInvalidRule),
		"secret env twice":    one("env: TOKEN_FILE", "env: DB_HOST", "secrets.0.env", contract.CodeDuplicateEnv),
		"volume path":         one("data: /var/lib/svc", "data: var/lib", "volumes.data", contract.CodeInvalidMount),
		"template file":       one("svc.conf.tmpl", "svc.conf", "templates.0.file", contract.CodeInvalidTemplate),
		"template delims":     one("    path: /etc/svc.conf\n", "    path: /etc/svc.conf\n    delims: [\"[[\", \"\"]\n", "templates.0.delims", contract.CodeInvalidTemplate),
		"setup no command":    one("command: [svc, init]", "command: []", "setup.0.command", contract.CodeInvalidSetupStep),
		"setup for each":      one("    command: [svc, init]\n", "    command: [svc, init]\n    for_each: nope\n", "setup.0.for_each", contract.CodeInvalidSetupStep),
		"client env":          one("    command: [svc, init]\n", "    command: [svc, init]\n    env: {X: \"{client}\"}\n", "setup.0.env.X", contract.CodeInvalidSetupStep),
		"client command":      one("command: [svc, init]", "command: [svc, \"{client}\"]", "setup.0.command.1", contract.CodeInvalidSetupStep),
		"client entrypoint":   one("    command: [svc, init]\n", "    entrypoint: [\"svc-{client}\"]\n    command: [svc, init]\n", "setup.0.entrypoint.0", contract.CodeInvalidSetupStep),
		"client write":        one("writes: [{name: token}]", "writes: [{name: \"clients/{client}\"}]", "setup.0.writes.0.name", contract.CodeInvalidSetupStep),
		"each bare folder":    one(`"clients/{client}"`, "clients/", "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"each prefix":         one(`"clients/{client}"`, `"digest-{client}"`, "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"each suffix":         one(`"clients/{client}"`, `"clients/{client}.pem"`, "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"each nested":         one(`"clients/{client}"`, `"a/b/{client}"`, "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"each no folder":      one(`"clients/{client}"`, `"other/{client}"`, "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"each file secret":    one(`"clients/{client}"`, `"token/{client}"`, "setup.1.writes.0.name", contract.CodeInvalidSetupStep),
		"folder not declared": one("writes: [{name: token}]", "writes: [{name: other/}]", "setup.0.writes.0.name", contract.CodeInvalidSetupStep),
		"each asks":           one("    for_each: web\n", "    for_each: web\n    asks: [{name: who, description: Who.}]\n", "setup.1.asks", contract.CodeInvalidSetupStep),
		"provide no for_each": one("writes: [{name: token}]", "writes: [{name: token}]\n    reads: [{provide: web, field: token}]", "setup.0.reads.0.provide", contract.CodeInvalidSetupStep),
		"provide other":       one("reads: [{provide: web, field: token}]", "reads: [{provide: api, field: token}]", "setup.1.reads.0.provide", contract.CodeInvalidSetupStep),
		"provide to_consumer": one("reads: [{provide: web, field: token}]", "reads: [{provide: web, field: host}]", "setup.1.reads.0.field", contract.CodeInvalidSetupStep),
		"setup env MP_":       one(`env: {CLIENT: "{client}"}`, `env: {MP_INSTANCE: "{client}"}`, "setup.1.env.MP_INSTANCE", contract.CodeInvalidSetupStep),
		"healthcheck form":    one(`["CMD", "svc", "check"]`, `["svc", "check"]`, "healthcheck.test", contract.CodeInvalidHealthcheck),
		"duration":            one("interval: 5s", "interval: soon", "healthcheck.interval", contract.CodeInvalidDuration),
		"retries":             one("retries: 3", "retries: 0", "healthcheck.retries", contract.CodeInvalidRetries),
		"env placeholder":     one("secrets:\n", "environment:\n  SVC_DB: \"{nope.host}\"\nsecrets:\n", "environment.SVC_DB", contract.CodeInvalidFieldSource),
		"env file field":      one("secrets:\n", "environment:\n  SVC_DB: \"{db.password}\"\nsecrets:\n", "environment.SVC_DB", contract.CodeInvalidFieldSource),
		"env no config":       one("secrets:\n", "environment:\n  SVC_DB: \"{config.nope}\"\nsecrets:\n", "environment.SVC_DB", contract.CodeInvalidFieldSource),
		"alias no config":     one("secrets:\n", "network:\n  aliases: [{config: nope}]\nsecrets:\n", "network.aliases.0", contract.CodeInvalidRule),
		"publish port":        one("secrets:\n", "publish:\n  - {port: 0, host: 8080}\nsecrets:\n", "publish.0.port", contract.CodeInvalidPort),
		"publish host port":   one("secrets:\n", "publish:\n  - {port: 80, host: 70000}\nsecrets:\n", "publish.0.host", contract.CodeInvalidPort),
		"publish no config":   one("secrets:\n", "publish:\n  - {port: 80, host: {config: nope}}\nsecrets:\n", "publish.0.host", contract.CodeInvalidRule),
		"publish text config": {[][2]string{{"secrets:\n", hostConfig + "publish:\n  - {port: 80, host: {config: data-dir}}\nsecrets:\n"}}, "publish.0.host", contract.CodeInvalidRule},
		"mount host relative": one("secrets:\n", "host_mounts:\n  - {host: srv/svc, path: /srv/svc}\nsecrets:\n", "host_mounts.0.host", contract.CodeInvalidMount),
		"mount host config":   one("secrets:\n", "host_mounts:\n  - {host: {config: nope}, path: /srv/svc}\nsecrets:\n", "host_mounts.0.host", contract.CodeInvalidRule),
		"mount host secret":   {[][2]string{{"secrets:\n", hostConfig + "host_mounts:\n  - {host: {config: key}, path: /srv/svc}\nsecrets:\n"}}, "host_mounts.0.host", contract.CodeInvalidRule},
		"mount install out":   one("secrets:\n", "host_mounts:\n  - {host: {install: ../etc}, path: /srv/svc}\nsecrets:\n", "host_mounts.0.host", contract.CodeInvalidMount),
		"mount install root":  one("secrets:\n", "host_mounts:\n  - {host: {install: .}, path: /srv/svc}\nsecrets:\n", "host_mounts.0.host", contract.CodeInvalidMount),
		"mount path config":   {[][2]string{{"secrets:\n", hostConfig + "host_mounts:\n  - {host: /srv/svc, path: {config: data-dir}}\nsecrets:\n"}}, "host_mounts.0.path", contract.CodeInvalidMount},
		"mount path relative": one("secrets:\n", "host_mounts:\n  - {host: /srv/svc, path: srv/svc}\nsecrets:\n", "host_mounts.0.path", contract.CodeInvalidMount),
		"mount path twice":    one("secrets:\n", "host_mounts:\n  - {host: /srv/svc, path: /var/lib/svc}\nsecrets:\n", "host_mounts.0.path", contract.CodeInvalidMount),
		"mount install twice": one("secrets:\n", "host_mounts:\n  - {host: {install: a}, path: {install: a}}\n  - {host: {install: b}, path: {install: a}}\nsecrets:\n", "host_mounts.1.path", contract.CodeInvalidMount),
		"mount create plain":  one("secrets:\n", "host_mounts:\n  - {host: /srv/svc, path: /srv/svc, create: true}\nsecrets:\n", "host_mounts.0.create", contract.CodeInvalidRule),
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := edit(t, goodModule, tc.edits)
			_, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), caps)
			wantProblem(t, problems, tc.path, tc.code)
		})
	}
}

// hostConfig declares the config values the host access cases name: an int port, a text folder
// and a secret.
const hostConfig = `config:
  http-port: {env: SVC_HTTP_PORT, type: int, default: "8080", description: Host port.}
  data-dir: {env: SVC_DATA_DIR, type: string, default: /srv/svc, description: Host folder.}
  key: {env: SVC_KEY, type: secret, description: A key.}
`

// TestHostAccessEntry accepts every form of publish and host_mounts (spec 0013, Fields).
func TestHostAccessEntry(t *testing.T) {
	t.Parallel()
	data := edit(t, goodModule, [][2]string{{"secrets:\n", hostConfig + `publish:
  - {port: 8080, host: {config: http-port}}
  - {port: 9090, host: 9090}
host_mounts:
  - {host: /var/run/svc.sock, path: /run/svc.sock}
  - {host: {config: data-dir}, path: /srv/svc, read_only: false}
  - {host: {install: state}, path: {install: state}, read_only: false, create: true}
secrets:
`}})
	if _, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), testCaps(t)); len(problems) > 0 {
		t.Errorf("problems = %v, want none", problems)
	}
}

// TestCapabilityRules breaks one capability rule per case (AC-2, spec 0012's rule list).
func TestCapabilityRules(t *testing.T) {
	t.Parallel()
	tests := map[string]ruleCase{
		"bad yaml":         one("kind: Capability", "kind: [", ".", contract.CodeInvalidEntry),
		"unknown key":      one("kind: Capability", "kind: Capability\nextra: 1", ".", contract.CodeInvalidEntry),
		"v0alpha1":         one("modular-platform/v1", "modular-platform/v0alpha1", "apiVersion", contract.CodeUnsupportedVersion),
		"format too new":   one("kind: Capability", "kind: Capability\nformatVersion: \"1.3\"", "formatVersion", contract.CodeFormatTooNew),
		"kind":             one("kind: Capability", "kind: Module", "kind", contract.CodeWrongKind),
		"no name":          one("name: store", "name: \"\"", "name", contract.CodeMissingField),
		"name pattern":     one("name: store", "name: Store", "name", contract.CodeInvalidName),
		"name mismatch":    one("name: store", "name: other", "name", contract.CodeNameMismatch),
		"no description":   one("description: A store and a login for one consumer.", "description: \"\"", "description", contract.CodeMissingField),
		"scheme":           one("scheme: store", "scheme: Store!", "scheme", contract.CodeInvalidRule),
		"no fields":        {[][2]string{{"fields:\n", "otherfields:\n"}, {"otherfields:\n", ""}}, ".", contract.CodeInvalidEntry},
		"field name":       one("  host:", "  Host:", "fields.Host", contract.CodeInvalidName),
		"field type":       one("{type: string, description: Host name.}", "{type: text, description: Host name.}", "fields.host.type", contract.CodeInvalidType),
		"field no desc":    one("{type: string, description: Host name.}", "{type: string, description: \"\"}", "fields.host.description", contract.CodeMissingField),
		"bad pattern":      one(`pattern: "[a-z]+"`, `pattern: "[a-z"`, "fields.name.pattern", contract.CodeInvalidRule),
		"pattern on int":   one("{type: int, description: Port.}", "{type: int, pattern: \"[0-9]+\", description: Port.}", "fields.port.pattern", contract.CodeInvalidRule),
		"empty values":     one("values: [fast, slow]", "values: []", "fields.mode.values", contract.CodeInvalidRule),
		"repeated values":  one("values: [fast, slow]", "values: [fast, fast]", "fields.mode.values", contract.CodeInvalidRule),
		"values on string": one("{type: string, description: Host name.}", "{type: string, values: [a], description: Host name.}", "fields.host.values", contract.CodeInvalidRule),
		"both on text":     one("{type: string, description: Host name.}", "{type: string, direction: both, description: Host name.}", "fields.host.direction", contract.CodeInvalidDirection),
		"bad direction":    one("direction: to_provider", "direction: sideways", "fields.owner.direction", contract.CodeInvalidDirection),
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, problems := contract.CheckCapability("capabilities/store.yaml", []byte(edit(t, goodCapability, tc.edits)))
			wantProblem(t, problems, tc.path, tc.code)
		})
	}
}

// TestYieldsRules breaks one yields rule per case (AC-2, spec 0014's rule list).
func TestYieldsRules(t *testing.T) {
	t.Parallel()
	caps := testCaps(t)
	tests := map[string]ruleCase{
		"unknown target":     one("  capability: store\n", "  capability: nope\n", "yields.capability", contract.CodeInvalidYields),
		"yields provisioner": one("  capability: store\n", "  capability: store-admin\n", "yields.capability", contract.CodeInvalidYields),
		"named not string":   one("named: [name]", "named: [name, port]", "yields.named", contract.CodeInvalidYields),
		"named no field":     one("named: [name]", "named: [name, nope]", "yields.named", contract.CodeInvalidYields),
		"login not named":    one("login: name", "login: host", "yields.login", contract.CodeInvalidYields),
		"missing source":     one("    host: {admin: host}\n", "", "yields.fields.host", contract.CodeMissingFieldSource),
		"unknown field":      one("    host: {admin: host}\n", "    host: {admin: host}\n    nope: generate\n", "yields.fields.nope", contract.CodeUnknownField),
		"to_provider source": one("    host: {admin: host}\n", "    host: {admin: host}\n    owner: generate\n", "yields.fields.owner", contract.CodeInvalidFieldSource),
		"generate on text":   one("    host: {admin: host}\n", "    host: generate\n", "yields.fields.host", contract.CodeInvalidFieldSource),
		"output on text":     one("    host: {admin: host}\n", "    host: output\n", "yields.fields.host", contract.CodeInvalidFieldSource),
		"admin no field":     one("{admin: host}", "{admin: nope}", "yields.fields.host", contract.CodeInvalidFieldSource),
		"admin file to text": one("{admin: host}", "{admin: admin_password}", "yields.fields.host", contract.CodeInvalidFieldSource),
		"named not listed":   one("    port: {admin: port}\n", "    port: named\n", "yields.fields.port", contract.CodeInvalidFieldSource),
		"named field source": one("    name: named\n", "    name: {admin: host}\n", "yields.fields.name", contract.CodeInvalidFieldSource),
		"job latest":         one("example.test/job:1.0.0", "example.test/job:latest", "yields.job.image", contract.CodeInvalidJob),
		"job no provision":   one("provision: echo provision", "provision: \"\"", "yields.job.provision", contract.CodeInvalidJob),
		"login no revoke":    one("    revoke: echo revoke\n", "", "yields.job.revoke", contract.CodeInvalidJob),
		"revoke no login":    one("  login: name\n", "", "yields.job.revoke", contract.CodeInvalidJob),
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := edit(t, goodProvisioner, tc.edits)
			k, problems := contract.CheckCapability("capabilities/store-admin.yaml", []byte(data))
			if len(problems) > 0 {
				t.Fatalf("own rules: %v", problems)
			}
			byName := map[string]contract.Capability{"store": caps["store"], "store-admin": k}
			wantProblem(t, contract.CheckYields("capabilities/store-admin.yaml", k, byName), tc.path, tc.code)
		})
	}
}

func TestGoodEntries(t *testing.T) {
	t.Parallel()
	caps := testCaps(t)
	if got := contract.CheckYields("capabilities/store-admin.yaml", caps["store-admin"], caps); len(got) > 0 {
		t.Errorf("yields problems = %v, want none", got)
	}
	for _, data := range []string{
		goodModule,
		strings.Replace(goodModule, "example.test/svc:1.0.0", "registry.test:5000/team/svc:1.0.0@sha256:"+strings.Repeat("a", 64), 1),
	} {
		if _, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), caps); len(problems) > 0 {
			t.Errorf("problems = %v, want none", problems)
		}
	}
}

// TestLoadCatalog checks the layout, the problem lines and that only clean entries are served.
func TestLoadCatalog(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"capabilities/store.yaml":       {Data: []byte(goodCapability)},
		"capabilities/store-admin.yaml": {Data: []byte(goodProvisioner)},
		"capabilities/twin.yaml":        {Data: []byte(strings.Replace(goodCapability, "name: store", "name: twin", 1))},
		"modules/svc/module.yaml":       {Data: []byte(goodModule)},
		"modules/svc/svc.conf.tmpl":     {Data: []byte("x")},
		"modules/bad/module.yaml":       {Data: []byte(strings.Replace(goodModule, "name: svc", "name: bad\nextra: 1", 1))},
		"modules/empty/README.md":       {Data: []byte("no entry")},
	}
	c, problems := contract.LoadCatalog(fsys)
	if len(c.Modules) != 1 || c.Modules[0].Name != "svc" {
		t.Errorf("modules = %v, want only svc", c.Modules)
	}
	if len(c.Capabilities) != 3 || c.Capabilities[0].Name != "store" || c.Capabilities[2].Name != "twin" {
		t.Errorf("capabilities = %v, want store, store-admin, twin", c.Capabilities)
	}
	var lines []string
	for _, p := range problems {
		lines = append(lines, p.String())
	}
	if len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "catalog/modules/bad/module.yaml: .: invalid_entry: ") ||
		!strings.HasPrefix(lines[1], "catalog/modules/empty/module.yaml: .: invalid_entry: ") {
		t.Errorf("lines = %q", lines)
	}
}

// TestModuleEnvironmentAndClientFields accepts every placeholder form 0013 lists, and refuses a
// slot that leaves a required to_provider field without a source.
func TestModuleEnvironmentAndClientFields(t *testing.T) {
	t.Parallel()
	caps := testCaps(t)
	text := edit(t, goodModule, [][2]string{{"secrets:\n", "environment:\n  SVC_DB: \"{db.host}:{db.port}/x\"\n  SVC_RUN_AS: \"{uid}:{gid}\"\n  SVC_LITERAL: \"{ not a placeholder }\"\nsecrets:\n"}})
	if _, problems := contract.CheckModule("catalog/modules/svc/module.yaml", []byte(text), caps); len(problems) > 0 {
		t.Errorf("placeholders refused: %v", problems)
	}

	required := edit(t, goodCapability, [][2]string{{"to_provider, optional: true", "to_provider"}})
	k, problems := contract.CheckCapability("catalog/capabilities/store.yaml", []byte(required))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	caps["store"] = k
	text = edit(t, goodModule, [][2]string{{"    fields:\n      owner: {value: svc}\n", ""}})
	_, problems = contract.CheckModule("catalog/modules/svc/module.yaml", []byte(text), caps)
	wantProblem(t, problems, "uses.db.fields.owner", contract.CodeMissingFieldSource)
}

// TestParseEnvironment splits literal text and placeholders.
func TestParseEnvironment(t *testing.T) {
	t.Parallel()
	got := contract.ParseEnvironment("{public.base_path}/api?u={uid}&c={config.mode}{ x }")
	want := []contract.EnvPart{
		{Kind: contract.EnvUses, Text: "{public.base_path}", Name: "public", Field: "base_path"},
		{Kind: contract.EnvLiteral, Text: "/api?u="},
		{Kind: contract.EnvUID, Text: "{uid}"},
		{Kind: contract.EnvLiteral, Text: "&c="},
		{Kind: contract.EnvConfig, Text: "{config.mode}", Name: "mode"},
		{Kind: contract.EnvLiteral, Text: "{ x }"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ParseEnvironment = %+v, want %+v", got, want)
	}
}

// TestLinuxCapEntry checks the hardening.cap_add messages and accepted names (spec 0022): a
// misspelt known name gets its one spelling suggested, and any kernel name passes the catalog
// even when a deployed graph would refuse it.
func TestLinuxCapEntry(t *testing.T) {
	t.Parallel()
	caps := testCaps(t)
	tests := map[string]struct {
		capAdd string
		want   []string // path: message
	}{
		"accepted":        {"[NET_BIND_SERVICE, SYS_ADMIN]", nil},
		"empty":           {"[]", nil},
		"lower case":      {"[net_bind_service]", []string{`hardening.cap_add.0: "net_bind_service" is not a Linux capability name; write NET_BIND_SERVICE`}},
		"prefix":          {"[CAP_NET_BIND_SERVICE]", []string{`hardening.cap_add.0: "CAP_NET_BIND_SERVICE" is not a Linux capability name; write NET_BIND_SERVICE`}},
		"lower prefix":    {"[cap_chown]", []string{`hardening.cap_add.0: "cap_chown" is not a Linux capability name; write CHOWN`}},
		"unknown":         {"[NOT_A_CAP]", []string{`hardening.cap_add.0: "NOT_A_CAP" is not a Linux capability name`}},
		"all":             {"[ALL]", []string{"hardening.cap_add.0: ALL is not allowed here; name each Linux capability"}},
		"twice":           {"[CHOWN, NET_RAW, CHOWN]", []string{"hardening.cap_add.2: CHOWN is already listed at hardening.cap_add.0"}},
		"last kernel cap": {"[CHECKPOINT_RESTORE]", nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := edit(t, goodModule, [][2]string{{"tmpfs: [/tmp]", "tmpfs: [/tmp]\n  cap_add: " + tc.capAdd}})
			_, problems := contract.CheckModule("modules/svc/module.yaml", []byte(data), caps)
			var got []string
			for _, p := range problems {
				if p.Code != contract.CodeInvalidHardening {
					t.Errorf("unexpected problem %+v", p)
					continue
				}
				got = append(got, p.Path+": "+p.Message)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("problems = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestUndeployableLinuxCaps lists, in entry order, the cap_add names a deployed graph refuses
// (spec 0022).
func TestUndeployableLinuxCaps(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		hardening *contract.Hardening
		want      []string
	}{
		"no hardening":    {nil, nil},
		"none":            {&contract.Hardening{}, nil},
		"deployable":      {&contract.Hardening{CapAdd: []string{"NET_BIND_SERVICE"}}, nil},
		"mixed":           {&contract.Hardening{CapAdd: []string{"SYS_ADMIN", "NET_BIND_SERVICE", "CHOWN"}}, []string{"SYS_ADMIN", "CHOWN"}},
		"other spellings": {&contract.Hardening{CapAdd: []string{"net_bind_service", "CAP_NET_BIND_SERVICE", "ALL"}}, []string{"net_bind_service", "CAP_NET_BIND_SERVICE", "ALL"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := contract.Module{Hardening: tc.hardening}.UndeployableLinuxCaps()
			if !slices.Equal(got, tc.want) {
				t.Errorf("UndeployableLinuxCaps() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestModuleHostAccess joins every reason a deployed graph refuses into one phrase (spec 0022).
func TestModuleHostAccess(t *testing.T) {
	t.Parallel()
	publish := []contract.Publish{{Port: 80}}
	mounts := []contract.HostMount{{}}
	caps := func(names ...string) *contract.Hardening { return &contract.Hardening{CapAdd: names} }
	tests := map[string]struct {
		m    contract.Module
		want string
	}{
		"nothing":        {contract.Module{}, ""},
		"deployable cap": {contract.Module{Hardening: caps("NET_BIND_SERVICE")}, ""},
		"publish":        {contract.Module{Publish: publish}, "publishes host ports"},
		"two reasons":    {contract.Module{Publish: publish, Hardening: caps("SYS_ADMIN")}, "publishes host ports and adds the Linux capability SYS_ADMIN"},
		"three reasons":  {contract.Module{Publish: publish, HostMounts: mounts, Hardening: caps("SYS_ADMIN")}, "publishes host ports, mounts host paths and adds the Linux capability SYS_ADMIN"},
		"two caps":       {contract.Module{Hardening: caps("SYS_ADMIN", "NET_BIND_SERVICE", "NET_ADMIN")}, "adds the Linux capabilities SYS_ADMIN and NET_ADMIN"},
		"three caps":     {contract.Module{Hardening: caps("CHOWN", "SETUID", "SETGID")}, "adds the Linux capabilities CHOWN, SETUID and SETGID"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.m.HostAccess(); got != tc.want {
				t.Errorf("HostAccess() = %q, want %q", got, tc.want)
			}
		})
	}
}
