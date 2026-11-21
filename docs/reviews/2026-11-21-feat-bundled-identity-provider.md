# Review, feat/bundled-identity-provider, 2026-11-21

**Reviewed by**: Sonnet 5.5 (author on another model)
**Scope**: 230 files (about 153 without generated output), branch vs origin/dev
**Verdict**: Approve with nits

## Summary
This change adds Pocket ID as the bundled identity provider. It adds a setup session that hands off to the provider's first admin page through a signed one time token, a gate cookie, and a Caddy `forward_auth` check. It also adds the `oidc-provisioner` capability with provision and revoke scripts, a job image, and the first admin bind that needs a valid setup session. I found no blocker and no major problem. The security design holds up: separate MAC contexts for the session and the gate, a host bound handoff token with single use, and checks that fail closed. The findings below are small hardening and drift items. `go test -race` for `libs`, `sdk`, `catalog` and `services/api` passes.

## Minor
### 🟡 The setup code is burned before the session key is read, `services/api/internal/setup/setup.go:~250`
**Problem**: `Redeem` calls `keeper.CodeUsed` and only afterwards reads the session key. If the key file is missing or unreadable, the code is already recorded as used, the call answers 500, and the same code is refused from then on.
**Why it matters**: A transient file error locks you out of the setup until you run the recovery steps (delete the code, restart the keeper).
**Suggested fix**: Read the key first, and record the code as used only when a cookie can be minted.

### 🟡 `FileKeeper.Done` removes the key and code before it writes the done marker, `services/api/internal/setup/filekeeper.go:~85`
**Problem**: If the marker write fails after the removals, `Run` does not retry on a restart, because it retries only while the code file still exists.
**Why it matters**: `.setup-done` is then never written. After a later `api admin reset`, the page tells you to remove a `.setup-done` that does not exist, which is confusing.
**Suggested fix**: Write the marker first, then remove the files, or let `Run` also retry when the marker is missing and an admin is bound.

### 🟡 The job scripts leave plaintext secrets in the job container's `/tmp`, `catalog/capabilities/oidc-provisioner.yaml` (provision and revoke)
**Problem**: `/tmp/key.hdr` (the admin API key), `/tmp/secret.json` (the client secret) and `/tmp/out` are never deleted. The job image runs `sleep infinity`, so the container is long lived.
**Why it matters**: If `/tmp` is not a tmpfs for this job, the secrets stay on disk after the run. Mode 0600 limits who can read them but does not remove them.
**Suggested fix**: Add a `trap 'rm -f /tmp/key.hdr /tmp/secret.json /tmp/out ...' EXIT` at the top of both scripts, and confirm the job runs with a tmpfs `/tmp`.

### 🟡 Spec drift after the check code rename, `libs/check/check.go:31`
**Problem**: `named_not_allowed` became `invalid_binding`, but spec 0012 (lines 126 and 149) still names the old code.
**Why it matters**: The spec is the source of truth and now describes a code the checks never return.
**Suggested fix**: Update spec 0012, or note the amendment in spec 0037's Amends line.

### 🟡 Spec says `Sec-Fetch-Site`, code checks `Origin`, `services/api/internal/api/setuphttp.go:33`
**Problem**: Spec 0036 says every POST needs `Sec-Fetch-Site: same-origin`. The handler compares `Origin` with the configured public origin.
**Why it matters**: The code is arguably stricter and matches principle 12 (origin from config), but the spec and the code disagree.
**Suggested fix**: Fix the spec text, or add the `Sec-Fetch-Site` check too.

## Nits
- ⚪ `services/api/internal/setup/identity.go:~140`, `r.Host != host` is a case sensitive compare against the configured origin host. Browsers send lower case, but a mixed case issuer would fail the handoff. Compare with `strings.EqualFold`.
- ⚪ `services/api/internal/setup/identity.go:~55`, the used nonce table is in memory, so an api restart inside the 60 second window forgets nonces. The risk is very small, but a comment would help.
- ⚪ `web/src/components/Setup.tsx:239`, `window.location.assign(url)` trusts the API's link. A check that the link starts with `https://` costs one line.
- ⚪ `catalog/images/oidc-job/Dockerfile:3`, the comment says the build context is the folder alone, but `release.yml` builds with `context: .` and passes `MP_SOURCE=local`, which this Dockerfile ignores.
- ⚪ `services/api/internal/api/setuphttp.go`, handlers answer a single `Problem` while the OpenAPI responses reference `ProblemList`. This matches the existing setup routes, but the two should be made to agree.

## Strengths
- The setup session and the gate cookie use different MAC contexts (`mp-setup`, `mp-gate`, `mp-handoff`), so one value can never pass as another. The handoff MAC length prefixes the host.
- The gate fails closed everywhere: any error answers 403, Caddy turns 502, 503 and 504 into 403, and a gate with no guard or a route without its own host is a check error.
- `cleanURLPath` bans characters a Caddy matcher would read as a pattern, and the golden Caddyfile gates the prefix (`/setup*`), so a trailing slash does not bypass it.
- The first admin bind is atomic and fails closed when no setup session exists. The `ON CONFLICT DO UPDATE` comment explains the snapshot trap well.
- The provision script builds all JSON with `jq`, keeps the API key in a header file, and only touches clients that carry the platform marker.

## Test coverage
Good. The new logic has tests beside it: `libs/setupsession`, `services/api/internal/setup` (service, identity handoff, file keeper), `setuphttp_test.go` and `identityhttp_test.go`, `signin/setup_test.go`, the render goldens including the gate Caddyfile, `libs/contract` and `libs/check` cases, the sdk setup file, the store integration test for the bind race, and vitest for the setup page and base path. The suites for libs, sdk, catalog and services/api passed under `-race` here. Not run by me: the integration and e2e build tags, the web tests and the smoke. The findings above (code burned before key read, `Done` ordering) have no test yet.
