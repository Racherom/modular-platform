# Review, feat/bundled-identity-provider (second pass), 2026-11-21

**Reviewed by**: Sonnet 5.5 (author on another model)
**Scope**: 232 files (about 155 without generated output and goldens), branch vs origin/dev, 50 commits
**Verdict**: Approve with nits

## Summary
This change makes Pocket ID the bundled identity provider. The first admin page sits behind a Caddy `forward_auth` gate, and a setup session hands off to it through a one time token and a gate cookie. A new `oidc-provisioner` capability registers the `api` client, and the first sign in binds the admin only when it carries a setup session. I found no blocker and no major problem. All five findings of the first pass are fixed, and the fixes are correct. The new findings are small: a truncated error message, one corner of the revoke script, a stale page text, and thin probing of the gate in the smoke. `go test -race` passes for `libs`, `sdk`, `catalog`, `services/api` and `services/renderer`, and `pnpm vitest run` passes (33 files, 375 tests).

## Earlier findings
The first pass file lists five minors (the summary of it said four). Status of each:

1. **Setup code burned before the key is read**: fixed in `a6598c8`. `Redeem` now reads the key before `keeper.CodeUsed` (`services/api/internal/setup/setup.go:245`). The test `missing key keeps the code` redeems with no key, checks that no code was recorded as used, then redeems again after the key is back.
2. **`Done` removes files before it writes the marker**: fixed in `a6598c8`. `FileKeeper.Done` writes `.setup-done` first and removes `session-key`, `code-used` and `code` last (`filekeeper.go:86`). `TestFileKeeperDoneCutShort` fails each of the three removals in turn, checks that the marker is there and the code file is still there (so `Run` retries at the next start), then checks that a second `Done` finishes. `Run`'s start resume is covered by the test near `setup_test.go:422`.
3. **Plaintext secrets in the job's `/tmp`**: fixed in `e233d3d`. Both scripts set `trap '...' EXIT` over every temp file they make, and the `fail` path exits through it. I also confirmed that `jobService` gives every job `tmpfs: [/tmp]` (`libs/render/bind.go`), so the files never reach disk. The goldens carry the trap line.
4. **Spec 0012 still names `named_not_allowed`**: fixed in `88a7a89`. Both places in spec 0012 now say `invalid_binding`. No other file names the old code.
5. **Spec 0036 says `Sec-Fetch-Site`, code checks `Origin`**: fixed in `88a7a89` and `c97f719`. Specs 0036, 0033 (index and rationale) and 0034 AC-18 now describe the `Origin` check, including the missing `Origin` case. The code and the spec agree.

The five nits of the first pass (`EqualFold` on the host, the nonce table comment, the `https://` check before `location.assign`, the Dockerfile comment about the build context, `Problem` against `ProblemList`) were not claimed as fixed and are still open. They stay nits.

## Minor
### 🟡 The "not made by the platform" message leaves out the way out, `catalog/capabilities/oidc-provisioner.yaml:52`
**Problem**: Spec 0037 AC-12 says the failure reads: `client <id> exists and was not made by the platform; delete it in Pocket ID or name another client id in the graph`. The script prints only `client $MP_CLIENT_ID exists and was not made by the platform`. The e2e test (`oidcclient_e2e_test.go:230`) only looks for the first half, so the gap passes.
**Why it matters**: This is the one message an operator sees when a hand made client collides with the graph. Without the second half they must find the fix themselves.
**Suggested fix**: Print the full sentence from the spec, and make the e2e test look for `name another client id`.

### 🟡 The revoke job can delete from a partial list when a page body is not JSON, `catalog/capabilities/oidc-provisioner.yaml:~78`
**Problem**: The loop only checks the HTTP status (`= 200`). The `jq ... >> /tmp/drop` that reads the page and the `jq` that reads `totalPages` are not checked. If a page answers 200 with a body that is not the expected JSON, the first `jq` fails quietly, the `totalPages` test (`[ 1 -lt "" ]`) fails, and `|| break` leaves the loop. The next loop then deletes every id already collected from earlier pages.
**Why it matters**: AC-12 says the revoke job deletes nothing when any page of the list failed. This is a corner (a 200 with a wrong body, for example from a proxy page), but it is the exact promise the spec makes, and the clients it would delete do carry the marker, so the harm is small.
**Suggested fix**: Check the exit status of both `jq` calls with `|| fail "reading the client list failed"`, and treat an empty `totalPages` as a failure.

### 🟡 The page text for a missing setup code describes the keeper, not this build, `web/src/components/Setup.tsx:~85`
**Problem**: When the code file is missing, the page says to remove `.setup-done` and restart the keeper. In this build there is no keeper yet, and `.setup-done` does not exist while the setup is pending. The README (`platform-README.md.tmpl`, *Lost the setup code*) says the right steps: delete the files in `secrets/api/setup/`, then run `sh setup.sh`.
**Why it matters**: An operator who lost the code follows the page and gets stuck.
**Suggested fix**: Make the text follow the README for now, and switch it when the keeper of #32 lands. The vitest case for `no_code` should assert the new words.

