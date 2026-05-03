# Feature: Startup config checks (JWT secret, bind, invite)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (security hardening that must land in Phase 1; supports the safety implied by US-11 invite gating and overall hosting posture)

## Acceptance criteria
- Server refuses to start if the JWT signing secret is shorter than the documented minimum length.
- Server refuses to start if the JWT signing secret matches a dev-default denylist (e.g., empty string, `dev`, `secret`, `change-me`).
- If the configured bind address is non-loopback, the server refuses to start unless `CHAT_ALLOW_PUBLIC_BIND=1` is set.
- Server refuses to start if `CHAT_INVITE_CODE` is unset (since registration depends on it; see US-11).
- All failure modes print a clear, actionable error to stderr and exit non-zero.

## Implementation steps
1. Create `apps/server/internal/config/config.go` that loads env vars into a `Config` struct.
2. Add `Validate()` on `Config` that returns the first violation found, with a human-readable message.
3. Maintain a small denylist of dev-default JWT secrets in the config package.
4. Detect non-loopback bind by parsing the host portion of the address and matching against `127.0.0.0/8` and `::1`.
5. Call `Config.Validate()` from `main.go` before any DB or HTTP setup.

## Test plan
- `test_config_rejects_short_jwt_secret` — covers SEC startup checks.
- `test_config_rejects_dev_default_jwt_secret` — covers SEC startup checks.
- `test_config_rejects_public_bind_without_override` — covers SEC startup checks.
- `test_config_allows_public_bind_when_override_set` — covers SEC startup checks.
- `test_config_rejects_missing_invite_code` — covers US-11 enforcement at startup.

## Files expected to be touched or created
- `apps/server/internal/config/config.go`
- `apps/server/internal/config/config_test.go`
- `apps/server/main.go` (call `Validate` on startup)

## Risks
- Overly strict denylist could surprise developers; mitigated by clear error messages naming the offending value.
