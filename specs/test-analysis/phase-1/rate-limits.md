---
feature: rate-limits
phase: phase-1
analyzed_at: 2026-05-03T15:55:44Z
analyzed_commit: 591befe817d623fe996c5282024ea6a6a177f613
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Rate limits (per-IP login/register, per-username login backoff)

**Spec:** `specs/plans/phase-1/feature-rate-limits.md`
**Implementation status:** implemented — `apps/server/internal/ratelimit/{iplimit,userlimit}.go` ship a token-bucket limiter (LRU-bounded) and a per-username failure tracker; `apps/server/internal/http/middleware_ratelimit.go` wraps `/api/login` and `/api/register` via `IPRateLimit` middleware; the login handler consults the user limiter before authenticating, increments on failure, resets on success. main.go wires both limiters with the configs `LoginIPConfig` (10/5min) and `RegisterIPConfig` (5/15min) and `LoginUserConfig` (2 free attempts → 500ms steps capped at 2s). 17 tests across the three test files (6 + 6 + 5), all passing.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | Login and register endpoints enforce a per-IP rate limit (e.g., token bucket with burst + steady-state thresholds documented in code). | covered | `apps/server/internal/ratelimit/iplimit_test.go::TestIPLimiterRejectsEleventhLoginAttemptWithinFiveMinutes` (SEC-5 wire test: bucket starts full at 10, 11th attempt 0..5min returns false) + `TestIPLimiterRefillsOverTime` (steady-state replenishment) + `TestIPLimiterIsolatesKeys` (per-IP isolation) + `TestIPLimiterEvictsLeastRecentlyUsed` (LRU bound at Capacity prevents memory growth) + `TestIPLimiterAllowsConcurrentAccess` (under -race) + `TestIPLimiterEmptyKeyAlwaysAllowed` (no-key bypass). Middleware wiring: `middleware_ratelimit_test.go::TestIPRateLimitBlocksEleventhLoginAttemptWithin5min` + `TestIPRateLimitBlocksRegisterAfterBurst`. |
| AC-2 | Login enforces a per-username backoff: repeated failures for the same username progressively delay subsequent attempts and/or return 429 after a threshold. | covered | `userlimit_test.go::TestUserLimiterBackoffGrowsWithFailures` (linear: failure 3 → 500ms, 4 → 1s, 5 → 1.5s) + `TestUserLimiterBackoffCapsAtMaxDelay` (caps at 2s — prevents lockout-DoS) + `TestUserLimiterResetClearsBackoff` (success path) + `TestUserLimiterExpiresAfterResetAfterWindow` (5min idle eviction) + `TestUserLimiterIsCaseInsensitive` (`Alice` and `alice` share the same backoff state — closes the case-bypass attack). End-to-end: `middleware_ratelimit_test.go::TestSuccessfulLoginResetsUsernameBackoff`. |
| AC-3 | Limits are observable in `auth_events` (rejected attempts logged). | covered | `middleware_ratelimit_test.go::TestRateLimitRejectionLoggedToAuthEvents` (drives a 429 and asserts an `auth_events` row with kind `rate_limited` is written). The middleware takes a `RateLimitAuditSink` interface — keeps the package reusable from tests (no `*sql.DB` dep) while still satisfying the AC. |
| AC-4 | Limits return HTTP 429 with the user-safe error envelope (see `feature-logging-and-error-envelope.md`). | covered | `middleware_ratelimit_test.go::TestRateLimitedResponseUsesEnvelope` (429 status + envelope shape `{ok:false, data:null, error:{code:"rate_limited", message:...}}` + `Retry-After` header in seconds per RFC 7231 §7.1.3). |

## Findings

### Coverage notes