### 🟡 The smoke proves the gate for three exact paths only, `scripts/smoke-managed.sh:~139`
**Problem**: The check sends `GET /setup`, `/signup/setup` and `/api/signup/setup` with no cookie. AC-18 says the gate guards by prefix so that variants such as `/setup/` and `/setup/__data.json` are closed, and Caddy matches paths in its own way (decoding, case, repeated slashes). No variant is sent, and the 502 to 403 rule of `handle_errors` is proven only by the golden text, not by a running Caddy.
**Why it matters**: The gate is the one thing that keeps a stranger from claiming the admin account of the provider. The golden shows the intent, but only a request shows the behaviour on the pinned Caddy and Pocket ID versions.
**Suggested fix**: Add a short loop of variants to the same `curl` check (`/setup/`, `/setup/x`, `//setup`, `/%73etup`, `/SETUP`, `/api/signup/setup/`) that expects 403 with no cookie. A case that Caddy does not match may answer 404 from Pocket ID; the check can accept 403 or 404 there, but never 200 or a page.

## Nits
- ⚪ `services/api/internal/setup/identity.go:140`, `r.Host != host` is case sensitive and compares with the host and port of the issuer, while Caddy's `header_up Host {host}` sends the host without the port. At the published ports 80 and 443 this is fine; a public origin with another port would refuse every handoff. A comment, or `strings.EqualFold` on the host part only, would state the limit.
- ⚪ `services/api/internal/setup/identity.go:~55`, the used nonce table is in memory, so a restart of `api` inside the 60 second window forgets nonces. A one line comment would help (carried over).
- ⚪ `web/src/components/Setup.tsx:239`, `window.location.assign(url)` trusts the link from the API; a check for `https://` costs one line (carried over).
- ⚪ `catalog/images/oidc-job/Dockerfile:3`, the comment says the context is the folder alone, but `release.yml` builds with `context: .` (carried over).
- ⚪ `services/api/internal/api/setuphttp.go`, handlers answer one `Problem` while the OpenAPI responses name `ProblemList` (carried over).
- ⚪ `web/src/components/SessionGate.tsx:~25`, the `reasons` map has no `setup_pending`, which the callback now returns in `?signed_out=`. The setup gate also drops the query when it moves to `/setup`, so the banner would not show anyway. Add the text and keep the query, or leave it.
- ⚪ `services/api/internal/store/queries.sql` (`EnsureStack`), the change to `ON CONFLICT DO NOTHING` with no target and the retry in `store.go` fix a real race but have nothing to do with the identity provider. A separate `fix:` commit would keep the release notes clean. The no target form also hides a clash on `compose_project`; the retry then ends in an error, which is acceptable.

## Strengths
- The three MAC contexts (`mp-setup`, `mp-gate`, `mp-handoff`) keep every value to its own use, the handoff MAC length prefixes the host, and `Verify` checks the MAC before it parses any number. A test sends a gate cookie as a setup session and gets `no_session`.
- Every gate path fails closed: an error anywhere answers 403, Caddy turns 502, 503 and 504 of the gated paths into 403, and the checks refuse a gate with no guard, two guards, or a route with no host of its own. `cleanURLPath` bans the characters a Caddy matcher would read as a pattern, and the render turns each gate path into a prefix.
- The first admin bind is one atomic statement, with a comment that explains why `DO UPDATE` beats `DO NOTHING` plus a `SELECT`, and with no setup session nothing binds (`admit` fails closed). `Origin` is compared with the configured public origin, and a missing `Origin` is refused.
- The provision script builds all JSON with `jq`, passes the API key as a header file, never prints a response body, keeps its files in a job `tmpfs`, and only touches a client that carries the marker. The Pocket ID entry keeps both keys and the database password out of the Compose file.
- The earlier findings were fixed with a test for each Go change, and the spec text was updated in the same series.

## Test coverage
Good. New and changed logic has tests beside it: `libs/setupsession` (including context separation), `services/api/internal/setup` (redeem refusals, the missing key case, handoff, gate, `Done` cut short at each removal, the start resume), `setuphttp_test.go` and `identityhttp_test.go` (Origin, errors, headers), `signin/setup_test.go`, `sdk/setupfile_test.go`, the `libs/check` and `libs/contract` cases, the render goldens for the gate Caddyfile and the managed flavor, the store integration test for the bind race, and vitest for the setup page and the base path. I ran the Go suites for `libs`, `sdk`, `catalog`, `services/api` and `services/renderer` under `-race` (all pass) and the web suite (all pass). I did not run the Docker integration or e2e tests or the smoke. Gaps: no test checks that the job scripts really remove their temp files (the trap is asserted only as golden text), the revoke script's bad body path has no test, and the gate has no request test against a running Caddy beyond the three exact paths.
