---
feature: startup-config-checks
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: Startup config checks (JWT secret, bind, invite)

**Spec:** `specs/plans/phase-1/feature-startup-config-checks.md`
**Implementation status:** implemented — `apps/server/internal/config/config.go` defines `Config.Load` and `Config.Validate`; `apps/server/main.go:57-64` calls `cfg.Validate()` before any other setup and exits with `log.Fatalf` on error. The PRD §9 `CHAT_ALLOW_PUBLIC_BIND` override is referenced at `main.go:71`.
**E2E test directory:** `tests/e2e/phase-1/startup-config-checks/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | Server refuses to start if the JWT signing secret is shorter than the documented minimum length. | missing | — |
| AC-2 | Server refuses to start if the JWT signing secret matches a dev-default denylist (e.g., empty string, `dev`, `secret`, `change-me`). | missing | — |
| AC-3 | If the configured bind address is non-loopback, the server refuses to start unless `CHAT_ALLOW_PUBLIC_BIND=1` is set. | missing | — |
| AC-4 | Server refuses to start if `CHAT_INVITE_CODE` is unset (since registration depends on it; see US-11). | missing | — |
| AC-5 | All failure modes print a clear, actionable error to stderr and exit non-zero. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — short JWT secret rejected at startup**
- **What to assert:** Build the binary. Start it with `CHAT_JWT_SECRET=short` (likely well below the documented minimum — verify the threshold by reading `config/config.go` once during test authoring; PRD §9 SEC-1 says "≥ 32 bytes when hex-encoded random source is used"). Capture combined stdout+stderr. `cmd.Wait()` → assert exit code is non-zero (`*exec.ExitError`). Assert stderr contains a substring naming the env var (`CHAT_JWT_SECRET`) and the failure reason (some token like `too short`, `minimum`, `length`). Assert no port was listening (the test must NOT call `waitForPort` here — it would wait the full timeout). Conversely, with `CHAT_JWT_SECRET=<random 32-byte hex>` and the rest of the env valid, boot succeeds and port listens.
- **Layer:** Go (boot binary, capture exit + stderr).
- **File path:** `tests/e2e/phase-1/startup-config-checks/jwt_secret_length_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port; build factored into a helper. Other env: `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. The test does NOT use the gold-standard `startServer` (which expects success); instead it needs `tryStartServer(t, env) (exitCode int, stderr string)` that runs `cmd.Run()` synchronously and captures output.
- **Helpers it can reuse:** none — first test in dir. Define harness with `tryStartServer(t, env)` and a `successStartServer(t, env)` variant for the positive case.

**AC-2 — dev-default JWT secret denylist rejected**
- **What to assert:** For each denylisted value the spec calls out (verify the exact list by reading `config/config.go` once during test authoring; spec mentions empty string, `dev`, `secret`, `change-me`), boot the binary with `CHAT_JWT_SECRET=<denylisted>` and the rest of the env valid; assert non-zero exit; assert stderr names the env var and signals "denylisted" or "weak default" or similar (verify exact phrasing). Assert with a strong random secret, boot succeeds. Note that the empty-string case may overlap with AC-1's "too short" — that's fine, both ACs are satisfied by the same rejection.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/startup-config-checks/jwt_secret_denylist_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1.

**AC-3 — non-loopback bind rejected without `CHAT_ALLOW_PUBLIC_BIND=1`**
- **What to assert:** Boot with `CHAT_LISTEN_ADDR=0.0.0.0:0` (or any non-loopback host) and the rest of the env valid (no `CHAT_ALLOW_PUBLIC_BIND`); assert non-zero exit; assert stderr names the env var (`CHAT_LISTEN_ADDR`) and the override env var (`CHAT_ALLOW_PUBLIC_BIND`) so the operator knows the recovery path. Boot with `CHAT_LISTEN_ADDR=0.0.0.0:<freePort>` and `CHAT_ALLOW_PUBLIC_BIND=1` → boot succeeds, port listens. Boot with `CHAT_LISTEN_ADDR=127.0.0.1:<freePort>` (loopback) and no override → boot succeeds. Boot with `CHAT_LISTEN_ADDR=[::1]:<freePort>` (loopback v6) and no override → boot succeeds.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/startup-config-checks/public_bind_guard_test.go`.
- **Setup it needs:** same as AC-1; binding `0.0.0.0` requires permission on most OSes — should be fine on macOS/Linux without root for high ports. The test should pick its own free port.
- **Helpers it can reuse:** harness from AC-1.

**AC-4 — missing `CHAT_INVITE_CODE` rejected**
- **What to assert:** Boot with all env valid except `CHAT_INVITE_CODE` (omit entirely; in Go `exec.Cmd` setting `cmd.Env` to a slice without that key is sufficient). Assert non-zero exit; assert stderr names `CHAT_INVITE_CODE` and signals "must be set" or "required". Conversely, with the var set to a random non-empty value, boot succeeds. Also test the empty-string case (`CHAT_INVITE_CODE=`) — assert behaves the same as missing (rejected), per the spec's "unset" intent.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/startup-config-checks/invite_code_required_test.go`.
- **Setup it needs:** same as AC-1; harness must allow constructing a minimal env that explicitly excludes named keys.
- **Helpers it can reuse:** harness from AC-1.

**AC-5 — every failure path prints a clear, actionable error and exits non-zero**
- **What to assert:** This is the cross-cutting umbrella for AC-1..AC-4. Define the test as a table-driven sweep: for each `(env_override, expected_substring)` pair from the four prior ACs (e.g. `("CHAT_JWT_SECRET", "short", "CHAT_JWT_SECRET")`, `("CHAT_JWT_SECRET", "secret", "denylist")`, `("CHAT_LISTEN_ADDR", "0.0.0.0:0", "CHAT_ALLOW_PUBLIC_BIND")`, `("CHAT_INVITE_CODE", "", "CHAT_INVITE_CODE")`), run `tryStartServer`, assert exit code is non-zero AND the captured stderr is non-empty AND it contains the expected substring. Additionally assert no panic stack (`goroutine ` substring absent) and no Go internal path leaked (`runtime/`, `apps/server/internal/` absent in stderr), since "actionable" means user-readable not developer-readable.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/startup-config-checks/error_messages_actionable_test.go`.
- **Setup it needs:** same as AC-1; this test is the large table-driven one.
- **Helpers it can reuse:** harness from AC-1.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. THIS feature is the only one that primarily tests the *failure* boot path — extend the harness with `tryStartServer(t, env map[string]string) (exitCode int, stderr string)` that returns rather than calling `t.Fatal` on non-zero exit. The harness also needs to construct the env without inheriting from the test process (otherwise an inherited `CHAT_INVITE_CODE` defeats AC-4).

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/startup-config-checks/harness_test.go` with copied helpers + `tryStartServer(t, env)` (returns exit code + stderr; never `t.Fatal`s on boot failure) and `successStartServer(t, env) *runningServer` (positive-path helper).
- Add `jwt_secret_length_test.go` (AC-1), `jwt_secret_denylist_test.go` (AC-2), `public_bind_guard_test.go` (AC-3), `invite_code_required_test.go` (AC-4), `error_messages_actionable_test.go` (AC-5 — table-driven over AC-1..AC-4 cases).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
