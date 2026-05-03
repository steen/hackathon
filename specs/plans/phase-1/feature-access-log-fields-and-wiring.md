# Feature: Access-log field completeness and middleware wiring

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Why this exists

Follow-up to [feature-logging-and-error-envelope](./feature-logging-and-error-envelope.md) (PR #24). That feature is marked done, but auditing the merged code against its acceptance criteria surfaces two gaps:

- The plan AC says the access log records "method, path, status, latency, IP, and user ID (if known)." The implementation in `apps/server/internal/http/middleware.go:83` logs `method, path, status, latency, request_id` only — IP and user ID are missing.
- The middleware (`AccessLog`, `Recover`, `RequestIDMiddleware`) is defined but never applied. `apps/server/main.go:34-35` mounts `wsapi.Handler` directly on the mux. None of the new middleware sits in the request path, so SEC-11 hygiene and panic recovery are not exercised in the running server.

This plan closes both gaps without changing the envelope/redaction behaviour already shipped.

## Requirements covered

- PRD §6 — every JSON response uses the standard envelope (already satisfied by `WriteOK` / `WriteError`; this plan ensures the wrapping middleware is on the request path).
- PRD §9 SEC-11 — "Access logs of a login + WS upgrade contain no `token` or `ticket` value." Redaction logic exists; wiring is required for it to take effect at runtime.
- PRD §9 — access logs source IP. The threat model (login rate limit keyed on IP, abuse triage) assumes IP is captured.
- Parent feature AC line 10 — IP and user ID present in the access log.

## Acceptance criteria

- The access log line emitted by `AccessLog` includes:
  - `remote_ip=<ip>` — taken from `r.RemoteAddr` (host portion only). When `CHAT_TRUSTED_PROXY=1` (PRD §9), the leftmost `X-Forwarded-For` entry is preferred.
  - `user_id=<id>` — set by an authenticated handler via a context helper; empty string when the request is unauthenticated.
- `apps/server/main.go` wraps the mux so that every request flows through `RequestIDMiddleware → AccessLog → Recover` (outermost to innermost) before reaching `wsapi.Handler`.
- `/ws` continues to upgrade successfully through the middleware stack (the existing `statusRecorder.Hijack` path is exercised by an integration test).
- A panic raised inside any handler under the wired stack is caught by `Recover`, never crashes the server process, and produces the user-safe envelope already implemented.

## Implementation steps

1. Extend the access log line in `apps/server/internal/http/middleware.go` to include `remote_ip` and `user_id`. Add a `hostFromRemoteAddr(r)` helper that strips the port; honour `X-Forwarded-For` only when a future config flag (`CHAT_TRUSTED_PROXY`) is set — for now read via a small accessor so the auth-internals feature can wire the flag without touching this file again.
2. Add `WithUserID(ctx, id)` / `UserID(ctx)` helpers in `apps/server/internal/http/errors.go` (mirror of the existing `WithRequestID` / `RequestID` pair). Authenticated handlers (Phase 1 auth) will populate this; unauthenticated paths leave it empty.
3. In `apps/server/main.go`, build the handler chain explicitly: `handler := RequestIDMiddleware(AccessLog(Recover(mux)))`. Pass that to `http.Server.Handler`.
4. Add an integration test under `apps/server/internal/http/` that drives a request through the wired chain and asserts:
   - `X-Request-Id` header is set on the response.
   - The access log line contains `remote_ip=` and `user_id=` keys.
   - A panicking sub-handler returns the standard error envelope and the server stays up.
5. Add an integration test that opens a WebSocket against the wired stack and confirms the upgrade still succeeds (regression guard for the `Hijack` path).

## Test plan

- `test_access_log_includes_remote_ip` — drives a request with a known `RemoteAddr` and asserts the log line contains the host portion.
- `test_access_log_includes_user_id_when_set` — wraps a handler that calls `WithUserID(r.Context(), "u_123")` and asserts the log line shows `user_id=u_123`.
- `test_access_log_user_id_empty_when_unauthenticated` — covers the "(if known)" qualifier.
- `test_server_chain_request_id_header_present` — asserts `X-Request-Id` on a response routed through the full chain.
- `test_server_chain_panic_does_not_crash` — registers a panicking handler, fires a request, asserts envelope and that subsequent requests still succeed.
- `test_server_chain_ws_upgrade_through_middleware` — opens `/ws` against the wired mux and confirms the handshake completes.

## Files expected to be touched or created

- `apps/server/internal/http/middleware.go` (extend log line, add `hostFromRemoteAddr`)
- `apps/server/internal/http/middleware_test.go` (new field tests)
- `apps/server/internal/http/errors.go` (`WithUserID` / `UserID` helpers)
- `apps/server/internal/http/errors_test.go` (round-trip test for the new helpers)
- `apps/server/main.go` (wire the chain)
- `apps/server/main_chain_test.go` *(new)* — end-to-end chain test including the WS upgrade.

## Risks

- Logging `RemoteAddr` raw (with port) leaks the ephemeral client port into logs without operational value; mitigated by stripping with `net.SplitHostPort` and falling back to the raw value on parse error.
- Trusting `X-Forwarded-For` unconditionally is a known IP-spoofing vector; this plan only reads it behind the `CHAT_TRUSTED_PROXY` flag described in PRD §9 to preserve the documented behaviour.
- Wiring `Recover` around `wsapi.Handler` must not break the WebSocket hijack path; the integration test in step 5 is the regression guard.
