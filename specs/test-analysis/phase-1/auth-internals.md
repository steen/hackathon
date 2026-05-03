---
feature: auth-internals
phase: phase-1
analyzed_at: 2026-05-03T15:24:52Z
analyzed_commit: 440d18f3597c167bc4b9641f18785fe249d9b69d
implementation_status: implemented
total_acs: 5
covered: 4
partial: 1
missing: 0
deferred: 0
---

# Test analysis: Auth internals (bcrypt + JWT + password policy)

**Spec:** `specs/plans/phase-1/feature-auth-internals.md`
**Implementation status:** implemented — `apps/server/internal/auth/{password,jwt,login,constants}.go` ship the full set: bcrypt cost-10 hashing/verify, HS256 JWT issue/parse with `tv` token-version claim, constant-time login that always runs bcrypt against a precomputed dummy hash on unknown user, and a 10–72-byte password length policy. The package is intentionally caller-agnostic — no repo or HTTP dependency — and 16 in-package tests all pass.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `internal/auth` exposes password hashing/verification using bcrypt with a sane cost. | covered | `apps/server/internal/auth/password_test.go::TestHashVerifyRoundTrip` + `TestVerifyMalformedHashIsCollapsedToInvalid` (collapses every failure mode to `ErrInvalidPassword` so callers can't leak "malformed hash" vs "wrong password" via error type). `BcryptCost = 10` per PRD §9 OWASP floor. |
| AC-2 | JWT issuance and verification include a `tv` (token-version) claim used to invalidate tokens server-side (US-12). | covered | `jwt_test.go::TestJWTIssueParseRoundTrip` + `TestJWTRejectsTokenVersionMismatch` + `TestJWTRejectsTamperedSignature` + `TestJWTRejectsWrongSigningKey` + `TestJWTRejectsExpiredToken` + `TestJWTEmptySigningKeyRejected`. Claims struct embeds `jwt.RegisteredClaims` and adds `TokenVersion int` with json tag `tv`. |
| AC-3 | A constant-time login path: when a user does not exist, the code still performs a bcrypt comparison against a dummy hash so timing does not leak username existence. | covered | `login_test.go::TestAuthenticateLoginConstantTimeWithinTolerance` (5-sample average per arm, 2.5x ratio tolerance — loose by design, see findings below). Plus `TestAuthenticateLoginErrorMessageByteIdenticalForUnknownVsWrongPassword` for the SEC-4 byte-identical error text companion. The constant-time mechanism (`VerifyDummy` against a precomputed hash) is also verified by `password_test.go::TestDummyHashIsValidBcrypt` and `TestDummyHashCostMatchesBcryptCost` — the dummy hash must actually be a valid bcrypt at the production cost or the comparison would short-circuit. |
| AC-4 | A password length policy is enforced (e.g., min 8, max 72 to stay within bcrypt's input limit). | covered | `password_test.go::TestEnforcePolicy` (rejects too-short and too-long; accepts at-bounds). Note: `PasswordMinLen = 10` (PRD §9 wins over the spec's "e.g., min 8"). `PasswordMaxBytes = 72` matches bcrypt's hard limit. |
| AC-5 | Token signing key is loaded from config (validated by startup checks; see `feature-startup-config-checks.md`). | partial | This package accepts `signingKey []byte` as a parameter on every Issue/Parse call (correct shape; `jwt_test.go::TestJWTEmptySigningKeyRejected` enforces non-empty). The "loaded from config" half is for `feature-startup-config-checks` to ship — that feature isn't implemented yet, so end-to-end "key comes from a real config source" is unverifiable today. Marking partial because the package's contribution is done; the load-from-config promise lives elsewhere. |

## Findings

### Coverage notes

- **Constant-time test is intentionally loose.** The 2.5x ratio tolerance in `TestAuthenticateLoginConstantTimeWithinTolerance` is generous because CI runners are noisy. The spec acknowledges this is "a sanity check, not a strict timing assertion". The test catches the failure mode that matters: someone removes `VerifyDummy` and unknown-user becomes microseconds while wrong-password stays ~10ms (bcrypt cost 10). With cost 10 producing 5–50 ms, the floor check (`< 5ms is suspicious`) is the real load-bearing assertion. Skipped under `-short` so unit-test runs stay fast.
- **Byte-identical error message is a separate AC.** Spec lists this only in the test plan ("test_login_error_message_byte_identical..." → covers SEC-4), not in the AC list. The implementation exposes a single `LoginErrorMessage = "invalid username or password"` const and `ErrLogin = errors.New(LoginErrorMessage)` and the test asserts both unknown-user and wrong-password arms return errors with `e.Error() == LoginErrorMessage`. This is what the spec's intent demands; AC-3 ("constant-time path") and the SEC-4 test together cover it.
- **Issuer is hardcoded.** `JWTIssuer = "chat-server"` lives in constants.go with a documented decision ("there is one issuer for the lifetime of the MVP"). `Parse` enforces it via `jwt.WithIssuer`. If a future split server (`chat-admin`?) needs distinct issuers, this becomes a config item — unblocked but un-anticipated.
- **No JWT subject-claim test.** All 6 JWT tests check `tv`, signature, expiry, issuer/key. None explicitly assert the parsed `Subject` matches the input `userID` after a round-trip. The round-trip test uses `claims.Subject` indirectly to set up the token but doesn't assert it on the parse side. Low risk because the subject is part of the canonical JWT envelope and the library handles it, but a one-line addition to `TestJWTIssueParseRoundTrip` would close the gap.

### Architecture note

The package deliberately keeps `AuthenticateLogin` free of JWT signing — the spec says "issuing a JWT is intentionally NOT done here so this function stays free of the signing key and timekeeping dependencies that JWT issuance pulls in". This means `feature-auth-endpoints` will compose `AuthenticateLogin` + `Issue` itself. The seam is correct; no AC tests it because no AC names it, but the next feature's findings should anchor that wiring.

## Recommendations

1. No new tests added by this run — coverage at the package boundary is appropriate for all 5 ACs.
2. **Optional one-liner**: extend `TestJWTIssueParseRoundTrip` to assert `claims.Subject == userID` after parse. Catches a regression where someone drops `Subject` from the claims struct.
3. AC-5 promotion is gated on `feature-startup-config-checks` shipping. When it lands, the wiring of "JWT signing key loaded from `CHAT_JWT_SIGNING_KEY` env var → passed to `auth.Issue`/`auth.Parse`" should appear in either `feature-auth-endpoints` or `feature-startup-config-checks` findings.
