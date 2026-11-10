# Module SDK contract

This is the language neutral contract of the module SDK, catalog format `modular-platform/v1` (spec 0011). It is for you if you build a second language SDK, a config server (#17), a policy server (#21) or anything else that has to start, configure, call or check a module on the SDK without reading the Go code. The design and its reasons live in [spec 0005](../docs/specs/0005-module-sdk/index.md); where this file and the spec disagree, the spec wins and this file is the bug.

## How to read this

- Each statement has an id (`ENV-3`) and names the Go tests that check it. A test link points at the file that holds the test, relative to this folder.
- [`TestContract`](contract_test.go) fails when a statement or a table row names no test, when a named test is missing from its file, or when an id repeats. Run it with `go test ./sdk/...`.
- Most tests run with `task test`. The ones in `data_integration_test.go` and `tls_integration_test.go` need Docker and the build tag `integration`; the one in `../libs/render/e2e_test.go` needs the tag `e2e`. `task test:integration` runs both.
- "Exit 1" means the process stops with exit code 1 before it listens on any port.
- Names in `<angle brackets>` are placeholders: `<ENV>` is a value's env name, `<cap>` a capability, `<i>` an instance, `<graph>` the graph name.

## 1. Catalog entry fields

An SDK reads the module's own catalog entry. It needs these fields; everything else in the entry belongs to the checks and the render.

- **CAT-1**: An SDK reads a v1 module entry's `runtime: sdk`, the `config` section, each slot's `uses.<slot>.capability`, `client` and `env` (keyed by the capability's field names), each provide's `provides.<p>.capability`, `auth` and `env.token`, and the `fields` of every capability file those name (`catalog/capabilities/<name>.yaml`: each field's `type`, `direction` and `optional`). It refuses an entry that breaks one of their rules, with the same codes as the catalog check. The fixtures in `contracts/fixtures/catalog/sdk-fields.yaml` hold one accepted and one refused case per rule. Tests: [`TestCheckFixtures`](manifest/manifest_test.go), [`TestSDKFieldFixtures`](../libs/contract/sdk_fields_test.go), [`TestGenerateRefuses`](cmd/sdkgen/generate_test.go)
- **CAT-2**: A config value has a name (the map key, under the graph format's naming rule, and unique together with the entry's `secrets` names), an `env`, a `type`, a one line `description` that does not start with `#`, and optionally `default`, `optional`, `min`, `max`, `pattern`, `values` and `generate`. Tests: [`TestCheckFixtures`](manifest/manifest_test.go)
- **CAT-3**: A value's `env` matches `^[A-Z][A-Z0-9_]*$`, does not end in `_FILE`, is not a bootstrap name, and is unique in the entry, counting every `uses.*.env` and `provides.*.env` name, every `secrets[*].env` name, every `environment` name and the `<ENV>_FILE` form of every value. Tests: [`TestCheckFixtures`](manifest/manifest_test.go)
- **CAT-4**: Rules go with types: `min` and `max` only on `int` and `duration` (with `min` not above `max`), `pattern` only on `string` (RE2), `values` only on `enum` (required, at least one, unique, none empty), `generate` only on `secret`, and no `default` on a `secret`. A `default` must pass the value's own rules. Tests: [`TestCheckFixtures`](manifest/manifest_test.go)
- **CAT-5**: `uses.<slot>.client` is `http`, `postgresql`, `s3` or absent, and only a `runtime: sdk` entry may set it (likewise `per_caller`). A slot's `env` keys are fields of its capability that reach the consumer; an unknown one is `unknown_field`. A client binding must map `url`; `http` also `token` when its capability has one; `postgresql` also `database`, `user` and `password`; `s3` also `bucket`, `access_key_id` and `secret_access_key`. The sdk's binding loader reads a `secret` or `certificate` field under `<field>_file` (`password` becomes `password_file`, `token` becomes `token_file`, `ca` becomes `ca_file`), so `sdkgen` writes those keys into the binding spec. Tests: [`TestCheckFixtures`](manifest/manifest_test.go)
- **CAT-6**: A `runtime: sdk` entry has at most one provide and sets no `port` on it. `provides.<p>.auth` is `binding-token` or absent; `binding-token` needs `env.token`, the env that gets the clients directory of the tokens. A provide's `env` keys are fields of direction `to_provider` or `both`. Tests: [`TestCheckFixtures`](manifest/manifest_test.go)
- **CAT-7**: A binding to a provider with `auth: binding-token` whose consumer entry maps no `token` is the error `missing_token_mapping` at `modules.<consumer>.uses.<slot>`. Tests: [`TestCheck`](../libs/check/check_test.go)

## 2. Bootstrap names

Bootstrap names are fixed. A module never declares them, and the SDK reads them from the process env only, whatever other source is set.

| Name | Required | Meaning | Tests |
|---|---|---|---|
| `MP_INSTANCE` | yes | the instance name; the first label of the host name and the `iss` the module mints | [`TestStartProblems`](sdk_test.go), [`TestRenderSDKRules`](../libs/render/sdk_test.go) |
| `MP_DOMAIN` | in TLS mode | the suffix of the host name; the render writes `<graph>.internal` | [`TestStartLog`](sdk_test.go), [`TestTLSProblems`](tls_test.go) |
| `MP_ENV_FILE` | no | env file paths, separated by `:` | [`TestPrecedence`](sdk_test.go) |
| `MP_ACME_URL` | with `MP_CA_FILE` | the issuer's ACME directory URL, `https` only | [`TestTLSProblems`](tls_test.go) |
| `MP_CA_FILE` | with `MP_ACME_URL` | the root CA (PEM) of the issuer and of every module certificate | [`TestTLSProblems`](tls_test.go), [`TestTLSBindingTrust`](tls_test.go) |
| `MP_CALLER_SIGNING_KEY_FILE` | no | PEM (PKCS 8) Ed25519 private key; only `api` gets it | [`TestCallerEndToEnd`](caller_test.go), [`TestCallerKeyFileProblems`](caller_test.go) |

- **BOOT-1**: No entry may use a bootstrap name as a value's `env`, and with a push source set the SDK still reads the bootstrap names from the process env (and only them). Tests: [`TestCheckFixtures`](manifest/manifest_test.go), [`TestPushManagedEnv`](sources_test.go)
- **BOOT-2**: An unset or empty `MP_INSTANCE` is `missing`; one that breaks the instance naming rule is `invalid`. Tests: [`TestStartProblems`](sdk_test.go)
- **BOOT-3**: `MP_DOMAIN` is one or more DNS labels joined by `.`, each matching `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`. Anything else is `invalid`, in plain mode too. Tests: [`TestStartProblems`](sdk_test.go)
- **BOOT-4**: The host name is `<MP_INSTANCE>.<MP_DOMAIN>`, or `MP_INSTANCE` alone when `MP_DOMAIN` is unset (plain mode only). It is the one name a module goes by: its certificate name, the server name clients verify, its network alias, the host in binding URLs that point at it, and the only `aud` it accepts. Tests: [`TestStartLog`](sdk_test.go), [`TestTLSMode`](tls_test.go), [`TestCallerEndToEnd`](caller_test.go), [`TestRenderSDKRules`](../libs/render/sdk_test.go)

## 3. Where a declared value comes from

Levels, highest first. The first level that sets a value wins whole; lower levels are not read for it.

1. the start source, asked by value name (section 13)
2. the process env: `<ENV>`, or the file that `<ENV>_FILE` names
3. the env files in `MP_ENV_FILE`, a later file above an earlier one; each file may hold `<ENV>` or `<ENV>_FILE`
4. the `default`
5. else `missing`, unless the value is `optional`

- **SRC-1**: Every type in the core set (`string`, `int`, `bool`, `duration`, `url`, `enum`, `secret`) loads from `<ENV>`, from the file `<ENV>_FILE` names, from an env file line `<ENV>=` and from an env file line `<ENV>_FILE=`, and every type but `secret` from its `default`. Tests: [`TestSources`](sdk_test.go)
- **SRC-2**: The levels rank as listed: the process env beats every env file, a later env file beats an earlier one, an earlier file still fills a value a later one leaves out, and the default comes last. Tests: [`TestPrecedence`](sdk_test.go), [`TestStartSource`](sources_test.go)
- **SRC-3**: `<ENV>` and `<ENV>_FILE` share one slot per level: a process env `<ENV>_FILE` beats an env file's `<ENV>`, a later file's `<ENV>_FILE` beats an earlier file's `<ENV>`, and both forms at one level (the process env, or one env file) are `ambiguous_source`. Tests: [`TestPrecedence`](sdk_test.go), [`TestStartProblems`](sdk_test.go), [`TestManyProblems`](sdk_test.go)
- **SRC-4**: A file named by `<ENV>_FILE` is read whole with trailing whitespace trimmed; one that cannot be read is `invalid`. Tests: [`TestSources`](sdk_test.go), [`TestStartProblems`](sdk_test.go)
- **SRC-5**: An empty value counts as unset everywhere: an empty env var, an empty env file line, and a `_FILE` file that is empty after trimming. A required value with nothing set is `missing`. Tests: [`TestPrecedence`](sdk_test.go), [`TestManyProblems`](sdk_test.go)
- **SRC-6**: An optional value without a default that nothing sets is unset (label `unset`), and the module can tell it apart from an empty or default value. Tests: [`TestOptionalValue`](sdk_test.go), [`TestStartLog`](sdk_test.go)
- **SRC-7**: A `secret` comes only from the start source or from files: `<ENV>_FILE` (in the process env or an env file) or an env file line. As a plain process env var it is `secret_in_env`. On a plain env file line it loads, with one warning per value (`secret on a plain env file line`) that names the line, not the value. Tests: [`TestSecretInEnv`](sdk_test.go), [`TestSecretNeverShows`](sdk_test.go), [`TestSources`](sdk_test.go)
- **SRC-8**: Text is parsed by type: `int` is a decimal 64 bit integer; `bool` is exactly `true` or `false`; `duration` is Go syntax like `90s` (a bare number is `invalid`); `url` is absolute with a scheme and a host; `string`, `enum` and `secret` are taken as they are. A value that does not parse is `invalid`. Tests: [`TestRulesParse`](manifest/manifest_test.go), [`TestStartProblems`](sdk_test.go)
- **SRC-9**: A parsed value is then checked by its rules: outside `min` or `max` is `out_of_range`, a string the `pattern` does not match whole is `no_match`, and an enum text not in `values` is `not_allowed`. Tests: [`TestRulesParse`](manifest/manifest_test.go), [`TestManyProblems`](sdk_test.go), [`TestStartProblems`](sdk_test.go)

## 4. Env file syntax

- **EF-1**: An env file is UTF 8 with LF or CRLF line ends. Each line is blank, a comment (`#` after optional spaces), or `KEY=value` with `KEY` matching `^[A-Z][A-Z0-9_]*$` from the first column and no space before `=`. A missing last newline is fine. Tests: [`TestParse`](envfile/envfile_test.go)
- **EF-2**: The value runs to the end of the line and may hold `=`. When it starts and ends with the same quote (`'` or `"`), the quotes are removed and nothing inside is unescaped. There is no `export`, no `${VAR}` expansion, no inline comment and no multi line value. Tests: [`TestParse`](envfile/envfile_test.go), [`TestPrecedence`](sdk_test.go)
- **EF-3**: A line that breaks the syntax, a key twice in one file, and a listed file that does not exist are each `env_file`, reported against `MP_ENV_FILE` with `<file>:<line>` in the message for a line. Tests: [`TestParse`](envfile/envfile_test.go), [`TestStartProblems`](sdk_test.go), [`TestManyProblems`](sdk_test.go)
- **EF-4**: A key that is neither a declared value, the `_FILE` form of one, a binding env key nor a bootstrap name is `unknown_key`, with the key as the value part. Tests: [`TestStartProblems`](sdk_test.go)
- **EF-5**: An env file problem never repeats the value on the line, since it may be a secret. Tests: [`TestParseNeverRepeatsAValue`](envfile/envfile_test.go)

## 5. Problem lines

- **PL-1**: Each problem is one error log line whose message is `config: <value> (<ENV>): <code>: <message>`. Tests: [`TestManyProblems`](sdk_test.go), [`TestStartProblems`](sdk_test.go)
- **PL-2**: The value part is the value's name for a declared value; the name itself for a bootstrap name or an undeclared env file key or snapshot name; `uses.<cap>.<key>` for a binding key; `provides.<cap>.clients_dir` for the clients directory; `module` for `validate` and for the combined sources line; `push` for `no_snapshot`. Tests: [`TestStartProblems`](sdk_test.go), [`TestBindingProblems`](bindings_test.go), [`TestClientsDirProblems`](bindings_test.go), [`TestValidate`](sdk_test.go), [`TestStartSourceProblems`](sources_test.go), [`TestPushSnapshots`](sources_test.go), [`TestPushNoSnapshot`](sources_test.go)
- **PL-3**: A value reports only its first failing rule, in this order: source (`source_error`, `ambiguous_source`, `secret_in_env`, `managed_env`), presence (`missing`), type (`invalid`), then `out_of_range`, `no_match`, `not_allowed`. Tests: [`TestStartProblems`](sdk_test.go), [`TestManyProblems`](sdk_test.go)
- **PL-4**: All problems of a start are reported in one pass, sorted by value part (byte order), then file, then line. Then the module exits 1 without running its setup and logs no `config loaded`. Tests: [`TestManyProblems`](sdk_test.go)
- **PL-5**: A module may add one last check of its loaded values (`Validate` in Go). It runs only when no other problem was found, and its error is the one line `config: module (module): validate: <error>`. Tests: [`TestValidate`](sdk_test.go)

| Code | When | Tests |
|---|---|---|
| `missing` | a required value, bootstrap name, binding key or consumer token is not set | [`TestManyProblems`](sdk_test.go), [`TestBindingProblems`](bindings_test.go), [`TestClientsDirProblems`](bindings_test.go) |
| `invalid` | text that does not parse by type, an unreadable file, a bad bootstrap name or key file | [`TestStartProblems`](sdk_test.go), [`TestBindingProblems`](bindings_test.go), [`TestCallerKeyFileProblems`](caller_test.go) |
| `out_of_range` | below `min` or above `max` | [`TestStartProblems`](sdk_test.go) |
| `no_match` | a string the `pattern` does not match whole | [`TestManyProblems`](sdk_test.go) |
| `not_allowed` | an enum text not in `values` | [`TestManyProblems`](sdk_test.go) |
| `ambiguous_source` | `<ENV>` and `<ENV>_FILE` at one level | [`TestManyProblems`](sdk_test.go), [`TestStartProblems`](sdk_test.go) |
| `secret_in_env` | a secret as a plain process env var | [`TestSecretInEnv`](sdk_test.go) |
| `env_file` | a bad env file line, a key twice, a missing env file | [`TestStartProblems`](sdk_test.go) |
| `unknown_key` | an undeclared env file key or snapshot name | [`TestStartProblems`](sdk_test.go), [`TestPushSnapshots`](sources_test.go) |
| `managed_env` | a declared value in env or an env file while a push source is set | [`TestPushManagedEnv`](sources_test.go) |
| `invalid_binding` | a bad clients directory entry, a `sslmode` other than `disable` or `verify-full` | [`TestClientsDirProblems`](bindings_test.go), [`TestBindingProblems`](bindings_test.go) |
| `downgrade_refused` | in TLS mode, an `http://` binding URL to a module on the SDK | [`TestTLSProblems`](tls_test.go) |
| `tls_config` | one TLS setting without the other, TLS without `MP_DOMAIN`, a CA file without a certificate | [`TestTLSProblems`](tls_test.go) |
| `validate` | the module's own last check failed | [`TestValidate`](sdk_test.go) |
| `source_error` | the start source failed, missed its deadline, or was combined with a push source | [`TestStartSourceProblems`](sources_test.go) |
| `no_snapshot` | no pushed snapshot accepted within 30 seconds | [`TestPushNoSnapshot`](sources_test.go) |

## 6. Start log

- **LOG-1**: A successful start logs one Info line with the message `config loaded` and the attributes `host` (the host name), `tls` (`tls` or `plain`), a group `values` (value name to source label, and each field of a bound slot with no client as `uses.<slot>.<field>`) and a group `bindings` (capability to client kind). It never holds a value. Tests: [`TestStartLog`](sdk_test.go), [`TestTLSMode`](tls_test.go), [`TestFieldBinding`](fields_test.go)
- **LOG-2**: The source labels are `source`, `env`, `file:<path>`, `env-file:<path>:<line>`, `default`, `push` and `unset`, and `binding` for a slot field that was delivered (`unset` for one that was not). A value from an env file's `<ENV>_FILE` line carries `file:<path>` of the file it names. Tests: [`TestSources`](sdk_test.go), [`TestStartLog`](sdk_test.go), [`TestStartSource`](sources_test.go), [`TestPushSnapshots`](sources_test.go), [`TestFieldBinding`](fields_test.go)
- **LOG-3**: A plain mode start logs the warning `serving plain HTTP: no certificate issuer bound` once, and a provider that turned binding auth off logs `binding auth off: anonymous calls allowed` once. Tests: [`TestStartLog`](sdk_test.go), [`TestAllowAnonymous`](bindings_test.go)
- **LOG-4**: No secret, binding token, password, secret key, slot certificate's content or start source error text ever reaches a log line, a problem line or the module's printed values. Tests: [`TestSecretNeverShows`](sdk_test.go), [`TestSecretInEnv`](sdk_test.go), [`TestHTTPBinding`](bindings_test.go), [`TestDataBindings`](bindings_test.go), [`TestClientsDirProblems`](bindings_test.go), [`TestTokenHidden`](httpclient/httpclient_test.go), [`TestStartSourceProblems`](sources_test.go), [`TestFieldBinding`](fields_test.go)

## 7. Serving and the healthcheck

- **SRV-1**: In plain mode a module serves HTTP on port 8080. In TLS mode it serves TLS only, on port 8443, and does not listen on the plain port. Tests: [`TestSDKPairEndToEnd`](../libs/render/e2e_test.go), [`TestTLSMode`](tls_test.go), [`TestTLSAgainstPebble`](tls_integration_test.go)
- **SRV-2**: `/healthz` and `/readyz` need no binding token and no caller token. Every other path passes binding token auth (section 9) and then the caller check (section 11). Tests: [`TestBindingAuth`](bindings_test.go), [`TestInternalAuth`](../services/renderer/internal/api/server_test.go)
- **SRV-3**: The `healthcheck` subcommand reads only the bootstrap names and loads no config. It calls `/readyz` on `127.0.0.1`: in plain mode over HTTP on the plain port, in TLS mode over HTTPS on the TLS port with the host name as server name, trusting `MP_CA_FILE`. It exits 0 only on a 200, and 1 on any other answer, on no answer, or on a missing or bad bootstrap name. Tests: [`TestPlainHealthcheck`](tls_test.go), [`TestTLSMode`](tls_test.go), [`TestTLSAgainstPebble`](tls_integration_test.go)
- **SRV-4**: A module may add commands (`WithCommand` in Go), run as `<binary> <name> [args...]`, such as `api admin reset`. A command loads and checks the config and opens the bindings like a start, with the same problem lines, but runs no setup, listens on no port, gets no certificate, and logs neither `config loaded` nor the start warnings. It exits 0 when it succeeds, and 1 on a refused start, an unknown name or its own error (logged as `command failed`). Tests: [`TestCommand`](command_test.go)

## 8. Bindings, consumer side

- **BIND-1**: A binding `url` is absolute: `http` or `https` for `http` and `s3`, `postgres` for `postgresql`, the last without credentials or a database in it. Anything else is `invalid` for `uses.<cap>.url`. Tests: [`TestBindingProblems`](bindings_test.go)
- **BIND-2**: The HTTP client sends `Authorization: Bearer <token>` (the `token_file` content, trailing whitespace trimmed) on each call to the binding's own address and below its base path, and to no other host. An empty token file is `missing`, an unreadable one `invalid`. Tests: [`TestBearer`](httpclient/httpclient_test.go), [`TestNoToken`](httpclient/httpclient_test.go), [`TestHTTPBinding`](bindings_test.go), [`TestBindingProblems`](bindings_test.go)
- **BIND-3**: The PostgreSQL client signs in to the `url` host and port with `user`, the password from `password_file` and `database`. libpq env vars change none of it. `sslmode` is `disable` or `verify-full` (anything else is `invalid_binding`); `verify-full` speaks TLS to the `url` host and trusts `ca_file`. Tests: [`TestDataBindings`](bindings_test.go), [`TestEnvIgnored`](pg/pg_test.go), [`TestVerifyFull`](bindings_test.go), [`TestBindingProblems`](bindings_test.go), [`TestPostgreSQLIntegration`](data_integration_test.go)
- **BIND-4**: The S3 client uses the `url` as its endpoint, `access_key_id`, the key from `secret_access_key_file` and `region`, and carries `bucket`. Tests: [`TestDataBindings`](bindings_test.go), [`TestS3Integration`](data_integration_test.go)
- **BIND-5**: An optional binding the graph left unbound does not stop the start; asking for its client says it is unbound. Tests: [`TestUnboundOptional`](bindings_test.go)
- **BIND-6**: A slot with no `client` that maps at least one field in `env` gets a getter, `Uses.<Slot>(ctx)` in Go, returning a struct with one member per mapped capability field, typed by the capability file: `string`, `int`, `bool`, `url` (parsed, absolute), `enum` (a generated string type), `secret` (the content of the file its env names, trailing whitespace trimmed) and `certificate` (the file's PEM). An optional field not delivered is nil. A slot that maps no field gets no getter. Tests: [`TestGenerateFields`](cmd/sdkgen/generate_test.go), [`TestGenerateUses`](cmd/sdkgen/generate_test.go), [`TestFieldBinding`](fields_test.go)
- **BIND-7**: The fields of such a slot are read and checked once at start, with the config values. A required field unset is `missing`; a text its type refuses is `invalid`; a `pattern` or `values` miss is `no_match` or `not_allowed`; a secret or certificate file that cannot be read is `invalid`, an empty secret file `missing`, and a certificate file with no PEM certificate `invalid`. Each is one line `config: uses.<slot>.<field> (<ENV>): <code>: <message>`. Tests: [`TestFieldBindingProblems`](fields_test.go)
- **BIND-8**: An optional such slot with every mapped env unset or empty is unbound, and its getter says so; with any one set, its required fields must all be present. Tests: [`TestFieldBinding`](fields_test.go), [`TestFieldBindingProblems`](fields_test.go)
- **BIND-9**: A `BindingResolver` answer for such a slot (its fields in the getter's own types) replaces the slot's fields for that call and is checked by the BIND-7 rules; a bad answer, including a field the slot does not map, is an error and never a partial value. Tests: [`TestFieldBindingResolver`](fields_test.go)

## 9. Binding tokens and their paths

- **TOK-1**: A provider with `auth: binding-token` reads its clients directory once at start. Each entry is one consumer: the file name is the consumer's instance name, the content (trailing whitespace trimmed) its token. Hidden entries (starting with `.`) are skipped, and a symlink to a regular file counts as that file. Tests: [`TestBindingAuth`](bindings_test.go), [`TestHiddenAndLinkedClients`](bindings_test.go)
- **TOK-2**: An entry that is not a regular file, a file name that breaks the instance naming rule, an empty file, a token under 32 characters, and two files with the same token are each `invalid_binding`. Tests: [`TestClientsDirProblems`](bindings_test.go)
- **TOK-3**: An unset `clients_dir` env or a directory without any token is `missing`, unless the module turned binding auth off for this run (the renderer does in public mode); then the provider starts without a clients directory and takes calls without a token. Tests: [`TestClientsDirProblems`](bindings_test.go), [`TestStartRefused`](../services/renderer/internal/api/server_test.go), [`TestAllowAnonymous`](bindings_test.go)
- **TOK-4**: A call without a token, with a token that is in no file, or with an `Authorization` header that is not `Bearer <token>` gets 401 with a problem list holding one problem `unauthorized`. A valid token makes the calling module known as the matching file's name. A removed file refuses only that consumer, from the next start. Tests: [`TestBindingAuth`](bindings_test.go), [`TestInternalAuth`](../services/renderer/internal/api/server_test.go)
- **TOK-5**: The render gives each binding to a `binding-token` provider one Compose secret `<c>_uses_<slot>_token` from `./secrets/<c>/uses_<slot>_token`, which `setup.sh` creates once. It is mounted on the consumer at `/run/secrets/uses/<slot>/token` (its `token` env) and on the provider at `/run/secrets/clients/<provide>/token/<c>` (its `env.token` is `/run/secrets/clients/<provide>/token`). Tests: [`TestRenderSDKRules`](../libs/render/sdk_test.go), [`TestRenderSDKGolden`](../libs/render/sdk_test.go)
- **TOK-6**: Every `secret` or `certificate` field reaches the consumer only as a file, mounted at `/run/secrets/uses/<slot>/<field>` (or the slot's `files` path) with its env set to that path. A binding to a provisioner gets its password as a per binding file `./secrets/<c>/uses_<slot>_password`, which `setup.sh` creates; a secret field of an external instance is a file you place at `./secrets/<external>/<provide>_<field>`, which `setup.sh` checks and does not create. Tests: [`TestRenderSampleRules`](../libs/render/render_test.go), [`TestRenderExternal`](../libs/render/sdk_test.go)
- **TOK-7**: Two rendered SDK modules started with `setup.sh` and `docker compose up` reach each other through their binding (200), and a call without the token gets 401. Tests: [`TestSDKPairEndToEnd`](../libs/render/e2e_test.go)

## 10. TLS and ACME

- **TLS-1**: TLS mode needs `MP_ACME_URL` (an `https` URL), `MP_CA_FILE` (readable, with at least one certificate) and `MP_DOMAIN`. Either TLS setting alone, TLS without `MP_DOMAIN`, or a bad CA file is `tls_config`; an `http` ACME URL is `invalid`. Neither TLS setting means plain mode. Tests: [`TestTLSProblems`](tls_test.go), [`TestStartProblems`](sdk_test.go), [`TestStartLog`](sdk_test.go)
- **TLS-2**: The server speaks TLS 1.3 only and offers the protocols `h2`, `http/1.1` and `acme-tls/1`. A plain HTTP request or a TLS 1.2 handshake on the TLS port fails and never reaches the module. Tests: [`TestServerConfig`](tlsacme/manager_test.go), [`TestTLSMode`](tls_test.go), [`TestTLSAgainstPebble`](tls_integration_test.go)
- **TLS-3**: The module gets one ECDSA P-256 certificate for its host name from the ACME server over `tls-alpn-01`, and presents it to every client, whatever server name the client sends. Only the module's own certificate loop orders; a handshake never does, so many clients make one order. Tests: [`TestTLSAgainstPebble`](tls_integration_test.go), [`TestChallengeRouting`](tlsacme/manager_test.go)
- **TLS-4**: Only a hello that offers `acme-tls/1` and nothing else gets the validation answer, and only for the name being ordered (case and a trailing dot ignored) while that order waits. Any other hello gets the held certificate; before one is held, the handshake fails. Tests: [`TestChallengeRouting`](tlsacme/manager_test.go), [`TestClientIssue`](tlsacme/client_test.go)
- **TLS-5**: One order registers the account if needed (an account that already exists counts as registered), orders exactly the host name, settles each authorization (`valid` needs nothing, `pending` gets its `tls-alpn-01` challenge, any other state fails the attempt), and sends a CSR with an empty subject, the host name as its only DNS name and a fresh ECDSA P-256 key. An order naming anything else, or without a `tls-alpn-01` challenge, fails the attempt. Tests: [`TestClientIssue`](tlsacme/client_test.go), [`TestClientRefusals`](tlsacme/client_test.go)
- **TLS-6**: When the ACME server answers `urn:ietf:params:acme:error:accountDoesNotExist`, the next attempt registers again; otherwise the account is registered once per process. Tests: [`TestClientRegistersAgain`](tlsacme/client_test.go)
- **TLS-7**: An issued certificate is held only when its leaf verifies against `MP_CA_FILE` for the host name with server auth usage (a CA clock up to one minute ahead is tolerated), matches its private key, and has not expired. Anything else is a failed attempt while the old certificate keeps serving. Tests: [`TestIssuedCheck`](tlsacme/manager_test.go)
- **TLS-8**: The next order starts once two thirds of the held leaf's lifetime (`NotBefore` to `NotAfter`) have passed, and never sooner than 30 seconds after the last issue. Tests: [`TestRenewal`](tlsacme/manager_test.go), [`TestTLSAgainstPebble`](tls_integration_test.go)
- **TLS-9**: A failed attempt logs one warning and is retried after 1, 2, 4, 8, 16 and then 30 seconds, for as long as it takes; the wait starts at 1 second again after a success. Each attempt is cut off after 5 minutes. Tests: [`TestBackoff`](tlsacme/manager_test.go), [`TestAttemptTimeout`](tlsacme/manager_test.go)
- **TLS-10**: Binding clients in TLS mode trust `MP_CA_FILE` (or the binding's own `ca_file`) and verify the server name. An `http://` binding URL whose host ends in `.<MP_DOMAIN>` (case ignored) is `downgrade_refused`; an `https://` URL to such a host, or an `http://` URL to any other host, is fine. Tests: [`TestTLSBindingTrust`](tls_test.go), [`TestTLSProblems`](tls_test.go), [`TestTLSHTTPSBindingToSDK`](tls_test.go)

## 11. The `MP-Caller` token

- **CALL-1**: `MP-Caller` carries a compact JWS with `alg` `EdDSA` and `kid` set to the RFC 7638 thumbprint of the Ed25519 public key, so no key id is stored on either side. Tests: [`TestMintVerify`](caller/caller_test.go), [`TestKID`](caller/caller_test.go), [`TestMintRefusesOtherKeys`](caller/caller_test.go)
- **CALL-2**: Its claims are `iss` (the minting module's `MP_INSTANCE`), `sub` (the user), `groups` (a list of strings, empty when none), `aud` (one host name, as a string), `iat`, `exp` (`iat` plus 5 minutes) and `jti` (16 random bytes, base64url, new per token). Tests: [`TestMintVerify`](caller/caller_test.go), [`TestSignerHidesKey`](caller/caller_test.go)
- **CALL-3**: A receiver refuses a token whose `alg` is not `EdDSA`, whose `kid` names no key of the calling module in the caller keys directory (CALL-6) or whose signature does not verify with it, whose `aud` is not exactly its own host name (one audience only), that lacks `exp`, `iat` or a non empty `sub`, that expired or whose `iat` lies in the future (30 seconds of leeway each), that lives longer than 5 minutes, or that is not in compact form. Tests: [`TestVerifyRefusals`](caller/caller_test.go)
- **CALL-4**: A token counts only on a call that passed binding token auth, only when its `iss` is the calling module the binding token proved, and only when it is signed with that module's own key: a token signed with another consumer's key is refused (spec 0015, AC-18). Two `MP-Caller` headers are refused. Every refusal gets 401 with one problem `invalid_token`, and the reason is logged, never the token. Tests: [`TestMiddleware`](caller/caller_test.go), [`TestCallerEndToEnd`](caller_test.go), [`TestLoadKeys`](caller/caller_test.go)
- **CALL-5**: A call without the header has no caller and is not an error. With no caller keys directory, the header is ignored (no caller) and logged once. Tests: [`TestMiddleware`](caller/caller_test.go), [`TestMiddlewareWithoutKeys`](caller/caller_test.go)
- **CALL-6**: A provide whose capability has the `to_provider` field `caller_key` maps it in `env.caller_key` (`MP_CALLER_KEYS_DIR` on `renderer` and `deploy-target`); that env names the caller keys directory, read once at start with TOK-1's entry rules: one file per consumer, named by its instance, holding one or more Ed25519 public keys as PKIX PEM blocks. `MP_CALLER_SIGNING_KEY_FILE` holds one Ed25519 private key as PKCS 8 PEM. An unreadable directory, a bad entry or other content is a line naming the file, never its content: `invalid_binding` at `provides.<p>.caller_keys_dir`, or `invalid` for the signing key. Tests: [`TestKeyFiles`](caller/caller_test.go), [`TestLoadKeys`](caller/caller_test.go), [`TestCallerKeyFileProblems`](caller_test.go)
- **CALL-7**: A module holding the signing key adds `MP-Caller` to a call on an HTTP binding when a caller is in the call's context, minted as its own instance for the URL's host (port dropped, lower case). A URL that names the provider by IP gets a token the provider refuses. Tests: [`TestCallerEndToEnd`](caller_test.go)

## 12. Readiness

- **RDY-1**: `/readyz` answers 503 until every required binding (a `uses` entry that is not `optional`) has answered once: HTTP `GET <url>/readyz` with any status below 500 (a 404 counts), a PostgreSQL ping, an S3 bucket that exists. Each failed poll logs the warning `binding not ready` with the capability, never a credential. Tests: [`TestReadiness`](bindings_test.go), [`TestReady`](httpclient/httpclient_test.go), [`TestPostgreSQLIntegration`](data_integration_test.go), [`TestPostgreSQLSignInFails`](data_integration_test.go), [`TestS3Integration`](data_integration_test.go)
- **RDY-2**: A provider that is down at start does not stop the consumer from starting; the consumer turns ready once the provider answers. Tests: [`TestReadiness`](bindings_test.go)
- **RDY-3**: In TLS mode `/readyz` answers 503 until a certificate is held, and again while the held one has less than 2 hours left, though it still serves. Tests: [`TestTLSUnreadyWithoutCertificate`](tls_test.go), [`TestReadyTimeLeft`](tlsacme/manager_test.go), [`TestTLSMode`](tls_test.go)
- **RDY-4**: With a push source, `/readyz` and every other call answer 503 until the first snapshot is accepted. Tests: [`TestPushNoSnapshot`](sources_test.go), [`TestPushReady`](sources_test.go)

## 13. Pluggable sources

The wire protocols arrive with #17 (config server) and #21 (policy server). These statements fix the behaviour any protocol must plug into. The tests shorten the 10 second, 30 second and 200 ms times.

- **SRC-10**: A start source is asked once per declared value, by value name, in name order, all under one shared 10 second deadline. A non empty answer wins whole (label `source`), is checked like any value, and hides every lower level for that value, so no `ambiguous_source` or `secret_in_env` applies to it. Not found, or `""`, falls through. Tests: [`TestStartSource`](sources_test.go)
- **SRC-11**: A failing start source call is `source_error` for that value only, with the fixed text `the start source failed`, never the source's own error. Past the deadline, nothing more is asked, and the value in flight and each one after it get one `source_error` line. A start source together with a push source is the one line `config: module (module): source_error: WithSource and WithPushSource cannot be combined`. Tests: [`TestStartSourceProblems`](sources_test.go)
- **SRC-12**: A pushed snapshot is complete and replaces the values whole. It is checked with the start rules; any problem (a bad value, a missing required value, an undeclared name as `unknown_key`) refuses it whole and keeps the previous one. Each accepted snapshot logs `config loaded`, with `push`, `default` or `unset` per value. Tests: [`TestPushSnapshots`](sources_test.go)
- **SRC-13**: With a push source, a declared value in the process env (either form) or on an env file line is `managed_env`; an empty `KEY=` line is not. Tests: [`TestPushManagedEnv`](sources_test.go)
- **SRC-14**: With a push source and no accepted snapshot within 30 seconds, the module exits 1 with exactly `config: push (push): no_snapshot: no snapshot arrived within 30 seconds`, after the lines of any refused snapshot; a refused snapshot does not extend the wait. Tests: [`TestPushNoSnapshot`](sources_test.go)
- **SRC-15**: A caller source may answer a value differently for one caller; other callers and calls without a caller get the snapshot value. Its answer is checked like any value, and is kept per caller and value for 30 seconds in a bounded cache that drops the least recently used answer first; an accepted snapshot drops them all. Tests: [`TestCallerSource`](sources_test.go), [`TestCallerSourceSwap`](sources_test.go), [`TestLRU`](cache_test.go)
- **SRC-16**: A caller source that misses its 200 ms budget or fails makes the getter fail, without the source's error text, and the module answers 503. Tests: [`TestCallerSourceFails`](sources_test.go)
- **SRC-17**: A binding resolver may pick another provider binding for one caller; that caller reaches it with that binding's credentials, others reach the module's own binding, one client is kept per provider binding, and a resolver that cannot answer fails like a caller source. Tests: [`TestBindingResolver`](sources_test.go)

## 14. The rendered stack

What a module on the SDK finds in a Compose project from `libs/render` (plain mode in this version).

- **REN-1**: Each `runtime: sdk` instance gets `MP_INSTANCE=<i>`, `MP_DOMAIN=<graph>.internal`, `MP_ENV_FILE=/etc/modular-platform/module.env` with `config/<i>.env` mounted there, the network alias `<i>.<graph>.internal` on the `internal` network, and `user: "${MP_UID}:${MP_GID}"` (like every module now), which `setup.sh` writes into `.env` from `id -u` and `id -g`. Tests: [`TestRenderSDKRules`](../libs/render/sdk_test.go), [`TestRenderSDKGolden`](../libs/render/sdk_test.go)
- **REN-2**: `config/<i>.env` (mode 0644) holds a two line header, then per value its description and `# required`, `# optional` or the commented default. It holds no binding line, since every binding field arrives through the binding, and never a secret. Tests: [`TestRenderSDKRules`](../libs/render/sdk_test.go)
- **REN-3**: Each binding field comes from the provider's declared source (spec 0013). A `platform: url` field of a `runtime: sdk` provider is `http://<p>.<graph>.internal:8080`; of any other provider it is `<scheme>://<p>.<graph>.internal:<port>` with the capability's `scheme`; an external instance's fields come from the graph. Tests: [`TestRenderSDKRules`](../libs/render/sdk_test.go), [`TestRenderSampleRules`](../libs/render/render_test.go), [`TestRenderExternal`](../libs/render/sdk_test.go)
- **REN-4**: A `generate: true` secret value `v` of instance `i` is the file `secrets/<i>/<v>`, mounted as the Compose secret `<i>_<v>`, with `<ENV>_FILE=/run/secrets/<i>_<v>`. Tests: [`TestRenderSDKRules`](../libs/render/sdk_test.go)

## Spec rules no test checks yet

Spec 0005 also asks for these. They are worth following, but nothing here proves them, so they are not contract statements:

- the provider compares tokens in constant time (`crypto/subtle`);
- binding readiness polls back off from 1 to 30 seconds, and the HTTP client never retries a call;
- the ACME account key is a fresh ECDSA P-256 key per start, kept in memory, and nothing TLS related is cached on disk;
- the S3 client uses path style addressing and the region `us-east-1` when `region` is unset;
- the per caller cache holds at most 10,000 answers.
