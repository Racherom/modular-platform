# 0008. Managed TLS and the certificate issuer

**Date**: 2026-11-03
**Parent**: [0006. Managed mode control plane](index.md)

## Summary

The platform's own SDK services (`api`, `planner`, `deployer-docker`) run in the SDK's TLS mode from spec 0005: each gets a 24 hour certificate for its host name from an internal ACME issuer (a small certificate authority that hands out certificates automatically) and serves only TLS on 8443. The issuer is its own module, `cert-issuer`, on the official step-ca image, reachable only on the `internal` network, so the CA key does not sit next to the Docker socket and the issuer can be swapped like any other module. Modules in deployed graphs stay plain in thread 1.

## Design

**Module**: `cert-issuer`, image `smallstep/step-ca:<pinned version>`, provides `acme`, uses nothing. Network `internal` only, alias `cert-issuer.modular-platform.internal`, listening on 9000. Healthcheck `step ca health`. Its keys and config are the files in `secrets/cert-issuer/` and `config/cert-issuer/`; its ACME account and order database lives on the named volume `cert_issuer_db`. A lost volume only drops ACME accounts, which the SDK's client creates again.

**When**: milestone 4 of the build plan. Until then the platform's SDK services run plain on `internal` with binding tokens and `MP-Caller`, so the first thread does not wait on the issuer.

**CA files** (created by `setup.sh` inside the step-ca image, principle 9 shape, never overwritten):
- `step ca init --pki` (keys and certificates only) with a generated password file: a root and an intermediate, name `Modular Platform internal CA`. Root certificate `secrets/cert-issuer/root_ca.crt` (public, but kept with the CA files), keys and password 0600.
- `config/cert-issuer/ca.json`, hand written in `deploy/managed/` and copied by `setup.sh`: one ACME provisioner named `acme` with only `tls-alpn-01`; `authority.policy.x509.allow.dns: ["*.modular-platform.internal"]`; `authority.claims.defaultTLSCertDuration`, `minTLSCertDuration` and `maxTLSCertDuration` at `24h`; `--acme-strict-fqdn` off.
- tls-alpn-01 validation on port 8443 (spec 0005 serves TLS there, not 443). Build plan task 15 checks whether the step-ca binary supports a custom ACME TLS port; if it does not, `cert-issuer` becomes a small image built in `services/cert-issuer/` from step-ca's Go packages with `acme.InsecurePortTLSALPN01 = 8443`, same files, same contract.

**SDK services** (`api`, `planner`, `deployer-docker`), set in `deploy/managed/compose.yaml`:
- `MP_DOMAIN=modular-platform.internal`, `MP_ACME_URL=https://cert-issuer.modular-platform.internal:9000/acme/acme/directory`, `MP_CA_FILE` pointing at the mounted root certificate.
- A network alias `<instance>.modular-platform.internal` on every network the service joins (`api` on `edge` and `internal`).
- Binding URLs `https://planner.modular-platform.internal:8443` and `https://deployer-docker.modular-platform.internal:8443` in `api`'s env file.
- Readiness waits for a certificate and drops when less than 2 hours remain (spec 0005, AC-9); renewal at two thirds of the lifetime.

**Ingress to api**: Caddy proxies `<base>/api/*` to `https://api.modular-platform.internal:8443`, trusting only the mounted internal root and checking that server name. The exact Caddy transport directives are checked at build against the pinned Caddy version.

**Caller key pair** (spec 0005, needed from milestone 1, so it does not wait for the CA): `setup.sh` runs `step crypto keypair` with an Ed25519 key in the step-ca image; the private key goes to `secrets/api/caller_signing_key` (`MP_CALLER_SIGNING_KEY_FILE`, `api` only), the public key to `secrets/platform/caller_keys.pem`, mounted on `planner` and `deployer-docker` as `MP_CALLER_KEY_FILE`. No rotation in thread 1.

**Out of thread 1**: deployed graphs render without `MP_ACME_URL` (plain mode, binding tokens), exactly like their static zip. `api` to `postgres` stays plain on `internal` until #3 gives unchanged providers certificates. Both are named gaps in [index.md](index.md).

## Rationale (short)

- A separate module over embedding step-ca in the deployer (spec 0005's first idea): the CA key stays away from the one container that is root on the host; the deployer would otherwise need a way to certify itself outside ACME (an SDK change); and principle 9 wants every platform piece to be a swappable default. Cost: one more container.
- Platform only over serving deployed graphs too: the issuer would have to join each deployment's network, which puts a platform service on networks of untrusted graphs. A later feature can give each deployment its own issuer.
- `--pki` plus a hand written `ca.json` over patching a generated config: the shipped file is reviewable, and no JSON tool is needed on the host.
- Known risk: tls-alpn-01 on 8443 departs from RFC 8737's port 443; the fallback image keeps the design if the binary cannot do it.
