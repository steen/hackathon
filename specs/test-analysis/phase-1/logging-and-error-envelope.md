---
feature: logging-and-error-envelope
phase: phase-1
analyzed_at: 2026-05-03T14:27:13Z
analyzed_commit: 8013612749226341ad515582320d35baa00ae5d4
implementation_status: partial
total_acs: 4
covered: 3
partial: 1
missing: 0
deferred: 0
---

# Test analysis: Access-log middleware and user-safe error envelope

**Spec:** `specs/plans/phase-1/feature-logging-and-error-envelope.md`
**Implementation status:** partial — `apps/server/internal/http/{middleware,errors}.go` ship the envelope, request-ID context, access log, and panic recovery. The access log line, however, omits two fields the spec requires (see AC-1 below). All in-package tests pass at this SHA.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | Access-log middleware logs method, path, status, latency, IP, and user ID (if known). | partial | `apps/server/internal/http/middleware_test.go::TestAccessLogRecordsMethodPathStatusLatencyAndRequestID` (covers method, path, status, latency, request_id; **does not** cover IP — and the impl's log line `"access method=%s path=%s status=%d latency_ms=%d request_id=%s"` does not emit IP either). |
| AC-2 | Sensitive query parameters (`token`, `ticket`) are stripped/redacted from logged URLs. | covered | `apps/server/internal/http/middleware_test.go::TestAccessLogStripsTokenQueryParam_SEC11` + `TestAccessLogStripsTicketQueryParam_SEC11` + `TestAccessLogRedactsRepeatedAndEncodedKeys` (handles repeated keys + percent-encoding edge cases). |
| AC-3 | Every JSON response uses the envelope `{ ok, data, error }` per PRD §6 with the documented null/non-null pairing. | covered | `apps/server/internal/http/errors_test.go::TestErrorEnvelopeShapeIsConsistent` (3 subtests covering ok-with-data, ok-with-nil-data, and error shapes; explicitly asserts JSON keys are physically present, not merely Go zero-values). |
| AC-4 | Internal error details are not exposed to clients but are logged on the server side with a request ID. | covered | `apps/server/internal/http/middleware_test.go::TestPanicRecoveryReturnsGenericEnvelopeAndLogsInternally` (asserts the panic value never reaches the response body; verifies the server log captures it with `request_id=`). |

## Findings

### Partial — AC-1 is missing IP

The middleware's `log.Printf` call (`apps/server/internal/http/middleware.go:83`) emits `method`, `path`, `status`, `latency_ms`, `request_id` — five of the seven fields the AC names. Missing:

- **IP** — no `remote_addr` / `ip=` field. `r.RemoteAddr` is always observable; the gap is purely in the format string. This matters: an access log without source IP can't support per-IP rate-limit tracing or abuse forensics.
- **user_id (if known)** — also absent. The spec hedges with "if known", and there's no auth feature shipped yet at this SHA, so user_id is never known today. This becomes a real gap once `feature-auth-endpoints` lands and the request context can carry a user ID.

**Recommended follow-up (production code change, out of test-agent scope):** extend the `Printf` format to include `remote_addr=%s` (from `r.RemoteAddr`, optionally split by `:` to drop the port), and plumb `user_id=%s` once auth lands. Then add a one-line assertion to `TestAccessLogRecordsMethodPathStatusLatencyAndRequestID` checking the new field.

The test-agent did NOT add a failing test to drive this. The gap is in production code and naming-an-IP-field is a straightforward owner change; a failing test in `tests/` would just shout at the next implementer without giving them the structured spec context they already have here.

### Coverage notes

- **In-package tests, no system-layer duplicates.** Unlike phase-0 features, this one's behavior is best exercised at the package boundary (middleware composition, log capture, JSON envelope). Adding a `tests/logging-and-error-envelope/` system-layer test would either duplicate the in-package tests or require building the server binary and parsing its log stream — high cost, low marginal coverage. The in-package tests already exercise the public functions (`AccessLog`, `Recover`, `RequestIDMiddleware`, `WriteOK`, `WriteError`, `RequestID`, `WithRequestID`) end-to-end against `httptest.NewRecorder`, which is the right level.
- **Hijacker forwarding is a real bug-class catch.** The author included a `Hijack()` method on `statusRecorder` (`middleware.go:54-62`) — without it, wrapping the WS endpoint with `AccessLog` would silently break `websocket.Accept`. There is no AC for "middleware preserves Hijacker", but the production code reflects awareness of the hazard. A future test-implement run could add a test wrapping a Hijacker-using handler and asserting the upgrade succeeds, but it's not required by any AC in this spec.
- **Spec → impl filename drift.** Spec lists `middleware_logging.go` and `middleware_recover.go` as expected files. Impl uses a single `middleware.go` containing both. Functionally identical; spec follow-up could relax the file-list to match.

## Recommendations

1. **Production change (not a test task):** add `remote_addr=` to the access log format. Then extend `TestAccessLogRecordsMethodPathStatusLatencyAndRequestID` with one assertion line.
2. **Wait-and-see:** revisit AC-1's `user_id` clause once `feature-auth-endpoints` lands and a user ID can be plumbed via context.
3. **Optional hardening test:** wrap a Hijacker-using handler with `AccessLog` + httptest, dial it via WS, assert upgrade succeeds. Catches the silent regression where someone removes the `Hijack()` method on `statusRecorder`. Not gated by any AC; defer until the agent revisits this feature.
