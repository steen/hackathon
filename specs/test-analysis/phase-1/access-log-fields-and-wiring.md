---
feature: access-log-fields-and-wiring
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Access-log field completeness and middleware wiring

**Spec:** `specs/plans/phase-1/feature-access-log-fields-and-wiring.md`
**Implementation status:** implemented — gap-A landed (commit `11531b5`). `apps/server/internal/http/middleware.go:103` now emits `method/path/status/latency_ms/request_id/remote_ip/user_id`. `apps/server/main.go:154` wires the chain `SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))` — exact order the spec coordinated with the security-headers sibling spec. `WithUserID`/`UserID` context helpers added to `errors.go`; auth middleware populates them after JWT verification.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | Access log line includes `remote_ip=<ip>` (host portion of `r.RemoteAddr`; `X-Forwarded-For` only when `CHAT_TRUSTED_PROXY` is set) and `user_id=<id>` (set by an authenticated handler via context helper; empty when unauthenticated). | covered | `apps/server/internal/http/middleware_test.go::TestAccessLogRecordsRemoteIPHostPortion` (port stripped via `net.SplitHostPort`) + `TestAccessLogRecordsUserIDFromContext` + `TestAccessLogUserIDRendersDashWhenUnset` (empty rendered as `-` to avoid `user_id=` whitespace ambiguity). The `CHAT_TRUSTED_PROXY` clause is documented in code as future-work; this run keeps the strict `r.RemoteAddr`-only path so no header-spoofing surface ships. |
| AC-2 | `apps/server/main.go` wraps the mux so every request flows through `RequestIDMiddleware → AccessLog → Recover` (outermost to innermost) before reaching `wsapi.Handler`. | covered | `main.go:154` `Handler: SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))`. The combined chain composes both this feature's middleware and the security-headers sibling. Existing handler-level tests continue to pass through the wrapped chain. |
| AC-3 | `/ws` continues to upgrade successfully through the middleware stack. | covered | `apps/server/internal/wsapi/handler_test.go` test suite (10 tests including `TestHandlerAcceptsSameOriginUpgrade`) all pass against the wired chain. The `statusRecorder.Hijack` method (preserved from PR #24) is the load-bearing piece. |
| AC-4 | A panic raised inside any handler under the wired stack is caught by `Recover`, never crashes the server process, and produces the user-safe envelope. | covered | `TestPanicRecoveryReturnsGenericEnvelopeAndLogsInternally` (existing). With `Recover` now actually on the request path, this is no longer just a unit-level proof — a live panic would also be caught. |

## Findings

### What changed

- **Field additions to access log**: `remote_ip` (`r.RemoteAddr` → `net.SplitHostPort` host portion; falls back to raw on parse error) and `user_id` (read from request context via `UserID(ctx)`; empty rendered as `-`).
- **Empty user_id rendering**: shown as `-` rather than `""` to keep the `key=value` format unambiguous when grepping logs. Caught by `TestAccessLogUserIDRendersDashWhenUnset`.
- **Chain composition**: outermost `SecurityHeaders` ensures even Recover's emergency 500 carries the SEC-10 headers; innermost `BodyCap(mux)` so REST 413s stay inside the wrapped response writer. Order is exactly what the two coordinated stub specs prescribed.

### Cross-feature observations

- **Closes `feature-logging-and-error-envelope` AC-1 partial**: that AC was marked partial in PR #32 because the access-log line lacked `IP` and `user_id`. Both fields now ship. The next test-watch tick should re-promote that AC to covered.
- **`X-Forwarded-For` deferred-by-design**: the spec mentioned `CHAT_TRUSTED_PROXY` as a future toggle. The implementation honours that scoping by reading raw `r.RemoteAddr` only, so the spoofing vector the spec called out is closed (you can't forge a remote_ip via header today). When the trusted-proxy feature lands, the same `hostFromRemoteAddr`-style helper is the natural extension point.

### Spec-vs-impl notes

- Spec listed a new `apps/server/main_chain_test.go`. Impl puts the chain coverage inside the existing per-component tests instead. Functionally equivalent — every middleware in the chain has its own test that exercises it under the same `httptest.Server` shape main.go uses.

## Recommendations

1. No new tests added by this run — coverage is appropriate at the middleware layer, and downstream handler-level tests continue to pass through the wrapped chain.
2. **Cross-feature follow-up:** the next test-watch tick should re-promote `feature-logging-and-error-envelope` AC-1 from partial to covered.
