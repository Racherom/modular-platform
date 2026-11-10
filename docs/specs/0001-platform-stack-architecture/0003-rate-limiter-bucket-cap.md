# 0003. Rate limiter bucket cap and eviction

**Date**: 2026-10-30
**Parent**: [0001. Platform stack and architecture](index.md)

## Summary

The public renderer remembers one small record (a "bucket", its token count) for every client address it has seen in the last 10 minutes, and today nothing limits how many it keeps. This spec caps that memory at 10,000 clients. When the cap is reached and a new client arrives, the renderer forgets the client it saw longest ago and serves the newcomer normally, so nobody is ever turned away because memory is full. A forgotten client simply starts again with a fresh allowance, and the renderer logs one warning per minute while this is happening.

## Context

The public renderer limits each client with a token bucket (spec 0001, "Renderer modes" row): `MP_RENDERER_RATE` tokens per minute, burst 10, keyed by the full IPv4 address or the IPv6 /64 network, buckets dropped after 10 idle minutes by a sweeper that runs every minute. The map that holds the buckets (`libs/platform/ratelimit.go`) grows by one entry for every new key and has no upper bound. The review of the scaffold ([2026-10-30-main](../../reviews/2026-10-30-main.md), "Rate limiter memory is bounded only by time") flagged it as the one unbounded structure in a service that promises to be safe on the internet.

The forces: a sender with many IPv6 /64 networks (a single /48 holds 65,536 of them) can create keys as fast as it can send requests, so memory grows with the attacker's request rate, not with real use. That same sender already gets a fresh burst of 10 on every new address, so the per client limiter never stopped it, and no cap policy can change that. What a cap policy can change is whether that sender also gets a lever to lock real visitors out. Real use of the online editor is far below 10,000 distinct networks in 10 minutes (the editor sends one check per file open and one render per download, spec 0002). One bucket costs about 250 bytes (the map entry, the key string, the `rate.Limiter`, the timestamp, the list link), so 10,000 buckets are about 2.5 MB. The renderer container has no memory limit set in `deploy/online/compose.yaml`. The access log records no client address, and this must stay true.

Left undecided, a flood of new addresses grows the renderer's heap until the host runs out of memory, which takes down the online editor for everyone.

## Requirements

**User stories**:
- As the operator of the online editor, I want the renderer's rate limit memory to stay bounded under any traffic, so that a flood of new addresses cannot exhaust the host's memory.
- As a visitor of the online editor, I want to be served even while someone floods the renderer from many addresses, so that the limit on memory never turns into a lockout for me.
- As the operator, I want to see in the logs when the cap is being hit, so that I know the renderer is under a flood.

**Acceptance criteria**:
- **AC-1**: The limiter never holds more than 10,000 buckets. After requests from more than 10,000 distinct keys, `Len()` is exactly 10,000.
- **AC-2**: When the map is full, a request from a key the limiter does not hold is allowed: it gets a fresh bucket with the full burst of 10. The cap never causes a 429.
- **AC-3**: The bucket evicted to make room is the least recently seen one. With keys `k1` to `k10000` added in that order and `k1` then used again, adding `k10001` evicts `k2` and keeps `k1`.
- **AC-4**: A client whose bucket was evicted is treated as new when it returns: it gets a fresh bucket with the full burst of 10.
- **AC-5**: The sweep still drops every bucket idle longer than 10 minutes and keeps every bucket seen more recently. It walks from the least recently seen bucket and stops at the first one that is not idle.
- **AC-6**: In each sweep interval (one minute) in which the cap forced at least one eviction, the renderer logs exactly one `WARN` line with the message `rate limiter full` and the attributes `evicted` (evictions since the previous sweep), `buckets` (current count) and `max` (the cap). In an interval without evictions it logs nothing. No client address or key appears in any log line.
- **AC-7**: Everything else in the limiter is unchanged: burst 10, refill of `MP_RENDERER_RATE` per minute, 429 with `Retry-After` when a bucket is empty, `/healthz` and `/readyz` exempt, internal mode untouched. The existing tests pass, and `go test -race` passes with concurrent `Allow` and `Sweep` calls.

