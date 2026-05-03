---
feature: rate-limits
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: Rate limits (per-IP login/register, per-username login backoff)

**Spec:** `specs/plans/phase-1/feature-rate-limits.md`
**Implementation status:** implemented — `apps/server/internal/ratelimit/iplimit.go` + `userlimit.go` define the limiters; `LoginIPConfig()`, `RegisterIPConfig()`, `LoginUserConfig()` define the thresholds. They are wired in `apps/server/main.go:115-134` via `httpapi.IPRateLimit` middleware around `/api/auth/login` and `/api/auth/register`, plus `userLimiter` passed into `auth_handlers.go`.
**E2E test directory:** `tests/e2e/phase-1/rate-limits/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | Login and register endpoints enforce a per-IP rate limit (token bucket with burst + steady-state thresholds documented in code). | missing | — |
| AC-2 | Login enforces a per-username backoff: repeated failures for the same username progressively delay subsequent attempts and/or return 429 after a threshold. | missing | — |
| AC-3 | Limits are observable in `auth_events` (rejected attempts logged). | missing | — |
| AC-4 | Limits return HTTP 429 with the user-safe error envelope (see `feature-logging-and-error-envelope.md`). | missing | — |

## Findings

### Missing E2E tests

**AC-1 — per-IP login/register rate limit (token bucket)**
- **What to assert:** Boot the server. Verify the documented thresholds first by reading `ratelimit/iplimit.go` (`LoginIPConfig`, `RegisterIPConfig`) once during test authoring and pin the exact burst+rate. PRD §11 SEC-5 says "11th login attempt within 5 min from one source IP returns 429," so the canonical assertion is: send 10 valid-shape `POST /api/auth/login {"username":"alice","password":"x"}` requests (each will be 401 — that's fine, the IP limiter counts attempts not successes); the 11th request from the same source IP within the window returns HTTP 429. Repeat for `/api/auth/register` with the documented register burst (likely a different cap; pin from `RegisterIPConfig`). Also: an 11th attempt from a *different* source — only feasible by binding the test client to a different loopback IP (`127.0.0.2`) — should NOT be 429. (If the test runner OS doesn't allow `127.0.0.2`, skip that arm with `t.Skip`.)
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/rate-limits/ip_rate_limit_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Note: the IP-limiter middleware reads `clientIP(r)`. Behind a single loopback connection that's `127.0.0.1`. To exercise the per-IP differentiation arm the test must dial via a custom `http.Transport` with `Dialer.LocalAddr` set to `127.0.0.2:0`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `httpClientFromIP(t, srcIP) *http.Client` and `loginRaw(t, client, srv, u, p)`.

**AC-2 — per-username login backoff grows with failures**
- **What to assert:** Register `alice` with password `correct`. From source IP 127.0.0.1, attempt `POST /api/auth/login {"username":"alice","password":"wrong"}` repeatedly. Observe the response status sequence — at some failure count K (verify K from `LoginUserConfig` once during test authoring), responses transition from 401 to 429 (or the response latency starts climbing in a documented backoff curve). Assert: (a) at least one 429 occurs within 20 attempts; (b) for the AC-relevant phrasing "progressively delay," measure latency of attempts 1..K and assert at least one attempt's latency is >=50ms longer than attempt 1's (i.e. some delay was injected — this can be a soft check). Then a *successful* login `POST .../login {"username":"alice","password":"correct"}` should succeed once the backoff window passes (the test waits up to the documented reset window — pin from `LoginUserConfig`); after success, subsequent failed attempts start counting from zero again (covers AC-2 "reset on success").
- **Layer:** Go (boot binary, HTTP, timing).
- **File path:** `tests/e2e/phase-1/rate-limits/user_backoff_test.go`.
- **Setup it needs:** same as AC-1. Mark with `if testing.Short() { t.Skip() }` if the reset window is large.
- **Helpers it can reuse:** harness; `register`, `loginRaw`.

**AC-3 — rate-limit rejections recorded in `auth_events`**
- **What to assert:** Register `alice`. Trip the per-IP login limiter (burst+1 attempts in the window). Open SQLite read-only and `SELECT kind, ip FROM auth_events ORDER BY id DESC LIMIT 20`. Assert at least one row has a kind that names the rate-limit rejection (e.g. `login_rate_limited`, `login_throttled`, or whatever `auth_handlers.go` writes — verify by reading the handler/audit-sink code once during test authoring; the `ah.AuditSink()` reference at `main.go:131` is the place to start). Same for register: trip the per-IP register limiter, assert a corresponding rejection row appears with `kind` matching the register-rejection name. Same for per-username backoff: trip the user-backoff and assert a rejection row exists with `user_id` populated.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/rate-limits/auth_events_rate_limit_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`, `loginRaw`, `registerRaw`, `openDBReadOnly`, `selectAuthEvents`.

**AC-4 — 429 responses use the standard error envelope**
- **What to assert:** Trip the per-IP login limiter (11th attempt or whatever the cap is). Assert response status is 429 AND `Content-Type: application/json...` AND body decodes to `{"ok":false, "data":null, "error":{"code":"<some code>","message":"<non-empty>"}}` (verify the exact `code` constant by reading `errors.go` and the rate-limit middleware once during test authoring — likely `too_many_requests` or similar; assert it's non-empty and stable across runs). Assert the four `SecurityHeaders` are also present (proves the outer middleware ordering still applies to 429 responses). Same shape assertion for the register-IP-limit 429 and the user-backoff 429.
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/rate-limits/envelope_on_429_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `requireEnvelope(t, body, wantOK=false)`, `requireSecHeaders`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. AC-1's "different source IP" arm needs a custom `http.Transport` with `Dialer.LocalAddr` — encode that in `httpClientFromIP(t, srcIP)` so other tests can reuse.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/rate-limits/harness_test.go` with copied helpers + `register`, `loginRaw(t, client, srv, u, p) (status, body, latency)`, `registerRaw`, `httpClientFromIP(t, srcIP)`, `openDBReadOnly`, `selectAuthEvents`, `requireEnvelope`, `requireSecHeaders`.
- Add `ip_rate_limit_test.go` (AC-1), `user_backoff_test.go` (AC-2, gated on `!testing.Short()` if needed), `auth_events_rate_limit_test.go` (AC-3), `envelope_on_429_test.go` (AC-4).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
