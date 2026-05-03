# Phase 1: Persistence + auth

**Status:** planned
**Time estimate:** ~2 hrs
**PRD revision:** 7e33be3

## Goal
Real users, channels, messages persisted to SQLite.

## Dependencies
Phase 0

## Deliverables
- [ ] SQLite schema (`migrations/0001_init.sql`) including `users.token_version` and `auth_events` table.
- [ ] ULID generation.
- [ ] `internal/auth`: bcrypt + JWT with `tv` claim; constant-time login path; password length policy.
- [ ] Auth endpoints: register (invite-code gated) / login / me / logout / ws-ticket.
- [ ] Channels endpoints; messages endpoints (REST + WS).
- [ ] Hardening that must land in Phase 1 (not Phase 3):
- [ ] Startup checks: JWT secret length + dev-default denylist; non-loopback bind requires `CHAT_ALLOW_PUBLIC_BIND=1`; registration requires `CHAT_INVITE_CODE`.
- [ ] Per-IP rate limits on login and registration; per-username login backoff.
- [ ] WS read limit (64 KiB), per-conn send rate limit, 4 KiB body cap, REST 16 KiB body cap.
- [ ] Same-origin WS upgrade check; one-shot 30s ws-ticket flow; WS rejects sends to non-existent channels.
- [ ] Access-log middleware strips `token` and `ticket` query params; user-safe error envelope.
- [ ] SQLite file created `0600`.
- [ ] Response security headers (CSP, nosniff, no-referrer, frame-deny).
- [ ] Tests for US-1, US-2, US-3, US-4, US-5, US-6, US-11, US-12 and SEC-1…SEC-15.

## Validation criteria
- smoke test still green (now over authenticated WS).

## Features
- [SQLite schema](phase-1/feature-sqlite-schema.md)
- [ULID generation](phase-1/feature-ulid-generation.md)
- [Auth (bcrypt + JWT)](phase-1/feature-internal-auth.md)
- [Auth endpoints](phase-1/feature-auth-endpoints.md)
- [Channels and messages endpoints](phase-1/feature-channels-messages-endpoints.md)
- [Phase-1 hardening](phase-1/feature-phase-1-hardening.md)
- [Startup checks](phase-1/feature-startup-checks.md)
- [Rate limits](phase-1/feature-rate-limits.md)
- [Request size limits](phase-1/feature-request-size-limits.md)
- [WS upgrade and ticket validation](phase-1/feature-ws-upgrade-ticket-validation.md)
- [Access-log middleware and error envelope](phase-1/feature-access-log-error-envelope.md)
- [SQLite file permissions](phase-1/feature-sqlite-file-permissions.md)
- [Response security headers](phase-1/feature-security-headers.md)
- [Tests for US-1…US-12 and SEC-1…SEC-15](phase-1/feature-tests-us-sec.md)
