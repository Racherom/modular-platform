# 0013. Module manifest v1 and its render

**Parent**: [0011. Capability and module contract](index.md)
**Extended by**: [0018](../0017-external-capability-providers/0018-identity-providers-grants.md) (#10): the login job, and one placed file per `same_as` chain (at the end of the chain). [0025](0025-slot-fields-config-delivery.md): every non secret config value of a non SDK entry renders as `${MP_<I>__<V>:-<default>}`, graph config values, and asked values the graph sets are not asked.

## Summary

This pins every field of a v1 module entry and what `libs/render` writes for it. A provide says where each field of its capability comes from; a slot says how each field reaches the container (env value, or a file and its path); install time inputs (asked values, secrets, setup steps) and run time needs (templates, data folders, host access, hardening, networks) are declared so the render, `setup.sh` and the deployer's guard all read one description. Spec 0005's `runtime`, `config`, `client` and `auth` fields keep their meaning.

## Example

```yaml
apiVersion: modular-platform/v1
kind: Module
name: postgres
description: PostgreSQL server, official image; creates a database and login per consumer.
image: postgres:18.6-alpine3.24
command: ["postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"]
provides:
  server:
    capability: postgresql-provisioner
    version: "18.6"
    port: 5432
    fields:
      host: {platform: host}
      port: {platform: port}
      url: {platform: url}
      admin_user: {value: postgres}
      admin_password: {secret: password}
      sslmode: {platform: tls, on: verify-full, off: disable}
      ca: {platform: ca}
uses:
  tls:
    capability: tls-certificate
    optional: true
    files:
      cert: /run/tls/server.crt
      key: /run/tls/server.key
secrets:
  - name: password
    env: POSTGRES_PASSWORD_FILE
    description: Superuser password.
templates:
  - file: postgresql.conf.tmpl
    path: /etc/postgresql/postgresql.conf
  - file: pg_hba.conf.tmpl
    path: /etc/postgresql/pg_hba.conf
volumes:
  data: /var/lib/postgresql
hardening:
  tmpfs: [/var/run/postgresql, /tmp]
healthcheck:
  test: ["CMD", "pg_isready", "-h", "127.0.0.1", "-U", "postgres"]
  interval: 5s
  timeout: 3s
  retries: 10
  start_period: 30s
```

## Fields

| Where | Field | Rule |
|---|---|---|
| top | `apiVersion`, `kind`, `formatVersion`, `name`, `description`, `image`, `healthcheck` | as 0011 and 0002 (`image` tagged, never `latest`; healthcheck rules unchanged) |
| top | `runtime` | `sdk` or absent (0005). An sdk entry gets an implicit optional slot `acme` (capability `acme`, delivered as `MP_ACME_URL` and `MP_CA_FILE`); declaring a slot named `acme` yourself is `reserved_slot` |
| top | `command`, `entrypoint` | lists of strings, copied as is |
| top | `environment.<NAME>` | fixed env for images that need a literal or composed value (`MP_MODE: managed`, `MP_API_ROOT: "{public.base_path}/api"`). Text with `{<slot>.<field>}` (a text field of one of its own bindings), `{config.<value>}`, `{uid}`, `{gid}`, `{install}` placeholders; an asked or Compose referenced value renders as its Compose variable. A name also declared by a config value or a slot mapping is `duplicate_env` |
| `provides.<p>` | `capability` | a known capability (`unknown_capability`) |
| | `version` | 0012's version grammar, required |
| | `port` | 1 to 65535, required unless `runtime: sdk` (then forbidden, `port_not_allowed`); an sdk entry still provides at most one provide (`too_many_provides`) |
| | `auth` | `binding-token` or absent (0005); requires a field `token` of direction `both` with source `generate` |
| | `fields.<f>` | one source per field of the capability (a provisioner: its own fields, not the yielded ones). Every required field needs one (`missing_field_source`); a field the capability lacks is `unknown_field` |
| | `env.<f>` | provider side delivery for `to_provider` and `both` fields: the env name gets the clients directory of that field (0005's `clients_dir` generalized) |
| `uses.<s>` | `capability`, `versions`, `optional`, `per_caller` | 0012; `per_caller` only with `runtime: sdk` |
| | `client` | `http`, `postgresql`, `s3` or absent, sdk only (0005). Each kind needs its fields mapped: `http` url and, if the capability has `token`, token; `postgresql` url, database, user, password; `s3` url, bucket, access_key_id, secret_access_key |
| | `env.<f>` | env name for a field; for a `secret` or `certificate` field the env gets the file path |
| | `files.<f>` | absolute path to mount a `secret` or `certificate` field at, for images that read a fixed path; may be combined with `env` |
| | `fields.<f>` | consumer side source for a `to_provider` field: `{secret: <name>}` (one of its own secrets, file fields), `{value: X}`, or `{platform: url \| host \| port, provide: <own provide>}` (text fields, plan time) |
| `config.<v>` | 0005's fields | plus `setup: ask` (setup.sh asks it, see *Install time*). A `default` may be exactly `{uid}` or `{gid}` (the setup user, rendered as `${MP_UID}` or `${MP_GID}`) or start with `{install}/` (platform flavor, `${MP_DIR}/...`); such a value is rendered as an env value, not as an env file line |
| `secrets[*]` | `name`, `description` | required; name unique with config value names. A `name` ending in `/` is a folder a setup step writes (needs `from: setup` and `mount`, no `env`) |
| | `from` | `generate` (default, 32 random bytes hex), `ask` (asked without echo), `setup` (a setup step writes it) |
| | `env`, `mount` | at least one: env gets `/run/secrets/<i>_<name>` (0002's rule), `mount` an absolute path |
| `setup[*]` | `name`, `entrypoint`, `command`, `env`, `for_each`, `asks`, `reads`, `writes` | see *Install time* |
| `templates[*]` | `file`, `path`, `delims` | a file in the module folder ending in `.tmpl`; an absolute mount path; optional `delims: ["[[", "]]"]` for files whose own syntax uses `{{ }}` (Authelia's template filter) |
| `volumes.<n>` | absolute path | a persistent data folder |
| `publish[*]` | `port`, `host` | container port; host port as `{config: <int value>}` or a number. Host access |
| `host_mounts[*]` | `host`, `path`, `read_only` | host path (absolute, `{config: <value>}`, or `{install: <relative>}`, platform flavor only, 0015), container path (absolute or the same `{install}` form), `read_only` default `true`, `create: true` for an `{install}` folder that `setup.sh` makes (mode 0700, as you) when missing. Host access |
| `network` | `egress`, `aliases` | `egress: true` joins the egress network; `aliases` lists `{config: <value>}` names added on every network |
| `hardening` | `user`, `writable_root`, `cap_add`, `tmpfs` | `user: root` or `user: image` (keep the image's user); `writable_root: true`; `cap_add` Linux capability names (upper case, no `CAP_`; a deployed graph may add only `NET_BIND_SERVICE`, 0022); `tmpfs` paths (not an opt out) |

**Field sources** (on a provide; values a template may read are marked *plan*):

| Source | Value | When |
|---|---|---|
| `{platform: host}` | the provider's host name `<i>.<graph>.internal` | *plan* |
| `{platform: port}` | the provide's `port`; for sdk 8080 plain or 8443 when its `acme` slot is bound | *plan* |
| `{platform: url}`, optional `path: /p` | `<scheme>://<host>:<port>` plus `path` when given; for sdk `http` or `https` by its TLS state | *plan*; needs the capability's `scheme` (sdk excepted) |
| `{platform: ca}` | the `ca` file of the provider's own bound `acme` or `tls-certificate` slot; absent when neither is bound (the field must be optional) | file |
| `{platform: tls, on: X, off: Y}` | `X` when the provider serves TLS (as for `ca`), else `Y` | *plan* |
| `{value: X}` | the literal (checked against the field's type and pattern) | *plan* |
| `{config: v}` | the provider's config value `v`; for an asked value the Compose variable (0011 naming), so a template cannot read it | run time |
| `{config: v, format: "https://{}"}` | the format with `{}` replaced by that value | run time |
| `{secret: n}` | the provider's own secret file `secrets/<p>/<n>` | file |
| `{generate: true}` | a per binding file, 32 random bytes hex, made by `setup.sh` (static, platform) or the deployer (managed) | file |
| `{platform: consumer}` | the consumer's instance name, per binding (an OIDC client id) | *plan* |
| `{uses: <slot>, field: <f>}` | that field of the provider's own binding on `<slot>`, text or file, passed through | as that field |
| `{uses: <slot>, format: "{origin}{base_path}/auth"}` | the format with each `{field}` replaced by that field of the provider's own binding on `<slot>` (text fields only). A field that binding does not give is empty, with no error. When any field it names is a Compose variable, the result is one too, with the literal parts escaped | as the fields it names |
| `{volume: <name>, path: <relative file>}` | a file the provider writes into its own data folder (Caddy's local root CA); the consumer gets the containing folder mounted read only at `/run/volumes/<s>/<f>/` and its env the file path in it, so the file may appear after start; the consumer depends on the provider being healthy | file |

**Catalog rules** added (path in the entry): `unknown_capability`, `unknown_field`, `missing_field_source`, `invalid_field_source` (a source whose type does not fit, like `{secret}` on a `string`, `platform: url` without a scheme), `reserved_slot`, `per_caller_not_allowed`, `invalid_hardening` (a `cap_add` name, 0022), `invalid_template`, `invalid_setup_step`, `invalid_mount` (relative path, two mounts on one path), plus every 0002 and 0005 rule still in force.

## Render

For instance `i` (module entry `e`) in graph `g`, besides 0002's and 0005's rules:

**Host name and networks**: every instance gets the alias `<i>.<g>.internal` on each network it joins. The project has `internal` (`internal: true`, every instance) and `egress` (default bridge, instances with `network.egress` or `publish`, and every consumer with a slot bound to an external instance, directly or through an external provisioner, since `internal` has no route out and the bound `host` is outside the stack; decided 2026-11-05). The render joins such a consumer by itself, in every flavor, and declares the `egress` network whenever any service joins it. Only bound slots count; an optional slot left unbound does not. So an unchanged entry binds to an external server (AC-1), and an entry bound only inside the stack stays off `egress`. The wait anchor `mp_jobs` (0014) is the one service on no network (`network_mode: none`). A provision job of an external provisioner joins `egress` by the same rule (0014). `network.aliases` add `${MP_<I>__<V>}` names. The static render's old single `default` network is gone.

**Hardening defaults**: `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `user: "${MP_UID}:${MP_GID}"`, `restart: unless-stopped` (managed and platform; static as today). `hardening.user: root` writes `user: "0:0"`, `image` omits `user`; `writable_root` omits `read_only`; `cap_add` and `tmpfs` are copied. A read only root means an image that writes anywhere outside its volumes needs a `tmpfs` entry (most need `/tmp`; Postgres needs `/var/run/postgresql`) or `writable_root` (Authelia writes `/app/.healthcheck.env`). The managed flavor's `MP_UID` and `MP_GID` are the deployer's `platform-uid` and `platform-gid`, which it writes into the project's `.env` at apply (0007 already passes them).

**Labels** (managed and platform flavors): every service gets 0006's `graph`, `instance`, `module` and `deployment` labels; a job service gets the `instance` and `module` of its provisioner instance plus `io.github.racherom.modular-platform.job` set to `provision` or `revoke`; the wait anchor `mp_jobs` (0014) gets only `graph`, `deployment` and `job: wait`. The deployer's guard allows that one key, with the values `provision`, `revoke` and `wait`.

**Volumes**: `volumes.<n>` mounts the folder `./data/<i>/<n>` (created by `setup.sh` as you, mode 0700, or by the deployer in the deployment folder) at the path. No named Compose volumes, so a non root module owns its data and *Delete data too* (0006) removes the folder.

**Binding delivery** (consumer `c`, slot `s`, provider `p`, provide `q`, capability `C`):
- non file field with `env`: the env value.
- file field (`secret`, `certificate`): Compose secret `<c>_uses_<s>_<f>` (three or more `_`, never equal to a 0002 name), file from the source (per binding: `./secrets/<c>/uses_<s>_<f>`; provider secret: `./secrets/<p>/<n>`; external: `./secrets/<p>/<q>_<f>`; certificate from a job: the job's output folder, 0014), mounted at `/run/secrets/uses/<s>/<f>` or at `files.<f>`; `env.<f>` gets that path.
- a field the consumer does not map is not delivered.
- `to_provider` and `both` fields on the provider: mounted at `/run/secrets/clients/<q>/<f>/<c>`, and the provide's `env.<f>` gets `/run/secrets/clients/<q>/<f>`. The SDK's clients directory reader (0005) reads the `token` directory unchanged in behavior.
- `depends_on`: 0002's rule toward `p`, or toward the provision job for a provisioned binding (0014).

**Asked values**: a config value with `setup: ask` becomes the Compose variable `MP_<I>__<V>` (instance and value upper cased, `-` as `_`). The service env gets `<ENV>: ${MP_<I>__<V>}`; `publish`, `host_mounts` and `network.aliases` that name it use the same variable. Two values mapping to one variable name (possible with `--` in names) is a planner error `env_name_collision`. An asked `secret` value becomes `secrets/<i>/<v>` with `<ENV>_FILE`, like a generated one. A config value that is not asked but is named by `publish`, `host_mounts`, `network.aliases` or a `{config}` field source also becomes that variable, written `${MP_<I>__<V>:-<default>}`, so you can override it in `.env`.

**Templates**: rendered with Go `text/template`, `Option("missingkey=error")`, no functions beyond the built ins, written to `config/<i>/<file without .tmpl>` (mode 0644) and mounted read only through a top level Compose `configs` entry `<i>_<name>`. Template data:

```text
.Graph        graph name
.Instance     instance name
.Host         <i>.<g>.internal
.Provides.<p> {Port}
.Uses.<s>     {Bound, Provider, Provide, Host, Port, URL, Values}   every declared slot is a key; Values holds the plan known non secret fields
.Clients.<p>  [{Instance, Slot, Host, Values}]                     every declared provide is a key; one item per binding to it, sorted by consumer then slot; Values holds its to_provider text fields
```

At catalog load each template is parsed and executed twice, once with every slot bound to placeholder values and once with every optional slot unbound; an error in either is `invalid_template`. That proves only that the keys exist, so every built in template also has a golden output in `libs/render/testdata/` from a real graph (the sample or the platform graph), reviewed like code.

**Host access**: `publish` writes `ports: ["${VAR}:<port>"]` or `"<n>:<port>"`; `host_mounts` writes bind mounts. The planner adds warning `host_access` at `modules.<i>` for each such instance (public and internal mode alike). The managed flavor answers 422 with error `host_access` instead; the platform flavor renders them.

## Install time

`setup.sh` (static and platform flavors) runs in this order, never overwriting a file and never printing a secret:
1. write `MP_UID`, `MP_GID` into `.env` once (0005);
2. asked config values, in the order the flavor states (static: instances in name order, values in name order; platform: instances in binding order, providers first, values in name order, which gives 0015's list): prompt = the value's `description`, default = its `default` (which may contain `{<other asked value of the same instance>}`, filled from the answer given), answer written as `MP_<I>__<V>=` into `.env`; secret typed values without echo into their file. An answer breaking the value's `pattern`, `min`, `max` or `values` is asked again on a terminal and stops `setup.sh` with a message naming the value when stdin is piped; an empty answer takes the default (an optional value with no default stays empty);
3. secrets with `from: ask`, same order, without echo;
4. every `generate` file: module secrets, then per binding files, in path order; then the empty output files of provision jobs (0014), mode 0600;
5. setup steps, instances in binding order (providers before consumers, ties by name), steps in declared order;
6. `data/<i>/<n>` folders and `host_mounts` with `create: true`, mode 0700;
7. files you place (external secret fields, 0012): when any is missing, `setup.sh` lists every missing path with the field's `description` and exits 1, creating nothing in their place.

A **setup step**:

```yaml
setup:
  - name: client-digest
    entrypoint: [sh, -c]
    command: ["authelia crypto hash generate pbkdf2 --password \"$(cat /in/oidc/client_secret)\" ... > /out/client-digests/$CLIENT"]
    env: {CLIENT: "{client}"}                  # literal env; the module names it; {client} only under for_each
    for_each: oidc                             # once per binding to the provide oidc
    asks: []                                   # [{name, description, secret}]: answers given on stdin, one per line, in order
    reads: [{provide: oidc, field: client_secret}]  # or {slot, field} or {secret: <own secret>}; mounted read only under /in/<slot or provide>/<field> or /in/<name>
    writes: [{name: "client-digests/{client}"}]  # files (or folders, name ending in /) in secrets/<i>/, written under /out; optional mode
```

- `entrypoint` (optional list) replaces the image's entrypoint; `command` (required list) replaces its command. An image whose entrypoint is a binary takes `command` as that binary's arguments (`api`: `command: [caller-key, generate]`); a step that needs a shell sets `entrypoint: [sh, -c]`.
- `env` (optional map) sets literal env for the step (`STEPPATH: /tmp/step`); it never holds a secret, and no key starts with `MP_` (those names belong to the platform). Every step also gets `MP_GRAPH` (the graph name) and `MP_INSTANCE`.
- `writes[*]`: `name` is a secret name, a folder name ending in `/`, or, in a `for_each` step only, `<folder>/{client}` (one file per client in that folder); a folder, in either form, is one of the module's `secrets` entries with `from: setup`. `mode` is `0600` (default) or `0644` (public material such as a root certificate). A bare folder write (`<folder>/`) counts as written when the folder exists; a `<folder>/{client}` write counts per client file, never by the folder. A module mounts a written folder through a `secrets` entry whose `name` ends in `/` and has a `mount` path (a read only bind of `secrets/<i>/<folder>`).

A step may also say `for_each: <provide>`. It then runs once per binding to that provide, in consumer instance name order. One consumer binds a provide at most once (`duplicate_binding`, 0012), so the consumer's instance name identifies the binding; that name is the step's **client**, the same value the `{platform: consumer}` field source gives (an OIDC client id). For each run:

- its reads of that provide's `to_provider` or `both` fields (`{provide: <p>, field: <f>}`) give that one client's file; its `{slot}` and `{secret}` reads work as in any step. A `{provide}` read is allowed only in a `for_each` step and only of its own provide;
- it has no `asks`: every question of `setup.sh` is asked once (**AC-10**), so a question per client has no place;
- the exact text `{client}` in its `env` values, its `command` and `entrypoint` items, and its `writes` names is replaced by the client; `reads`, the step `name` and `MP_ASK_<V>` values are never expanded. Nothing else is touched, so `${VAR}` or `{print $1}` stay as written. The step gets the client only where its entry places `{client}`: the module names its own env (`CLIENT: "{client}"`) or argument (`[--out, "/out/client-digests/{client}"]` for an image whose entrypoint is a binary and has no shell to read env). There is no fixed client env, so an image you do not change reads the client under the name it already expects. The client is an instance name, a validated DNS label (0002), so placing it in a shell command or a file name cannot inject anything;
- every `writes` name is exactly `<folder>/{client}`: one folder segment, and the file name is the bare client (no `{client}.pem`, no nesting). `setup.sh` creates `secrets/<i>/<folder>/` (0700) before the first run, even when the provide has no binding yet, so the module's mount of that folder always exists. The run for a client is skipped when every one of its files exists, so adding a client runs the step once, for that client only;
- a file of a client that is no longer bound stays. `setup.sh` never deletes a secret, and a template lists only current clients (`.Clients.<p>`), so the stale file is never read.
- a later step reads its output only as the whole folder (`{secret: <folder>/}`), never one client's file, so `setup_order` compares folder names only.

The skip goes by existence only. If you delete a client's generated `client_secret` by hand, `setup.sh` makes a new one but keeps the old digest, and that client cannot sign in until you delete its digest file too. An unbind and a rebind keep both files, so they still match. Rotation comes with #25.

`invalid_setup_step` at catalog load covers: `{client}` in a step with no `for_each`; a write of a `for_each` step that is not `<folder>/{client}`; a written folder that is not a `from: setup` folder entry in `secrets`; `asks` on a `for_each` step; a `{provide}` read outside a `for_each` step, of another provide, or of a `to_consumer` field; an `env` key starting with `MP_`; a `for_each` that names no declared provide.

The instance's own non secret asked values reach a step as env `MP_ASK_<V>`.

It runs as `docker run --rm -i --network none --user "$(id -u):$(id -g)" -v secrets/<i>:/out` plus its reads, in the module's own image (0001 principle 9). It is skipped when every `writes` entry exists (for a `for_each` step, per client), and fails `setup.sh` (`set -e`) when it exits non zero or leaves a `writes` entry missing. A step may read only files that steps 2 to 4 make, its own secrets, or outputs of steps ordered before it; anything else is planner error `setup_order`. Each written file gets its `writes` mode; `secrets/<i>/` folders are 0700.

**Managed flavor**: a graph whose instances need anything from steps 2, 3, 5 or 7 is refused with error `setup_required` at `modules.<i>` (#9 lifts this); generated files and empty job outputs (step 4), `data/` folders, and `MP_UID` and `MP_GID` in the project's `.env` are made by the deployer at apply, as 0007 already does for secrets.

## Critical test scenarios

- Golden renders (in `libs/render/testdata/`, `-update` as today) per case: an unchanged consumer with env and fixed path delivery; an sdk pair with a `both` token and a `to_provider` key; hardening opt outs; `egress` and aliases; asked values as variables; a template with a bound and an unbound slot (**AC-8**, **AC-9**, **AC-12**).
- Golden: a consumer whose slots bind to an external `postgresql` and through an external `postgresql-provisioner` joins `egress` with no `network.egress` in its entry; the same entry bound only to an instance in the stack joins `internal` only (**AC-12**). The external e2e consumer (`pg-roaming`) drops its `network.egress` and still signs in to the external server (**AC-1**).
- `docker inspect` in the e2e test shows no secret value in any service env (**AC-8**).
- A template naming `.Uses.nope` or `.Bogus`: `invalid_template` at load (**AC-9**).
- `setup.sh` from a rendered zip run twice with answers on stdin: the second run asks nothing and leaves every file's content and mtime unchanged (**AC-10**).
- A graph with `deployer-docker` (host mounts): plan warning `host_access`; managed render 422 `host_access`. A graph with `authelia` (setup steps): managed render 422 `setup_required` (**AC-11**).
- A step reading an output of a later step: `setup_order` (**AC-10**).
- Catalog fixtures refused with `invalid_setup_step`, one each: `{client}` in the `env`, `command`, `entrypoint` or `writes` of a step with no `for_each`; a `for_each` step writing the bare folder `client-digests/`, `digest-{client}`, `client-digests/{client}.pem` or `a/b/{client}`; a `<folder>/{client}` write whose folder is not a `from: setup` entry in `secrets`; `asks` on a `for_each` step; a `{provide}` read in a step with no `for_each`, of another provide, or of a `to_consumer` field; an `env` key `MP_INSTANCE` (**AC-2**).
- `setup.sh` with a `for_each` step over a provide with two clients: two runs, each with `{client}` replaced in its env, command and file name, while `${VAR}` in the command stays as written; bind a third client and run again: one run, for the new client only; with no binding: the folder exists, empty, and the step never runs; unbind a client: its file stays (**AC-10**).
- `sdkgen` over the planner, api and deployer entries gives the same `config.gen.go` as before the move except for paths in the header (**AC-20**).

## Rationale (short)

Field sources on the provider and delivery on the consumer split the two halves of a binding cleanly: the provider knows where a value comes from, the consumer knows what its image reads, and the capability file is the contract between them, so either side can be swapped (your choices: declared sources, env plus files). Templates (your choice) are limited to plan time facts so the planner, which never sees secrets or install answers, can render them deterministically; anything else stays an env reference the image fills itself. Secure hardening defaults with visible opt outs make every exception reviewable in the catalog, and the internal plus egress split keeps today's managed network shape for every stack (your choices). Host access as declared fields keeps "the platform is a graph" true while the deployed path refuses it (your choice). A `for_each` step gets its client only where the module places `{client}` (your choice, 2026-11-05), not under a fixed env name, so an image you do not own reads it under the name it already uses, and one token covers env, arguments and file names; per client files (`<folder>/{client}`) make the skip exact per client. Runners up for that: a fixed `MP_CLIENT` (what the paper Authelia entry used), or a `for_each: {provide, env}` object. Runners up overall: verbatim files filled by env (no template language, but Caddy routes per graph would need env tricks); one default network (simpler, every module reaches the internet); fragments as idempotent commands (less to declare, easier to get wrong).