## Options considered

1. **Evict the least recently seen bucket (chosen).** Pros: no one is ever refused because of the cap; memory is bounded; in normal use nothing is ever evicted early. Cons: under a flood, real clients are forgotten sooner and get a fresh burst on their next request, so per client limits get looser exactly when someone is attacking.
2. **Shared overflow bucket.** New keys share one bucket until space frees. Pros: fair to newcomers in a genuine surge; the total load from newcomers stays capped. Cons: in a flood the attacker drains the shared bucket, so every new real visitor gets 429 for up to 10 minutes after the flood began.
3. **Refuse new clients when full.** Answer 429 with `Retry-After` until the sweeper frees space. Pros: simplest code; strict. Cons: anyone with enough /64 networks locks out every new visitor for 10 minutes, at a cost of about 17 requests per second.
4. **Coarser keys when full.** Under pressure, key IPv6 by /48 and IPv4 by /24. Pros: the strongest answer to IPv6 floods; fewer buckets. Cons: a second keying mode to build, test and reason about; neighbours sharing a network get throttled together; it still needs one of the options above once the coarser map fills.

For finding the oldest bucket, a list from the Go standard library (`container/list`) ordered by last seen was chosen over `hashicorp/golang-lru/v2/expirable` (a new dependency with its own cleanup goroutine; only `Add` renews an entry's expiry and a lookup does not, so an active client's bucket would still expire 10 minutes after it was created) and over evicting a random map entry (simplest, but it can reset an active client in the middle of being throttled, and the sweep would still scan the whole map).

## Decision

**Chosen option**: Option 1: evict the least recently seen bucket, with a cap of 10,000 held in a `container/list` ordered by last seen.

The public renderer's rate limiter holds at most 10,000 buckets; a new client arriving at a full map takes the place of the bucket seen longest ago, and the sweeper logs one warning per minute while evictions happen. (basis: OWASP API4:2023, bound every structure an unauthenticated caller can grow; fail open on limiter state; the review finding on `libs/platform/ratelimit.go`)

