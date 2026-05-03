# Feature: Auth endpoints (register, login, me, logout, ws-ticket)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- US-1 — As a friend, I want to register an account, so I can join the chat.
- US-2 — As a friend, I want to log in with my username and password, so I can resume conversations.
- US-11 — As the host, I want registration gated by an invite code, so a publicly reachable instance is not joinable by strangers.
- US-12 — As a user, I want logout to actually invalidate my token server-side, so a stolen token stops working when I notice.

## Acceptance criteria
- `POST /api/register` requires a valid invite code (`CHAT_INVITE_CODE`) and creates a user with a hashed password (US-1, US-11).
- `POST /api/login` returns a JWT including a `tv` claim on success; constant-time on failure (US-2).
- `GET /api/me` returns the current user when given a valid bearer token; 401 otherwise.
- `POST /api/logout` increments the user's `token_version`, invalidating all outstanding tokens (US-12).
- `POST /api/ws-ticket` issues a one-shot, 30-second ticket bound to the user, redeemable once at WS upgrade (see `feature-ws-hardening.md`).
- All auth endpoints write entries to `auth_events`.
- `scripts/smoke.sh` continues to exit 0 after this feature lands.

## Implementation steps
1. Wire HTTP router (e.g., chi or stdlib `http.ServeMux`) and a JSON helper.
2. Implement handlers in `apps/server/internal/http/auth_handlers.go`:
   - `register`: validates invite code, applies password policy, hashes, inserts user, logs `auth_events`.
   - `login`: calls `auth.AuthenticateLogin`, returns token, logs success/failure.
   - `me`: reads bearer token, parses JWT, verifies `tv` against DB, returns user.
   - `logout`: parses token, increments `users.token_version` for the user, logs event.
   - `ws-ticket`: generates random 128-bit token, stores `(user_id, token, expires_at)` in an in-memory ticket store, returns it.
3. Add an auth middleware that extracts and verifies bearer tokens and `tv` from the DB.
4. Update `scripts/smoke.sh` to perform a `chatd login` (or fetch a ws-ticket via the test invite code) before sending, so the Phase-0 smoke acceptance survives the auth landing.

## Test plan
- `test_register_creates_user_with_invite_code` — covers US-1, US-11.
- `test_register_rejects_missing_or_wrong_invite_code` — covers US-11.
- `test_login_returns_token_for_valid_credentials` — covers US-2.
- `test_login_rejects_wrong_password` — covers US-2.
- `test_login_rejects_unknown_user_in_constant_time` — covers SEC-related goals (timing).
- `test_me_returns_current_user_for_valid_token` — covers US-2.
- `test_me_rejects_token_after_logout` — covers US-12.
- `test_logout_increments_token_version` — covers US-12.
- `test_ws_ticket_is_single_use_and_30s_ttl` — covers WS handshake spec.
- `test_auth_events_records_register_login_logout_kinds` — covers SEC-13. Drives a register → login → logout flow and asserts `auth_events` has one row per kind for that user.

## Files expected to be touched or created
- `apps/server/internal/http/auth_handlers.go`
- `apps/server/internal/http/auth_handlers_test.go`
- `apps/server/internal/http/middleware_auth.go`
- `apps/server/internal/auth/tickets.go`
- `apps/server/internal/auth/tickets_test.go`

## Risks
- Ticket store is in-memory; a server restart invalidates all outstanding tickets. Acceptable given the 30s TTL.
