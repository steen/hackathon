---
feature: smoke-test
phase: phase-0
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: System smoke test (`scripts/smoke.sh`)

**Spec:** `specs/plans/phase-0/feature-smoke-test.md`
**Implementation status:** implemented — `scripts/smoke.sh` exists and is wired into `package.json` as both `smoke` and `test` (`"test": "bash scripts/smoke.sh && pnpm -r --if-present run test"`). The script builds both binaries, generates a per-run JWT secret + invite code via `openssl rand`, picks a free port via `python3`, registers/logs in a fresh user, creates a `general` channel via REST, polls `/debug/subs?channel=<id>` until 2 watchers are subscribed, sends one message, polls both watcher stdouts for the message, and traps `EXIT INT TERM HUP` for a bounded TERM-then-KILL cleanup.
**E2E test directory:** `tests/e2e/phase-0/smoke-test/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `scripts/smoke.sh` boots the server, starts two `chatd watch` processes, sends a message via `chatd send`, and asserts both watchers received it. | missing | — |
| AC-2 | The script exits 0 on success, non-zero with a clear error message on failure. | missing | — |
| AC-3 | The script tears down all spawned processes on completion (success or failure). | missing | — |
| AC-4 | The script is referenced by the root `package.json` `test` script (or an equivalent task) so it runs as part of the standard test workflow. | missing | — |
| AC-5 | This test stays green for the rest of the project (validation criterion for Phase 0 and Phase 1). | missing | — |

## Findings

### Missing E2E tests

- **AC-1 — `smoke.sh` runs the boot/two-watch/one-send/assert-both-received cycle.**
  - **What to assert:** Running `bash scripts/smoke.sh` from the repo root exits 0 on a clean tree. The simplest credible E2E test is to invoke the script as a subprocess and check the exit code. Optional stronger variant: parse stdout for the `[smoke] OK: both watchers received <msg>` line. Note: the AC text says "via `chatd send`", but the implemented script uses REST POST after audit #78 (chatd send was rewired by `cf82cdc`); the AC's intent (boot → two watchers → produce one message → assert both received) is still satisfied.
  - **Layer:** Go (wraps `os/exec`) — runs in the same `go test ./tests/e2e/...` invocation as everything else.
  - **File path:** `tests/e2e/phase-0/smoke-test/smoke_runs_test.go`
  - **Setup it needs:** `repoRoot(t)`; ensure `go`, `openssl`, `python3`, `curl`, `bash` are on `PATH` (`t.Skip` with a clear reason if any is absent so CI without those tools doesn't fail spuriously); set `cmd.Dir = root`; capture combined output for the failure message; default to a 60s timeout via `context.WithTimeout`.
  - **Helpers it can reuse:** `repoRoot(t)` from the gold-standard harness.

- **AC-2 — Exit 0 on success, non-zero with clear error on failure.**
  - **What to assert:** Two cases:
    1. Happy path returns exit code 0 (covered by AC-1).
    2. Forced-failure path returns non-zero AND prints a `[smoke] ...` failure line on stderr. Easiest forcing: pre-occupy the chosen port by listening on `127.0.0.1:<port>` from within the test, then run `CHAT_SERVER_PORT=<port> bash scripts/smoke.sh`. The script's port-readiness loop will time out at the "did not open port" check.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/smoke-test/exit_codes_test.go`
  - **Setup it needs:** `net.Listen("tcp","127.0.0.1:0")` to grab and HOLD a port (don't close), pass it via `CHAT_SERVER_PORT`, run the script, assert non-zero exit and a substring match on stderr.
  - **Helpers it can reuse:** `repoRoot(t)`, `freePort(t)` from the gold-standard harness.

- **AC-3 — Script tears down all spawned processes on completion.**
  - **What to assert:** After the script exits (success or failure), no `server` or `chatd` process spawned by it remains alive. The portable proxy: run the script with a known `CHAT_SERVER_PORT`, wait for exit, then attempt to bind the same port — if `net.Listen("tcp","127.0.0.1:<port>")` succeeds, the server PID died.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/smoke-test/teardown_test.go`
  - **Setup it needs:** `freePort(t)` to pick a port, set `CHAT_SERVER_PORT=<port>`, run the script to completion, then bind the port.
  - **Helpers it can reuse:** `repoRoot(t)`, `freePort(t)`.

- **AC-4 — `package.json` `test` script references the smoke script.**
  - **What to assert:** `<root>/package.json`'s `.scripts.test` value contains the substring `scripts/smoke.sh` (currently `"bash scripts/smoke.sh && pnpm -r --if-present run test"`).
  - **Layer:** Go (static check).
  - **File path:** `tests/e2e/phase-0/smoke-test/package_json_wires_smoke_test.go`
  - **Setup it needs:** `encoding/json` decode of `package.json`.
  - **Helpers it can reuse:** `repoRoot(t)`.

- **AC-5 — Stays green for the rest of the project.**
  - **What to assert:** This AC is a meta-property; no single test can prove "stays green forever". The honest E2E equivalent is the AC-1 test executed in CI on every commit — that already enforces it. Recommend marking this AC as covered-by-AC-1 in a leading comment of `smoke_runs_test.go` rather than landing a duplicate test.
  - **Layer:** N/A (covered transitively by AC-1).
  - **File path:** N/A — note in `smoke_runs_test.go` leading comment.
  - **Setup it needs:** N/A.
  - **Helpers it can reuse:** N/A.

### Partial / suspect coverage

(None — `tests/e2e/` does not exist yet.)

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern for booting `apps/server` in a Go test: it builds the binary in `t.TempDir()`, picks a free port via `net.Listen("tcp", "127.0.0.1:0")`, generates a random `CHAT_JWT_SECRET` and `CHAT_INVITE_CODE` via `crypto/rand`. The first E2E test for any feature should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and the `runningServer` struct verbatim into a sibling `harness_test.go` in the per-feature dir. Do not import them — that test's package is `server_ws_hub_test`, the helpers are intentionally local. For this feature, only `repoRoot(t)` and `freePort(t)` are needed; `startServer(t)` is not — the smoke script boots its own server.

## Recommendations for /test-implement

- Create `tests/e2e/phase-0/smoke-test/harness_test.go` with `repoRoot(t)` and `freePort(t)` copied from the gold-standard harness.
- Add `tests/e2e/phase-0/smoke-test/smoke_runs_test.go` with `TestAC1_SmokeTest_HappyPathExitsZero`. Skip cleanly if `go`, `openssl`, `python3`, `curl`, or `bash` are missing from `PATH`. In a leading comment, call out that `AC-5` is covered transitively by this test running in CI on every commit.
- Add `tests/e2e/phase-0/smoke-test/exit_codes_test.go` with `TestAC2_SmokeTest_FailsNonZeroOnPortConflict`.
- Add `tests/e2e/phase-0/smoke-test/teardown_test.go` with `TestAC3_SmokeTest_NoLeftoverProcesses`.
- Add `tests/e2e/phase-0/smoke-test/package_json_wires_smoke_test.go` with `TestAC4_SmokeTest_PackageJsonTestRunsSmoke`.
- Do not write a separate AC-5 test — the `AC-5` tag in `smoke_runs_test.go`'s leading comment is enough for /test-analyze to see it as covered.