- **AC-1 is exercised at three layers.** Bare bucket math (`iplimit_test.go`), middleware integration (`middleware_ratelimit_test.go`), and end-to-end through the router via the live login handler. The 11th-attempt test pins SEC-5 to PRD §11 wording. The LRU-eviction test is what makes the spec's risk note ("memory growth from per-IP tracking; mitigated by a bounded LRU") provably honored — without this test, the LRU could be removed/broken without any AC noticing.
- **AC-2 case-insensitivity is load-bearing.** `TestUserLimiterIsCaseInsensitive` closes a real attack surface: without case-folding, an attacker discovering "alice" exists could probe `Alice`, `ALICE`, etc. as distinct keys, each with its own fresh failure budget. The fix is a one-liner (`strings.ToLower`) but the test is what guards against a "performance optimization" that drops it.
- **AC-2 `MaxDelay` cap addresses the DoS-via-lockout flip side.** Per the spec note ("linear backoff up to ~2s … without enabling lockout-DoS"). Without a cap, an attacker submitting 1000 failures could push the next-allowed-at to hours from now — the legitimate user is locked out without an admin tool to clear it. `TestUserLimiterBackoffCapsAtMaxDelay` pins this.
- **AC-3 design — single `kind` for both layers.** The middleware writes `auth_events.kind="rate_limited"` for both per-IP and per-username rejections; the row's `user_id` (NULL for unknown-user IP rejections, set for known-user backoff rejections) distinguishes them when present. The spec didn't mandate this; the implementer's design comment explains the choice ("keeping a single kind keeps the audit query simple"). Test asserts the kind string only; the user_id-vs-NULL distinction would be a separate test if it became load-bearing.
- **AC-4 envelope conformance.** `WriteError(429, "rate_limited", ...)` reuses the same envelope helper as every other error path, so a regression in the envelope shape (already covered by `TestErrorEnvelopeShapeIsConsistent` from the logging feature) would also fail this AC's test. Belt-and-braces but appropriately so — 429 is exactly the response a flaky client retries against, and a malformed body would burn the support channel.
- **`Retry-After` header.** Not a literal AC requirement, but the spec's reference to "user-safe error envelope" leaves it open. The implementation goes further and emits a properly-rounded RFC 7231 `Retry-After: <seconds>` so a well-behaved CLI / web client can back off. `writeRateLimited` ceil-rounds (`(d + 1s - 1) / 1s`) so a 1.2s retry doesn't round down to 1 and produce a too-early retry.

### Cross-feature observations

- **Closes the constant-time concern in `feature-auth-internals` AC-3 transitively.** With per-IP and per-username limits in place, even a perfect-timing attack against `auth.AuthenticateLogin` is throttled to 10 attempts per 5 min per IP and 2 free + 4 throttled attempts per username — well below what a timing oracle would need to extract a useful signal. Doesn't downgrade the constant-time test; complements it.
- **Doesn't change `feature-access-log-fields-and-wiring` deferred status.** main.go wires `IPRateLimit` middleware on the login/register routes specifically, NOT around the whole mux. The access-log / Recover / RequestIDMiddleware chain is still un-wired. The two are orthogonal.
- **Touches `auth_handlers.go` lightly.** The login handler now consults the user limiter pre-`AuthenticateLogin` and resets/increments around the success/failure outcomes; otherwise the auth-endpoints feature's tests still pass. No spec drift.

### Spec-vs-impl notes

- Spec lists `apps/server/internal/http/middleware_ratelimit.go` as expected; impl matches.
- Impl exposes `LoginIPConfig` / `RegisterIPConfig` / `LoginUserConfig` as exported defaults so main.go and tests share one source of truth for the threshold numbers — nice factoring not in the spec.

## Recommendations

1. No new tests added by this run — coverage is comprehensive across all 4 ACs at unit + middleware + handler layers.
2. **Optional follow-up test (low-risk):** assert that an `auth_events` row written for a per-username 429 carries the `user_id` when the username is known. Today's test only asserts the `kind`. Catches the regression "we accidentally pass empty userID for the username-known path", which would lose the audit signal for triaging brute-force.
3. **Cross-feature note for whoever wires `feature-access-log-fields-and-wiring`:** the rate-limit middleware predates the global access-log wrap. Once `AccessLog` wraps the mux, every 429 will appear in the access log too — that's intentional; just confirm the redaction logic doesn't try to scrub the `Retry-After` header value (it shouldn't, since redaction is on URL query params).
