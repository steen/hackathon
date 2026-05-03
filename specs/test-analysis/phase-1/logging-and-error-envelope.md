---
feature: logging-and-error-envelope
phase: phase-1
analyzed_at: 2026-05-03T20:38:00Z
analyzed_commit: a283ba3df1d16750dfe0ccbd8e4f370dd6519c68
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Access-log middleware and user-safe error envelope

**Spec:** `specs/plans/phase-1/feature-logging-and-error-envelope.md`
**Implementation status:** implemented — `apps/server/internal/http/{middleware,errors}.go` ship the envelope, request-ID context, access log, and panic recovery. AC-1's missing fields were closed by `feature-access-log-fields-and-wiring` (gap-A in `fa60bfd`), but a follow-up audit found the user_id field was always `-` for authenticated requests because `auth.RequireJWT` and `http.AccessLog` keyed their context values on different unexported types. **Audit fix in commit `cb1e075` (PR #86)** plumbs a pointer sink from AccessLog through RequireJWT, and adds a real chain-level black-box test. AC-1 now covered for real, not just structurally.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | Access-log middleware logs method, path, status, latency, IP, and user ID (if known). | covered | `apps/server/internal/http/middleware_test.go::TestAccessLogRecordsMethodPathStatusLatencyAndRequestID` + `TestAccessLogRecordsRemoteIPHostPortion` (added by gap-A) + `TestAccessLogRecordsUserIDFromContext` + `TestAccessLogUserIDRendersDashWhenUnset` + **the new black-box chain test added by `cb1e075`** (`TestAccessLogRecordsAuthenticatedUserIDThroughRealChain` at `middleware_test.go:242` — runs an authenticated request through the real chain `SecurityHeaders → RequestIDMiddleware → AccessLog → RequireJWT` and asserts `user_id` matches the authenticated user's ULID; the same test file's `TestAccessLogUserIDRendersDashWhenUnset` covers the unauthenticated case). The Printf format now includes `remote_ip=%s user_id=%s` per `apps/server/internal/http/middleware.go:103`. |
| AC-2 | Sensitive query parameters (`token`, `ticket`) are stripped/redacted from logged URLs. | covered | `apps/server/internal/http/middleware_test.go::TestAccessLogStripsTokenQueryParam_SEC11` + `TestAccessLogStripsTicketQueryParam_SEC11` + `TestAccessLogRedactsRepeatedAndEncodedKeys` (handles repeated keys + percent-encoding edge cases). |
| AC-3 | Every JSON response uses the envelope `{ ok, data, error }` per PRD §6 with the documented null/non-null pairing. | covered | `apps/server/internal/http/errors_test.go::TestErrorEnvelopeShapeIsConsistent` (3 subtests covering ok-with-data, ok-with-nil-data, and error shapes; explicitly asserts JSON keys are physically present, not merely Go zero-values). |
| AC-4 | Internal error details are not exposed to clients but are logged on the server side with a request ID. | covered | `apps/server/internal/http/middleware_test.go::TestPanicRecoveryReturnsGenericEnvelopeAndLogsInternally` (asserts the panic value never reaches the response body; verifies the server log captures it with `request_id=`). |

## Findings

### AC-1 history — three closures

- **Phase-1 original (PR #34, `fa60bfd-`):** middleware logged method/path/status/latency/request_id. Missing `IP` and `user_id`. Marked partial.
- **gap-A closure (`fa60bfd`, PR #79):** added `remote_ip=%s user_id=%s` to the Printf format. `TestAccessLogRecordsRemoteIPHostPortion` and `TestAccessLogRecordsUserIDFromContext` covered the middleware in isolation. **The agent flipped this to covered — but the production middleware chain was still broken.**
- **Audit fix (`cb1e075`, PR #86):** the in-isolation tests passed because they called `WithUserID(req.Context(), "...")` directly on the request context they handed to `AccessLog`. The production chain `SecurityHeaders → RequestIDMiddleware → AccessLog → RequireJWT` failed because:
  - `auth.RequireJWT` wrote the user id under `auth.ctxKeyUserID` (an unexported key in the auth package).
  - `http.AccessLog` read via `http.UserID`, which keys on a different unexported type.
  - So `user_id` was always `-` for authenticated requests in the live binary.
- The fix installs a pointer sink on the outgoing request context; `AccessLog` reads it after `ServeHTTP` returns, and `RequireJWT` writes through it via the new `MiddlewareConfig.WithUserID` callback (avoiding an `auth → http` import cycle). The new black-box test `TestAccessLogRecordsAuthenticatedUserIDThroughRealChain` (`middleware_test.go:242`) drives the real chain and pins the live behavior.

The lesson: testing middleware in isolation is necessary but not sufficient. Chain-level black-box tests catch the keying-mismatch class of bug that otherwise lurks until someone greps logs.

### Coverage notes

- **In-package tests, no system-layer duplicates.** Unlike phase-0 features, this one's behavior is best exercised at the package boundary (middleware composition, log capture, JSON envelope). Adding a `tests/logging-and-error-envelope/` system-layer test would either duplicate the in-package tests or require building the server binary and parsing its log stream — high cost, low marginal coverage. The in-package tests already exercise the public functions (`AccessLog`, `Recover`, `RequestIDMiddleware`, `WriteOK`, `WriteError`, `RequestID`, `WithRequestID`) end-to-end against `httptest.NewRecorder`, which is the right level.
- **Hijacker forwarding is a real bug-class catch.** The author included a `Hijack()` method on `statusRecorder` (`middleware.go:54-62`) — without it, wrapping the WS endpoint with `AccessLog` would silently break `websocket.Accept`. There is no AC for "middleware preserves Hijacker", but the production code reflects awareness of the hazard. A future test-implement run could add a test wrapping a Hijacker-using handler and asserting the upgrade succeeds, but it's not required by any AC in this spec.
- **Spec → impl filename drift.** Spec lists `middleware_logging.go` and `middleware_recover.go` as expected files. Impl uses a single `middleware.go` containing both. Functionally identical; spec follow-up could relax the file-list to match.

## Recommendations

1. No new tests added by this run — the audit-fix author added the load-bearing chain test (`TestAccessLogRecordsAuthenticatedUserIDThroughRealChain`) which now anchors AC-1's user_id requirement at the production-chain level rather than just the in-isolation middleware level.
2. **Generalizable lesson worth a contract doc note:** when middleware reads from `context.Value`, the canonical write site (and the canonical read site) must agree on the key — and the test must drive the *production chain order* to catch keying mismatches. The pointer-sink pattern in `cb1e075` is a defensive workaround for the cross-package case; in single-package middleware chains, exporting the keying type publicly is enough.
3. **Optional hardening test (still valid from prior tick):** wrap a Hijacker-using handler with `AccessLog` + httptest, dial it via WS, assert upgrade succeeds. Catches the silent regression where someone removes the `Hijack()` method on `statusRecorder`. Not gated by any AC.
