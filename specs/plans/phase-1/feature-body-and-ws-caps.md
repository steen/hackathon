# Feature: Body and WS read/send caps

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** done

## Requirements covered
- (security hardening; protects US-5 message flow and all REST endpoints from oversized payloads / floods)

## Acceptance criteria
- WebSocket reads are capped at 64 KiB per frame; oversized frames close the connection with a policy-violation code.
- Each WS connection has a per-conn send rate limit (e.g., N messages/sec with burst); excess sends are dropped or trigger close.
- WS message bodies (chat-message payloads) are capped at 4 KiB.
- REST request bodies are capped at 16 KiB; oversized bodies return 413.

## Implementation steps
1. In the WS upgrader/handler, set the `gorilla/websocket` (or chosen lib's) `SetReadLimit(64 * 1024)`.
2. Validate decoded message body length ≤ 4 KiB; on violation, send a typed error frame and close.
3. Add a per-connection token bucket on the send/incoming path; configure burst + steady-state.
4. Wrap REST handlers with `http.MaxBytesReader(w, r.Body, 16*1024)`.
5. Ensure 413 responses use the user-safe error envelope.

## Test plan
- `test_ws_rejects_frame_over_64kib` — covers SEC-6. Asserts the WebSocket close code is `1009` (per PRD §11 SEC-6).
- `test_ws_rejects_message_body_over_4kib` — covers SEC limits.
- `test_ws_send_rate_limit_drops_excess` — covers SEC limits.
- `test_rest_rejects_body_over_16kib_with_413` — covers SEC limits.

## Files expected to be touched or created
- `apps/server/internal/ws/handler.go` (read limit, body cap, send rate limit)
- `apps/server/internal/http/middleware_bodycap.go`
- `apps/server/internal/ws/handler_test.go`
- `apps/server/internal/http/middleware_bodycap_test.go`

## Risks
- Send rate limit is per-connection; coordinated multi-connection floods are addressed by IP rate limits in `feature-rate-limits.md`.
