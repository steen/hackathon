# Feature: WS hardening (origin check, ws-ticket flow, channel validation)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (security hardening that gates US-5 real-time delivery)

## Acceptance criteria
- WS upgrade enforces a same-origin check; cross-origin upgrades are rejected with a 403.
- WS connections must present a valid one-shot ticket from `POST /api/ws-ticket`; tickets expire after 30 seconds and are single-use.
- After successful ticket redemption, the WS connection is associated with the authenticated user identity.
- WS sends to non-existent channels are rejected with a typed error frame and do not crash the connection.

## Implementation steps
1. In the WS upgrader, configure `CheckOrigin` to compare the `Origin` header against the configured site origin (allow loopback during dev when bind is loopback).
2. On upgrade, read the ticket from a query parameter; redeem it atomically against the in-memory ticket store from `feature-auth-endpoints.md`. Reject if missing/expired/already-redeemed.
3. Bind the redeemed `user_id` onto the connection state.
4. On every inbound chat-message frame, look up the channel; if it does not exist, send `{type:"error", code:"CHANNEL_NOT_FOUND"}` and continue (do not close).

## Test plan
- `test_ws_rejects_cross_origin_upgrade` — covers SEC same-origin.
- `test_ws_rejects_missing_or_expired_ticket` — covers SEC ticket flow.
- `test_ws_ticket_is_single_use` — covers SEC ticket flow.
- `test_ws_send_to_nonexistent_channel_returns_error_frame` — covers SEC channel validation.

## Files expected to be touched or created
- `apps/server/internal/ws/handler.go` (origin check, ticket redemption, channel validation)
- `apps/server/internal/ws/handler_test.go`

## Risks
- Same-origin check must allow legitimate dev hosts; mitigated by deriving allowed origins from configured bind address and an explicit allowlist env var.
