---
feature: startup-config-checks
phase: phase-1
analyzed_at: 2026-05-03T15:29:59Z
analyzed_commit: 6bfb65d91d02eb571131c0ca8b6f00fd7977c78d
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Startup config checks (JWT secret, bind, invite)

**Spec:** `specs/plans/phase-1/feature-startup-config-checks.md`
**Implementation status:** implemented — `apps/server/internal/config/config.go` exposes `Load()` + `Config.Validate()`; `apps/server/main.go:39-47` calls them before any DB or HTTP setup, prints the offending field to stderr, and `os.Exit(1)`s on the first violation. The implementation goes meaningfully beyond the spec's denylist with entropy/ASCII/repeated-char/padded-default checks. 17 tests, all passing.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | Server refuses to start if the JWT signing secret is shorter than the documented minimum length. | covered | `apps/server/internal/config/config_test.go::TestSEC1_RejectsShortJWTSecret` + `TestSEC1_RejectsMissingJWTSecret` (empty case). `MinJWTSecretBytes = 32`. |
| AC-2 | Server refuses to start if the JWT signing secret matches a dev-default denylist (e.g., empty string, `dev`, `secret`, `change-me`). | covered | `TestSEC1_RejectsDenylistedJWTSecret` exercises the denylist. The impl extends the spec's "e.g." set to 16 known dev-defaults and matches case-insensitively, so `Change-Me`, `SECRET`, `Hackathon`, etc. are all rejected. The `allSameAfter` helper catches `change-meXXXXXX...` style padding past the length floor. |
| AC-3 | If the configured bind address is non-loopback, the server refuses to start unless `CHAT_ALLOW_PUBLIC_BIND=1` is set. | covered | `TestSEC2_RejectsPublicBindWithoutOverride` + `TestSEC2_AllowsPublicBindWhenOverrideSet` + `TestSEC2_AllowsLoopbackBindByDefault` + `TestSEC2_RejectsMalformedAddr`. Loopback detection handles `localhost`, `127.0.0.0/8`, `::1`. Empty host (e.g. `:8080`) is treated as bind-all and gated by the same override. |
| AC-4 | Server refuses to start if `CHAT_INVITE_CODE` is unset (since registration depends on it; see US-11). | covered | `TestUS11_RejectsMissingInviteCode`. |
| AC-5 | All failure modes print a clear, actionable error to stderr and exit non-zero. | covered | `main.go:42-44` writes via `fmt.Fprintf(os.Stderr, "config: %v\n", err)` then `os.Exit(1)`. The error messages name the offending env var by name (`CHAT_JWT_SECRET`, `CHAT_LISTEN_ADDR`, `CHAT_INVITE_CODE`) and explain the constraint ("got N bytes, need at least 32"; "set CHAT_ALLOW_PUBLIC_BIND=1 to allow"). `TestErrorsNeverContainSecret` is a non-AC sanity guard that error text never echoes the configured secret value back. |

## Findings

### Coverage notes

- **Implementation exceeds the spec.** The denylist in the spec is "e.g., empty string, `dev`, `secret`, `change-me`". The impl ships 16 entries plus four orthogonal weak-secret defenses (length, ASCII-only, repeated-char, low-entropy <5 distinct bytes). Each defense has its own test (`TestSEC1_Rejects{ShortJWTSecret,RepeatedSingleChar,LowEntropySecret,NonASCIISecret,DenylistedJWTSecret}`). The `TestSEC1_AcceptsRandom32ByteSecret` is the positive-path anchor that the strictness doesn't accidentally reject good secrets.
- **Padded-default trap (`allSameAfter`).** A subtle test would be `change-mexxxxxxxxxxxxxxxxxxxxxxxxx` (32 bytes, clears the length bar). The impl catches it via `allSameAfter`. No test directly exercises this branch by name; it's covered by the broader denylist test indirectly. Worth an explicit test eventually but not strictly an AC.
- **AC-5 is observed at two levels.** The package layer (`config.Validate`) returns a `error`; the wiring layer (`main.go:42-44`) is the one that writes to stderr + exits non-zero. The package tests cover the message content; main.go behavior is exercised by run-the-binary integration tests downstream (e.g. the smoke script's recent fix `CHAT_JWT_SECRET=$(openssl rand -hex 16)` proves end-to-end that an invalid config fails the boot at runtime).
- **Errors never echo secrets.** `TestErrorsNeverContainSecret` is a hardening test, not gated by any AC. It scrubs every validation error path to assert the configured `JWTSecret` value never appears in `err.Error()`. Catches the regression where someone changes a message from `"%s is too short"` to `"%s=%q is too short"` and accidentally leaks the secret to logs.

### Cross-feature observation

`feature-auth-internals` AC-5 ("Token signing key is loaded from config, validated by startup checks") was previously marked partial in PR #47 because no validated config source existed. **It still stays partial today**: `cfg.JWTSecret` is now loaded and validated, but no code path passes it to `auth.Issue` or `auth.Parse`. The wiring lives in the not-yet-shipped `feature-auth-endpoints`. The next test-watch tick after that lands should re-evaluate auth-internals AC-5.

`scripts/smoke.sh` (modified by 21ac520) and the `tests/server-ws-hub` and `tests/cli-send-watch` integration-test envs (modified by 9c7e9cd, 69410fa) all needed `CHAT_JWT_SECRET` + `CHAT_INVITE_CODE` injected at runtime to keep passing — the new validation is load-bearing at the integration boundary, exactly as intended. The maintainer wisely chose to generate them with `openssl rand` rather than commit literals (per CLAUDE.md "no hardcoded secrets").

### Spec note

Spec lists `apps/server/main.go (call Validate on startup)` as expected file. Done at `main.go:39-47` and the loop logging `config check ok: <name>` lines is a nice operator-visible signal that all checks ran.

## Recommendations

1. No new tests added by this run — coverage is strong at both the package and integration layers.
2. **One-line opportunity:** add an explicit `TestSEC1_RejectsPaddedDefault` exercising `change-mexxxxxxxxxxxxxxxxxxxxxxxxx` so the `allSameAfter` branch has a named test. Catches the case where someone refactors the loop and accidentally drops the prefix-then-padding check. Not gated by any AC.
3. Auth-internals AC-5 follow-up: when `feature-auth-endpoints` (or whoever) wires `cfg.JWTSecret` into `auth.Issue` / `auth.Parse`, re-evaluate that finding. Today: still partial.
