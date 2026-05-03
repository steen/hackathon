# Phase 1: Persistence + auth

**Status:** planned
**Time estimate:** ~2 hrs
**PRD revision:** 7e33be3

## Goal
Real users, channels, messages persisted to SQLite.

## Dependencies
Phase 0

## Deliverables
- [x] SQLite schema (`migrations/0001_init.sql`) including `users.token_version` and `auth_events` table.
- [x] ULID generation.
- [x] `internal/auth`: bcrypt + JWT with `tv` claim; constant-time login path; password length policy.
- [x] Auth endpoints: register (invite-code gated) / login / me / logout / ws-ticket.
- [ ] Channels endpoints; messages endpoints (REST + WS).
- [ ] Hardening that must land in Phase 1 (not Phase 3):
- [x] Startup checks: JWT secret length + dev-default denylist; non-loopback bind requires `CHAT_ALLOW_PUBLIC_BIND=1`; registration requires `CHAT_INVITE_CODE`.
- [ ] Per-IP rate limits on login and registration; per-username login backoff.
- [x] WS read limit (64 KiB), per-conn send rate limit, 4 KiB body cap, REST 16 KiB body cap.
- [x] Same-origin WS upgrade check; one-shot 30s ws-ticket flow; WS rejects sends to non-existent channels.
- [x] Access-log middleware strips `token` and `ticket` query params; user-safe error envelope.
- [x] SQLite file created `0600`.
- [x] Response security headers (CSP, nosniff, no-referrer, frame-deny).
- [ ] Tests for US-1, US-2, US-3, US-4, US-5, US-6, US-11, US-12 and SEC-1…SEC-15.

## Validation criteria
- smoke test still green; if the auth flow requires `chatd login` first, `scripts/smoke.sh` is updated as part of `feature-auth-endpoints.md`.

## Features

Test ownership: each feature's `## Test plan` carries its US-N / SEC-N coverage.

- [SQLite schema (also covers ULID generation)](phase-1/feature-sqlite-schema-and-ulid.md)
- [Auth (bcrypt + JWT)](phase-1/feature-auth-internals.md)
- [Auth endpoints](phase-1/feature-auth-endpoints.md)
- [Channels and messages endpoints](phase-1/feature-channels-and-messages.md)
- [Startup checks](phase-1/feature-startup-config-checks.md)
- [Rate limits](phase-1/feature-rate-limits.md)
- [Request size limits](phase-1/feature-body-and-ws-caps.md)
- [WS upgrade and ticket validation](phase-1/feature-ws-hardening.md)
- [Access-log middleware and error envelope](phase-1/feature-logging-and-error-envelope.md)
- [SQLite file permissions and response security headers](phase-1/feature-file-perms-and-headers.md)