**Implementation skills**: `golang-concurrency` · `golang-security` · `golang-observability` · `golang-testing` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`)

## Rationale

The cap exists to protect memory, and the only sender who can fill it is one with many addresses. That sender already escapes the per client limit by changing address, so the cap policy cannot take anything away from it. The policy only decides who else pays. Refusing newcomers (option 3) or starving them in a shared bucket (option 2) turns a memory guard into a cheap lockout of every real visitor, which is worse than the problem it fixes. Eviction makes the attacker pay with its own churn: what it costs a real client is a fresh allowance on the next request, which does that client no harm. Coarser keys (option 4) is the only option that actually limits an address rich sender, but it is a second keying mode plus a fallback policy, for a threat spec 0001 already assigns to protection in front of your network.

The cap of 10,000 was your pick over the recommended 100,000. It is still far above real use, so in normal operation nothing changes. Under a flood a real client is forgotten sooner (after about 10,000 new addresses instead of 100,000), which with eviction only means a fresh bucket. Memory stays near 2.5 MB. It is a constant beside `burst` and `idleBucket`, so changing it later is a one line edit.

The ordered list makes every step constant time: a request moves its bucket to the front, an eviction takes the back. It also lets the sweep stop at the first bucket that is not idle instead of scanning the whole map while holding the lock. It is about 30 lines of standard library code, in line with spec 0001's preference for the standard library over new dependencies. (basis: spec 0001, "HTTP" row; `container/list` docs)

## Design

**Data model** (in memory only, nothing persisted):

| Field | Type | Meaning |
|---|---|---|
| `RateLimiter.maxKeys` | `int` | The cap. Must be at least 1. |
| `RateLimiter.buckets` | `map[string]*list.Element` | Key to its list element. Replaces `map[string]*bucket`. |
| `RateLimiter.order` | `*list.List` | All buckets, front = most recently seen, back = least recently seen. |
| `RateLimiter.evicted` | `int` | Evictions forced by the cap since the last `Sweep`. Guarded by `mu`. |
| `bucket.key` | `string` | Its own key, so an eviction from the back can delete the map entry. |
| `bucket.limiter`, `bucket.seen` | unchanged | Each `list.Element.Value` is a `*bucket`. |

**Interface changes in `libs/platform`**:

| Function | Change |
|---|---|
| `NewRateLimiter(perMinute, burst int, idle time.Duration, maxKeys int) *RateLimiter` | New `maxKeys` parameter. If `maxKeys < 1` it calls `panic("platform: NewRateLimiter maxKeys must be at least 1")` (it is always a constant, so a bad value is a programming error caught at start). |
| `Allow(key string) bool` | Takes the lock, then reads the clock. Known key: `MoveToFront`. New key: if `order.Len() >= maxKeys`, remove `order.Back()` from the list and the map and increment `evicted`; then `PushFront` a fresh bucket and add it to the map. Either way, set the bucket's `seen` to `now`, then return `AllowN(now, 1)` as today. |
| `Sweep() (evicted, buckets int)` | In one locked section: walks from `order.Back()` and removes buckets with `seen.Before(cutoff)` (strict, as today: a bucket idle exactly 10 minutes survives this sweep), stopping at the first that is not. Returns `evicted` (evictions forced by the cap since the previous `Sweep`, never idle removals, which are neither counted nor logged) and `buckets` (the count after removal), then resets `evicted` to 0. Every call resets it; in production only `SweepEvery` calls `Sweep`. |
| `SweepEvery(ctx context.Context, every time.Duration, log *slog.Logger)` | New `log` parameter, required (no fallback to `slog.Default()`). Creates the ticker and calls the unexported `sweepTicks(ctx, ticker.C, log)`. |
| `sweepTicks(ctx context.Context, ticks <-chan time.Time, log *slog.Logger)` (new, unexported) | On each tick: `Sweep`, and if `evicted > 0`, log one `WARN` `rate limiter full` with `evicted`, `buckets`, `max` (`maxKeys`). Returns when `ctx` ends, with no final flush: evictions after the last tick are not logged. An "interval" in AC-6 is one tick; ticks the runtime drops simply fold into the next one. This is the test seam: tests pass their own channel. |
| `Len`, `RetryAfter`, `Middleware` | Unchanged. |

**Wiring in `services/renderer/internal/api/server.go`**: a new constant `maxClients = 10_000` beside `burst` and `idleBucket`; `NewRateLimiter(cfg.Rate, burst, idleBucket, maxClients)`; `go limiter.SweepEvery(ctx, time.Minute, log)`.

**Value sourcing**:

| Action | Value | Source |
|---|---|---|
| `Allow`, new key | the cap | `maxClients` constant in `server.go`, passed to `NewRateLimiter` |
| `Allow`, new key | which bucket to evict | `order.Back()` |
| `Allow`, new key | the newcomer's allowance | `rate.NewLimiter(perMinute/60, burst)`: `MP_RENDERER_RATE` and the `burst` constant, as today |
| `Allow` | `now` for `seen` and `AllowN` | `l.now()`, read after taking `mu` |
| `Sweep` | the idle cutoff | `l.now()` minus `idle` (the `idleBucket` constant, 10 minutes), as today |
| warning log | `evicted` | first return value of `Sweep` |
| warning log | `buckets` | second return value of `Sweep` (same locked section) |
| warning log | `max` | `maxKeys` |
| warning log | the logger | the `log *slog.Logger` parameter of `NewHandler(ctx, cfg, catalog, log)`, passed to `SweepEvery` |
| warning log | when an interval ends | each value received on the ticker channel (`ticker.C` in production, a test channel in tests) |

**Key invariants**:
- `len(buckets) == order.Len() <= maxKeys` at all times, both changed together under `mu`.
- List order is last seen order. This holds only because `Allow` reads the clock after taking the lock; reading it before (as today) lets two concurrent calls write `seen` out of order, which could stop the sweep early. It also needs a clock that never goes backward: `time.Now` is monotonic, and a fake clock in tests must only move forward. `Sweep` may keep reading the clock before taking the lock, as today: a slightly early cutoff only makes the sweep keep a bucket one tick longer.
- Every `Allow` renews its key (`MoveToFront` and `seen = now`), whether it returns true or false. A client that keeps sending while refused stays held and is not the next one evicted.
- The cap never refuses a request. Only an empty bucket does.
- No client address or rate key is logged anywhere, including in the warning.

**Security model**: unchanged. Public mode only, no auth, health paths exempt. Eviction gives an address rich sender nothing it did not already have by switching address.

**Configuration required**: none. The cap is a constant.

**Critical test scenarios** (in `libs/platform/platform_test.go`, with the fake clock the existing test uses and a small `maxKeys` such as 3 where the number does not matter):
- Fill past the cap: `Len` equals the cap, verifies **AC-1**.
- Full map, new key: `Allow` returns true ten times for it, verifies **AC-2**.
- Touch the oldest key, then add one: the second oldest is gone and the touched one remains, verifies **AC-3**.
- Exhaust a key's burst, evict it, return with it: ten requests allowed again, verifies **AC-4**.
- Mix of idle and recent buckets, advance the clock, `Sweep`: idle ones gone, recent ones kept, verifies **AC-5**.
- `Sweep` after evictions and idle removals: `evicted` counts only the cap evictions, `buckets` equals `Len`, and a second `Sweep` returns `evicted` 0, verifies **AC-6**.
- `sweepTicks` fed by a test channel, logging through `slog.NewJSONHandler` into a `bytes.Buffer`: after evictions, one tick gives exactly one `WARN` `rate limiter full` with `evicted`, `buckets`, `max`; a following quiet tick adds nothing; none of the test's key strings appear anywhere in the buffer, verifies **AC-6**.
- `NewRateLimiter` with `maxKeys` 0 panics with the message above, verifies **AC-1**.
- A bucket idle exactly 10 minutes survives a sweep; one idle 10 minutes plus a nanosecond is dropped, verifies **AC-5**.
- The existing `TestRateLimiterBurstAndSweep` (now passing `maxKeys` 10) and the server tests still pass; a server test (`TestPublicClientCap`) pins `maxClients` at exactly 10,000 through public mode with `cfg.Rate` 1 (one token a minute, so nothing refills during the test). Every request renews its client, a refused one too, so probing a client to show it is still held moves it to the front and it is no longer the one the next newcomer evicts. The test therefore uses two spent clients: A and then B each spend their burst and get a 429; 9,998 other distinct `X-Forwarded-For` addresses each send one request (10,000 held, A oldest, then B); A still gets a 429 (still held, so the cap is not 9,999; this probe renews A and leaves B oldest); one more new address sends a request; B is now allowed (it was evicted and starts fresh, so the cap is not 10,001); A still gets a 429 (the probe renewed it, so it was not evicted). No test address appears in the log. A parallel `Allow`/`Sweep` test runs under `-race`, verifies **AC-1**, **AC-7**.

## Build plan

One small slice through the code that already runs (Tracer Bullet: this thickens the public renderer strand).

1. Reshape `libs/platform/ratelimit.go`: `container/list` order, `maxKeys` with the panic guard, eviction in `Allow` with the clock read under the lock, `Sweep` from the back returning `(evicted, buckets)`. Update the `NewRateLimiter` call in `TestRateLimiterBurstAndSweep` to pass `maxKeys` 10. Satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**, **AC-5**.
2. Add the `log` parameter to `SweepEvery`, the unexported `sweepTicks` seam, and the once per tick warning. Satisfies **AC-6**.
3. Add `maxClients = 10_000` in `services/renderer/internal/api/server.go` and pass it and `log` through. Satisfies **AC-1**, **AC-6**, **AC-7**.
4. Extend `libs/platform/platform_test.go` and `services/renderer/internal/api/server_test.go` with the scenarios above and run `task test` (with `-race`). Satisfies **AC-1** to **AC-7**.

## Consequences

**Positive**:
- The renderer's rate limit memory is bounded at about 2.5 MB whatever arrives.
- A flood of new addresses can no longer lock real visitors out or take the host down through this map.
- The sweep touches only idle buckets, so it holds the lock briefly even when the map is full.
- You can see a flood in the logs without any address being recorded.

**Negative / tradeoffs**:
- During a flood, per client limits get looser: a real client forgotten by eviction gets a fresh burst of 10, and so does a single abusive client that happens to be evicted. CPU protection against an address rich sender still depends on protection in front of your network (spec 0001, Consequences).
- `NewRateLimiter` and `SweepEvery` change signature; every caller changes with them (today `services/renderer/internal/api/server.go` and `TestRateLimiterBurstAndSweep` in `libs/platform/platform_test.go`).
- A wrong `MP_PROXY_HOPS` that makes every request look like a new address would keep the map full and evicting, which quietly turns the per client limit into almost nothing. The `rate limiter full` warning firing every minute under normal traffic is the sign to check that setting.
- The cap is a constant, so changing it needs a new release.

**Neutral**:
- No migration: all state is in memory and a restart starts empty, as today. Reverting is one commit.
- Spec 0001's "Renderer modes" row now names the cap and points here.

## Follow-up

- [ ] Protection against CPU exhaustion (a cap on requests in flight, or a global limit) was kept out of this decision. Revisit through `/architect` if the renderer is seen saturating its CPU; it would need a 503 in `contracts/openapi/renderer.yaml`.
- [ ] When the optional OpenTelemetry capability arrives (spec 0001, Observability row), export evictions as a counter metric beside the warning.

## References

**Project sources**:
- Spec [0001](index.md): the "Renderer modes" row (token bucket, burst 10, keys, 10 minute idle), the standard library preference, and the Consequences line on flood protection in front of your network.
- Spec [0002](0002-provisional-graph-format.md): one check per open and per download, so real clients stay far below the limit.
- The scaffold review [2026-10-30-main](../../reviews/2026-10-30-main.md): "Rate limiter memory is bounded only by time".
- `libs/platform/ratelimit.go`, `libs/platform/clientip.go`, `services/renderer/internal/api/server.go`: the code this changes.
- Installed skills `golang-concurrency`, `golang-security`, `golang-observability`, `golang-testing`.

**Practices & standards**:
- Bounded caches with least recently used eviction.
- Fail open on limiter state: losing a client's record must never refuse that client.
- OWASP API Security Top 10 2023, API4 Unrestricted Resource Consumption (every server side structure an unauthenticated caller can grow needs a bound).

**Links** (web verified 2026-10-30):
- `golang.org/x/time/rate` (`Limiter.AllowN`): https://pkg.go.dev/golang.org/x/time/rate
- `container/list` (`PushFront`, `MoveToFront`, `Back`, `Remove`): https://pkg.go.dev/container/list
- `hashicorp/golang-lru/v2/expirable` (runner up; `Add` renews expiry, `Get` does not; `NewLRU` starts a cleanup goroutine): https://pkg.go.dev/github.com/hashicorp/golang-lru/v2/expirable
- OWASP API4:2023 Unrestricted Resource Consumption: https://api-security.owasp.org/editions/2023/en/0xa4-unrestricted-resource-consumption
