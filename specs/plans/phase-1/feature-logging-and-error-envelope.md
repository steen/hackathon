# Feature: Access-log middleware and user-safe error envelope

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (security hardening; cross-cutting hygiene supporting all phase 1 endpoints)

## Acceptance criteria
- Access-log middleware logs method, path, status, latency, IP, and user ID (if known).
- Sensitive query parameters (`token`, `ticket`) are stripped/redacted from logged URLs.
- All error responses use a consistent user-safe envelope: `{ "error": { "code": string, "message": string } }`.
- Internal error details (stack, raw DB error) are not exposed to clients but are logged on the server side with a request ID.

## Implementation steps
1. Create `apps/server/internal/http/middleware_logging.go` that wraps handlers, captures status + latency, and writes a single structured log line.
2. In the logging middleware, parse the URL, drop `token` and `ticket` query keys, and log the redacted form.
3. Create `apps/server/internal/http/errors.go` with `WriteError(w, code, message, status)` and a `RequestID(ctx)` helper.
4. Replace ad-hoc `http.Error` calls across handlers with `WriteError`.
5. Add panic recovery middleware that logs the stack and returns a generic envelope.

## Test plan
- `test_access_log_strips_token_query_param` — covers SEC logging hygiene.
- `test_access_log_strips_ticket_query_param` — covers SEC logging hygiene.
- `test_error_envelope_shape_is_consistent` — covers cross-cutting error format.
- `test_panic_recovery_returns_generic_envelope_and_logs_internally` — covers cross-cutting hygiene.

## Files expected to be touched or created
- `apps/server/internal/http/middleware_logging.go`
- `apps/server/internal/http/middleware_logging_test.go`
- `apps/server/internal/http/middleware_recover.go`
- `apps/server/internal/http/errors.go`
- `apps/server/internal/http/errors_test.go`

## Risks
- Forgetting to use `WriteError` in a handler leaks raw error text; mitigated by replacing all uses in one pass and adding a lint/test that scans for `http.Error(`.
