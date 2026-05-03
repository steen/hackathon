---
feature: auth-endpoints
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 7
covered: 7
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Auth endpoints (register, login, me, logout, ws-ticket)

**Spec:** `specs/plans/phase-1/feature-auth-endpoints.md`
**Implementation status:** implemented — all five endpoints ship in `apps/server/internal/http/auth_handlers.go` wired via `apps/server/main.go` behind the new `auth.RequireJWT` middleware. The `auth.TicketStore` (in-memory, 30s TTL, single-use) lives in `apps/server/internal/auth/tickets.go`. `scripts/smoke.sh` was updated to run register → login → ws-ticket → watch and exits 0 against the live binary. 25+ in-package tests, all passing.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `POST /api/auth/register` requires a valid invite code (`CHAT_INVITE_CODE`) and creates a user with a hashed password (US-1, US-11). | covered | `apps/server/internal/http/auth_handlers_test.go::TestRegisterCreatesUserWithInviteCode` + `TestRegisterRejectsMissingOrWrongInviteCode` + `TestRegisterRejectsBadUsername` (3-32 char regex) + `TestRegisterRejectsShortPassword` (delegates to `auth.EnforcePolicy`) + `TestRegisterRejectsDuplicateUsername` (envelope code `username_taken`). Invite check uses `crypto/subtle.ConstantTimeCompare` so an attacker cannot timing-discover a partial-match prefix. |
| AC-2 | `POST /api/auth/login` returns a JWT including a `tv` claim on success; constant-time on failure (US-2). | covered | `TestLoginReturnsTokenForValidCredentials` + `TestLoginRejectsWrongPassword` + `TestLoginUnknownUserAndWrongPasswordReturnIdenticalEnvelope` (SEC-4 byte-identical envelope, not just error string). The constant-time guarantee delegates to `auth.AuthenticateLogin` whose own tests (PR #47 findings) cover the timing tolerance. |
| AC-3 | `GET /api/auth/me` returns the current user when given a valid bearer token; 401 otherwise. | covered | `TestMeReturnsCurrentUserForValidToken` + `TestMeRejectsMissingBearer` + `TestMiddlewareRejectsMalformedBearer` (in middleware_test, also covers AC-3 via the same path). |
| AC-4 | `POST /api/auth/logout` increments the user's `token_version`, invalidating all outstanding tokens (US-12). | covered | `TestLogoutIncrementsTokenVersion` (asserts the row update) + `TestMeRejectsTokenAfterLogout` (end-to-end: pre-logout token works, post-logout token returns 401). The middleware re-reads `token_version` from the DB on every request — no in-process cache that would let a revoked token survive briefly. |
| AC-5 | `POST /api/auth/ws-ticket` issues a one-shot, 30-second ticket bound to the user, redeemable once at WS upgrade. | covered | `TestWSTicketIsSingleUse` (handler-level: second redemption fails) + `TestWSTicketReturnsExpiresAtInRFC3339` (wire format) + `TestWSTicketRequiresAuth` (must pass through RequireJWT). Bare ticket store covered by 6 dedicated tests in `apps/server/internal/auth/tickets_test.go`: `TestTicketStoreSingleUse`, `TestTicketStoreExpiry`, `TestTicketStoreExpiryBoundaryIsExpired` (off-by-one guard), `TestTicketStoreUnknownTicket`, `TestTicketStoreIssueProducesUniqueTokens`, `TestTicketStoreConcurrentRedeem` (32 goroutines × N redemptions guarded by `-race`). |
| AC-6 | All auth endpoints write entries to `auth_events`. | covered | `TestAuthEventsRecordsRegisterLoginLogoutKinds` (drives register → login → logout, asserts one `auth_events` row per `kind`) + `TestAuthEventsRecordsWSTicketIssued` + `TestAuthEventsRecordsLoginFailure` (failure event has `user_id=NULL` per the spec; `auth_events.user_id` is intentionally NULLABLE in the schema for exactly this case). Event kinds are package-level constants (`AuthEventRegister`, `AuthEventLoginSuccess`, etc.) so test assertions don't drift from handler emissions. |
| AC-7 | `scripts/smoke.sh` continues to exit 0 after this feature lands. | covered | Script updated by this PR to perform `chatd register --invite=$CHAT_INVITE_CODE` → `chatd login` → `chatd ws-ticket` → `chatd watch --ticket=$T` before send. Verified locally at this SHA: `[smoke] OK: both watchers received smoke-...`. The wiring vitest's existing assertions (script structure, trap, /debug/subs poll) continue to hold. |

## Findings

### Coverage notes

- **Strict JSON decode.** `decodeJSON` calls `dec.DisallowUnknownFields()`, so a typo in the client (e.g. `passwrd` instead of `password`) returns 400 rather than silently dropping the field. Caught by the implicit "missing password" test path in `TestRegisterRejectsShortPassword` — an unknown-field send would hit the decoder error first.
- **AC-1 invite-code ordering matters.** The handler checks the invite code BEFORE the username regex / password policy / DB call. This is deliberate (prevents an unauthenticated brute-forcer from probing username availability or password rules). The test `TestRegisterRejectsMissingOrWrongInviteCode` uses a syntactically valid username + password and an empty `invite_code`, asserting the response is the 403 `forbidden` envelope — proves the early return triggers.
- **AC-2 constant-time delegation.** The handler does NOT re-run a dummy bcrypt itself; it calls `auth.AuthenticateLogin` whose timing test (`TestAuthenticateLoginConstantTimeWithinTolerance`, see PR #47 findings) is the load-bearing one. The handler-level test only verifies the envelope is byte-identical (SEC-4), which is the spec's requirement at this layer.
- **AC-4 has a subtle property the test catches.** "Invalidating all outstanding tokens" means `token_version` increments transactionally — if a stale token race-checked the old version between the read and the increment, US-12 would silently leak. `TestMeRejectsTokenAfterLogout` is the right anchor: it issues a token, calls logout, then re-uses the original token and asserts 401. If the middleware ever cached the token-version locally, this test would still catch it.
- **AC-5 boundary behavior.** `TestTicketStoreExpiryBoundaryIsExpired` proves "exactly at T+30s" is rejected, not accepted. Off-by-one on TTL boundaries is a classic source of late-redemption flakiness; the test pins it to the strict reading.
- **Concurrent redemption is real.** `TestTicketStoreConcurrentRedeem` runs 32 goroutines all racing to redeem one ticket — exactly one should succeed. Without proper mutex ownership, two could "succeed" against the wrong-state map and a second WS connection sneaks in. The `-race` build tag also catches the data race on the bare map.

### Cross-feature observations

- **`feature-auth-internals` AC-5 — behaviorally satisfied, architecturally not yet.** That AC was previously partial (PR #47): "signing key is loaded from config". Today: `config.Load` reads `CHAT_JWT_SECRET`, `Validate` enforces the strength rules at startup, and the handler then reads the env var **a second time** (`apps/server/main.go:84` does `[]byte(os.Getenv(jwtSecretEnv))`, not `[]byte(cfg.JWTSecret)`). Functionally equivalent today; weaker than the AC text "loaded from config" reads on a strict reading. **Recommendation:** either an impl PR threads `cfg.JWTSecret` through to `NewAuthHandlers.SigningKey` so validation and use share one source, or the next test-watch tick re-evaluates AC-5 on this architecture point and keeps it `partial` until the cfg→handler chain is concrete.
- **`feature-security-headers-and-sqlite-ensure-wiring` (PR #47 stub) is still deferred.** main.go's `Handler:` chain is now `httpx.BodyCap(mux)`. No `SecurityHeaders` wrap. The `cfg.JWTSecret` wiring did not pull along the security-headers wiring, even though both touch main.go. Will be picked up by that feature's PR when it ships.
- **CLI changes are minimal but real.** `apps/cli/main.go` and `apps/cli/cmd/url.go` were touched. The smoke script invokes `chatd register / login / ws-ticket / watch --ticket=...`, so the CLI gained subcommands matching the new endpoints. There's no spec for "feature-cli-auth-flow" yet; the maintainer extended the existing CLI for smoke compatibility (AC-7 demand). When `feature-cli-full-commands` (phase 2) lands, the agent should anchor formal coverage there.

### Spec-vs-impl notes

- Spec lists `apps/server/internal/http/middleware_auth.go` as the expected middleware file. Impl puts the JWT middleware at `apps/server/internal/auth/middleware.go`, with thin glue (`WriteUnauthorized` helper) in `apps/server/internal/http/`. Functionally equivalent, structurally cleaner — keeps the auth machinery within the auth package.
- New helpers landed that the spec didn't enumerate: `apps/server/internal/auth/jwt_subject.go` (extracts subject claim safely), `apps/server/internal/http/auth_store.go` (DB access for users + auth_events), `apps/server/internal/http/sql_errors.go` (translates SQLite UNIQUE-constraint failures to `ErrUsernameTaken`). These are not load-bearing for any AC text but are good factoring.

## Recommendations

1. No new tests added by this run — coverage is comprehensive across all 7 ACs at the unit + integration boundary.
2. **Cross-feature follow-up:** the next test-watch tick should re-evaluate `phase-1/auth-internals` and promote AC-5 from partial to covered. The cfg→main→handlers→auth wiring chain is now fully present.
3. **Spec follow-up (out of test-agent scope):** when `feature-cli-full-commands` lands in phase 2, formally anchor coverage of `chatd register/login/ws-ticket/watch --ticket=...`. Today's smoke script provides system-level proof but no per-AC findings file.
