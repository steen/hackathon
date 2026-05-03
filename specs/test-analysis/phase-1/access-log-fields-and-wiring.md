---
feature: access-log-fields-and-wiring
phase: phase-1
analyzed_at: 2026-05-03T15:51:00Z
analyzed_commit: e46dd519c73becae18b6c412c894a05be96811db
implementation_status: stub
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# Test analysis: Access-log field completeness and middleware wiring

**Spec:** `specs/plans/phase-1/feature-access-log-fields-and-wiring.md`
**Implementation status:** stub — spec landed (status: planned), no implementation. `apps/server/main.go:111` still uses `Handler: httpx.BodyCap(mux)` with no `AccessLog`, `RequestIDMiddleware`, or `Recover` wrap. `apps/server/internal/http/middleware.go` still lacks `remote_ip` / `user_id` fields in the access-log line, no `hostFromRemoteAddr` helper, no `WithUserID` / `UserID` context pair.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The access log line emitted by `AccessLog` includes `remote_ip=<ip>` (host-only from `r.RemoteAddr`; honors `X-Forwarded-For` only when `CHAT_TRUSTED_PROXY=1`) and `user_id=<id>` (set by an authenticated handler via context helper; empty when unauthenticated). | deferred | impl is stub — middleware.go's `Printf` still emits `method, path, status, latency_ms, request_id` only. No helpers added. |
| AC-2 | `apps/server/main.go` wraps the mux so every request flows through `RequestIDMiddleware → AccessLog → Recover` (outermost to innermost) before reaching `wsapi.Handler`. | deferred | impl is stub — main.go's `Handler:` is still bare `httpx.BodyCap(mux)`. |
| AC-3 | `/ws` continues to upgrade successfully through the middleware stack (the existing `statusRecorder.Hijack` path is exercised by an integration test). | deferred | no integration test exists yet because the chain isn't wired. The Hijack method on `statusRecorder` does ship in middleware.go (PR #24's pre-emptive guard) so the regression-test surface is ready as soon as wiring lands. |
| AC-4 | A panic raised inside any handler under the wired stack is caught by `Recover`, never crashes the server process, and produces the user-safe envelope already implemented. | deferred | `Recover` middleware exists and has its own unit test (`TestPanicRecoveryReturnsGenericEnvelopeAndLogsInternally` from `feature-logging-and-error-envelope`), but there is no path through the running server for a panic to reach it because `Recover` is not wrapped around the mux. |

## Findings

### Why "stub" rather than "missing"

This spec was authored as a follow-up to address gaps the test-agent flagged in PR #32 (`feature-logging-and-error-envelope` AC-1: access-log line missing IP). It carries `**Status:** planned`. Like the sibling `feature-security-headers-and-sqlite-ensure-wiring` spec (PR #47 stub), this is the "spec exists, no impl yet" state — exactly what `deferred` is for.

A future PR will close it, presumably the same one that handles the security-headers wiring since both touch `main.go`'s chain construction. When it lands the next test-watch tick will detect the wiring and re-promote all four ACs.

### Pre-existing pieces that already satisfy parts of the spec

- **`statusRecorder.Hijack` (middleware.go:54-62)** ships from PR #24 and is the regression guard for AC-3. The `Hijack` method exists; what's missing is the chain that puts it on the request path so a `/ws` upgrade actually flows through.
- **`Recover` middleware** ships from PR #24 with its own unit test exercising panic → 500 envelope + server-side logging. AC-4's deferred status is purely about main.go not wrapping the mux with it; the middleware itself is correct.
- **`RequestIDMiddleware` + `WithRequestID` / `RequestID` context pair** (middleware.go + errors.go) ship and have round-trip + unique-ID tests.

What's NOT present:
- `WithUserID` / `UserID` context pair.
- `hostFromRemoteAddr` helper (auth_handlers.go has a similar `clientIP` helper at handler-layer, but that's per-handler, not access-log-wide).
- `remote_ip` / `user_id` fields in the `Printf` format string.
- The `CHAT_TRUSTED_PROXY` env var read.
- The chain wiring in main.go.

### Cross-feature coordination

The spec body explicitly notes coordination with `feature-security-headers-and-sqlite-ensure-wiring` (the PR #47 sibling stub): they share `apps/server/main.go` ownership of the middleware chain. Per that spec's recommendation, `SecurityHeaders` should be the outermost wrap, then `RequestIDMiddleware → AccessLog → Recover → mux` (with `httpx.BodyCap` somewhere in there too). The combined chain after both features land would be roughly:

```go
handler := SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(httpx.BodyCap(mux)))))
```

Order matters: `SecurityHeaders` outermost so the 413 envelope from `BodyCap` carries the four SEC-10 headers; `RequestIDMiddleware` outside of `AccessLog` so the access log line picks up the freshly-minted request ID; `Recover` innermost (just outside `mux`) so a panic in any handler — including the auth ones — produces the standard envelope.

`feature-auth-endpoints` (PR #38) has a `clientIP` helper at the handler layer that reads `r.RemoteAddr` for `auth_events` rows. That's a parallel implementation; once the access-log middleware reads `remote_ip` the same way, the two should agree on the IP value for any given request — useful for cross-referencing log lines and DB rows during incident triage.

### Pre-emptive observation about the partial finding it closes

`feature-logging-and-error-envelope` AC-1 (PR #32) was marked partial because `IP` and `user_id` were missing from the access-log line. This spec exists to close that gap. The next test-watch tick after the implementation PR lands should re-promote `feature-logging-and-error-envelope` AC-1 from partial to covered, and this feature's row from `0/4 deferred` to `4/4 covered`.

## Recommendations

1. **No tests added by this run** — there is no implementation to anchor tests against, and writing a guaranteed-failing test for a spec the maintainer has explicitly scheduled as follow-up would put `pnpm test` in a permanently-red state. Same call as PR #37, PR #47.
2. When the implementation lands, the integration test (`apps/server/main_chain_test.go`) the spec describes is the right shape: drive a request through the wired chain, assert `X-Request-Id` on the response, assert log line contains `remote_ip=`/`user_id=`, register a panicking handler and assert envelope + server-still-up, open `/ws` through the chain and assert successful upgrade. That's the load-bearing surface.
3. **Cross-feature note for the next implementer:** coordinate with `feature-security-headers-and-sqlite-ensure-wiring` on the chain order (see above). Doing both in one PR avoids a churn step where one spec's wiring has to be unwound and re-stacked when the other lands.
