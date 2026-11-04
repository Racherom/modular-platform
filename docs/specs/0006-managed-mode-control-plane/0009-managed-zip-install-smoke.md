# 0009. Managed zip, install, dev loop and smoke job

**Date**: 2026-11-03
**Parent**: [0006. Managed mode control plane](index.md)

## Summary

The managed platform ships as a hand written folder, `deploy/managed/`, packed into `modular-platform-<version>.zip` and attached to each release. You unzip it, run `sh setup.sh` once (it asks four things and creates every secret on your host), then `docker compose up -d`. The same folder drives `task dev:managed` for contributors and the `smoke-managed` CI job, which proves the whole done line on every pull request.

## Folder (`deploy/managed/`)

| File | Holds |
|---|---|
| `compose.yaml` | project `modular-platform`; image tags written literally (release-please extra files bump them), since a plain `docker compose up -d` reads only `.env`; services `ingress`, `web`, `api`, `planner`, `deployer-docker`, `cert-issuer`, `postgres`, `db-init`, `authelia`; networks `edge` and `internal` as spec 0001 pins; volumes `pg_data`, `caddy_data`, `caddy_config`, `authelia_data`; the platform labels (`deployment` = `platform`); `restart: unless-stopped` on every long running service; only `ingress` publishes ports |
| `compose.dev.yaml` | dev overlay: local image builds, Caddy routes `<base>/` to the Vite dev server on the host (`host.docker.internal` through `host-gateway`) |
| `compose.ci.yaml` | CI overlay: the four platform images with the tag `ci` |
| `Caddyfile` | `<base>/` to `web`, `<base>/api/*` to `api` over TLS (child [0008](0008-managed-tls-cert-issuer.md)), `<base>/auth/*` to Authelia, prefix kept; `tls internal` or ACME by `PLATFORM_TLS` |
| `authelia/configuration.yml` | spec 0001's bundled Authelia settings as pinned there (`server.address: tcp://:9091/auth`, issuer `${PLATFORM_ORIGIN}/auth`, session cookie `mp_authelia_session` with domain `PLATFORM_DOMAIN`), filled from env through Authelia's template filter; one OIDC client `mp-api` with the redirect URI `${PLATFORM_ORIGIN}/api/v1/auth/callback`, `authorization_policy: one_factor`, `consent_mode: implicit`, and the `email` and `email_verified` claims available from userinfo |
| `cert-issuer/ca.json` | child 0008's step-ca config |
| `db-init/init.sh` | idempotent psql script run by the `postgres` image: creates roles `api` and `authelia` and databases `platform_api` and `authelia` only if missing, passwords from their secret files |
| `.env.example` | every setting with its default and a one line comment |
| `setup.sh` | below |
| `README.md` | install, sign in, admin reset, back up (`secrets/`, `deployments/`, the volumes), upgrade by hand over the folder, what the deployer can do on your host, the sign out note for Authelia, and that after *Delete data too* a deployment folder keeps its rendered files but no secrets; plain language (the `docs` skill) |

Start order through `depends_on`: `postgres` healthy, then `db-init` completed, then `authelia` and `api`; `cert-issuer` healthy before the SDK services; `api` also waits for `authelia` and `ingress` (spec 0001). The install command stays `docker compose up -d` (not `--wait`, whose handling of the one shot `db-init` differs between Compose versions).

## `setup.sh`

POSIX shell, `set -eu`, needs only Docker. Never overwrites an existing file and never prints a secret. When stdin is not a terminal it reads the answers one per line in the order asked, so CI can pipe them.

1. Platform questions: domain (default `platform.localhost`, must contain a dot) and admin email.
2. `.env`: `PLATFORM_DOMAIN`, `PLATFORM_ORIGIN` (`https://${PLATFORM_DOMAIN}`, plus `:${PLATFORM_HTTPS_PORT}` when not 443; spec 0001), `PLATFORM_ADMIN_EMAIL`, `PLATFORM_TLS=internal`, `PLATFORM_HTTP_PORT=80`, `PLATFORM_HTTPS_PORT=443`, `PLATFORM_BASE_PATH=` (empty; site root only in thread 1, #24 opens it), `PLATFORM_DIR` (absolute path of this folder), `PLATFORM_UID`, `PLATFORM_GID` (`id -u`, `id -g`), `PLATFORM_SESSION_IDLE=1h`, `PLATFORM_SESSION_MAX=12h`, `PLATFORM_SIGNED_OUT=index`.
3. Platform secrets, plain random (32 bytes as hex): `secrets/api/uses_planner`, `secrets/api/uses_deploy-target` (each mounted on `api` and in the provider's clients directory), `secrets/api/oidc_client_secret`.
4. `cert-issuer` fragment (step-ca image): the caller key pair from milestone 1; the CA and its password from milestone 4 (child 0008).
5. `postgres` fragment: superuser password and the `api` and `authelia` role passwords.
6. `authelia` fragment (Authelia image, `--user $(id -u):$(id -g)`): asks the admin user name and password (no echo, passed on stdin); writes the session secret, storage encryption key, OIDC HMAC secret, OIDC signing key, the pbkdf2 digest of `secrets/api/oidc_client_secret`, and `users.yml` with the admin's argon2id hash and email.
7. `deployments/` with mode 0700.
8. Prints the next step (`docker compose up -d`) and the URL.

## Zip and release

- `task managed:zip` writes `dist/modular-platform-<version>.zip` holding `modular-platform-<version>/` with the folder above minus `compose.dev.yaml`, entries sorted with the fixed 1980 time, so the same version gives the same bytes.
- `release.yml`: after the images are pushed, build the zip and attach it to the GitHub release.

## Dev loop (`task dev:managed`)

Copies `deploy/managed/` into `.dev/managed/` (gitignored) once, runs `setup.sh` there with dev answers (admin `admin`, a printed random password, `admin@platform.localhost`), then starts it with `compose.dev.yaml` and the Vite dev server. `task dev` keeps running the online graph (0001 AC-2). Binds 80 and 443.

## CI job `smoke-managed`

On every pull request, beside `integration` in `.github/workflows/ci.yml`:
1. `task images` builds `web`, `planner`, `api`, `deployer-docker` with the tag `ci`.
2. `task smoke:managed`: copy the folder to a temp dir, pipe answers into `setup.sh` (domain `platform.localhost`, email `admin@example.com`, user `admin`, a random password), `docker compose -f compose.yaml -f compose.ci.yaml up -d`, wait until every service is healthy and `db-init` exited 0 (5 minutes).
3. Playwright (`web/smoke/managed.spec.ts`, Chromium, which resolves `*.localhost` to loopback): open `https://platform.localhost/`, sign in through Authelia, open the sample, *Deploy*, confirm the review, wait until every module badge reads `running` (5 minutes), *Remove* with *Delete data too*, expect `removed`. The browser context accepts the internal certificate for this test only; documentation never tells you to skip certificate checks.
4. Fail if any value from a file under `secrets/` appears in `docker compose logs` of the platform or of the deployed project.
5. Always: `docker compose logs` as an artifact, then `down --volumes`.

## Rationale (short)

- Hand written folder over rendering the platform graph: spec 0001 and 0004 allow it until #3; the `v0alpha1` format cannot express the socket mount, fragments or TLS wiring yet.
- Answers on stdin for CI over env vars: secrets never go through env (principle 9), and the interactive path stays the only code path.
- `.dev/managed/` over running in `deploy/managed/`: no secrets ever land in the tracked folder.
- The smoke job on every pull request (not only on main): GA tier, and this is the one test of the whole thread; it costs minutes per run.
