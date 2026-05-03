# Feature: Auth internals (bcrypt + JWT + password policy)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (internal building blocks; user-facing endpoints in `feature-auth-endpoints.md` cover US-1, US-2, US-11, US-12)

## Acceptance criteria
- `internal/auth` exposes password hashing/verification using bcrypt with a sane cost.
- JWT issuance and verification include a `tv` (token-version) claim used to invalidate tokens server-side (US-12).
- A constant-time login path: when a user does not exist, the code still performs a bcrypt comparison against a dummy hash so timing does not leak username existence.
- A password length policy is enforced (e.g., min 8, max 72 to stay within bcrypt's input limit).
- Token signing key is loaded from config (validated by startup checks; see `feature-startup-config-checks.md`).

## Implementation steps
1. Create `apps/server/internal/auth/password.go` exposing `Hash(pw)`, `Verify(hash, pw)`, and `EnforcePolicy(pw)` returning typed errors.
2. Create `apps/server/internal/auth/jwt.go` exposing `Issue(userID, tokenVersion)` and `Parse(tokenStr)` returning claims including `sub` and `tv`.
3. Implement `auth.AuthenticateLogin(username, password)` which:
   - Loads user; if not found, runs `bcrypt.CompareHashAndPassword` against a precomputed dummy hash and returns the same error.
   - On match, checks token version and issues a fresh JWT.
4. Add a constants file with the dummy hash and policy thresholds.

## Test plan
- Unit test: `Hash` then `Verify` round-trips; verification fails with wrong password.
- Unit test: `EnforcePolicy` rejects too-short and too-long passwords.
- Unit test: `Issue` then `Parse` round-trips; tampered token rejected; expired token rejected.
- Unit test: login timing for unknown user is within a small delta of timing for a known user with wrong password (sanity check, not a strict timing assertion).
- `test_login_error_message_byte_identical_for_unknown_vs_wrong_password` — covers SEC-4. PRD §9 mandates byte-identical error text.

## Files expected to be touched or created
- `apps/server/internal/auth/password.go`
- `apps/server/internal/auth/password_test.go`
- `apps/server/internal/auth/jwt.go`
- `apps/server/internal/auth/jwt_test.go`
- `apps/server/internal/auth/login.go`
- `apps/server/internal/auth/login_test.go`

## Risks
- Constant-time comparison for unknown users is timing-sensitive; absolute equivalence is impossible, so the test asserts "within a tolerance" rather than exact parity.
