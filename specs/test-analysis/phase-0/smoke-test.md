---
feature: smoke-test
phase: phase-0
analyzed_at: 2026-05-03T14:06:50Z
analyzed_commit: 4902b5f55fc78b6ea268a001d0aec33d5ad34ff8
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: System smoke test (`scripts/smoke.sh`)

**Spec:** `specs/plans/phase-0/feature-smoke-test.md`
**Implementation status:** implemented — `scripts/smoke.sh` exists and is executable; root `package.json` `test` script runs `bash scripts/smoke.sh && pnpm -r --if-present run test`. Verified by running the script in this worktree at `4902b5f`: both watchers received the broadcast.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `scripts/smoke.sh` boots the server, starts two `chatd watch` processes, sends a message via `chatd send`, and asserts both watchers received it. | covered | `scripts/smoke.sh` itself + `tests/smoke-test/wiring.test.ts::TestAC1_smoke_test_script_executes_the_documented_flow` (static check that the script invokes `./apps/server`, two `chatd watch`s, a `chatd send`, and a watcher-output assertion). Runtime: `pnpm test` invokes the script. |
| AC-2 | The script exits 0 on success, non-zero with a clear error message on failure. | covered | `tests/smoke-test/wiring.test.ts::TestAC2_smoke_test_script_uses_strict_mode_and_explicit_failure_output` (asserts `set -euo pipefail` + the FAIL stderr branch). Runtime: success path observed by `pnpm test`. |
| AC-3 | The script tears down all spawned processes on completion (success or failure). | covered | `tests/smoke-test/wiring.test.ts::TestAC3_smoke_test_script_traps_exit_and_kills_spawned_pids` (asserts `trap … EXIT INT TERM HUP` + a cleanup function that `kill`s recorded PIDs). |
| AC-4 | The script is referenced by the root `package.json` `test` script (or an equivalent task) so it runs as part of the standard test workflow. | covered | `tests/smoke-test/wiring.test.ts::TestAC4_smoke_test_script_is_wired_into_root_package_json_test_script` (now a hard assertion — the bootstrap-era early-return guard is removed). |
| AC-5 | This test stays green for the rest of the project (validation criterion for Phase 0 and Phase 1). | covered (meta) | `tests/smoke-test/wiring.test.ts::TestAC5_smoke_test_script_is_executable_and_present` (per-tick liveness check). The "stays green over time" property is what CI is for; this test only asserts the entry conditions. |

## Findings

### Coverage notes

- **The script is the test.** AC-1, AC-2, AC-3 are runtime properties of `scripts/smoke.sh`. The strongest verification is to actually run it — which `pnpm test` already does, and which CI runs on every commit. The vitest tests in `tests/smoke-test/wiring.test.ts` are static-source assertions: they catch a regression where someone deletes the trap, drops `set -euo pipefail`, or removes a watcher invocation, without re-running the script. They do NOT replace the runtime check; they complement it.
- **No duplicate runtime check.** I considered making the wiring vitest also `exec` `bash scripts/smoke.sh` directly, but that would mean every `pnpm test` runs the script twice (once at the top level, once inside the vitest workspace). Skipped to avoid the waste.
- **Spec change since last analysis.** The spec's "Status" line went from "planned" to "done (PR #18)". The "Files expected to be touched or created" list still mentions `.github/workflows/ci.yml` as optional — CI wiring is not covered here.

### Implementation gap closed

The previous analysis flagged that this feature gated on `feature-server-ws-hub` and `feature-cli-send-watch` (specifically on `apps/cli/main.go` not yet existing). PR #18 closed both gaps: it shipped `apps/cli/main.go` (the `chatd` binary entry point) AND `scripts/smoke.sh` together. As a result this feature went from `stub` → `implemented` in one PR.

## Recommendations

1. The wiring vitest has been hardened: dropped the early-return guard on AC-4 and replaced the four skipped placeholders with concrete static assertions.
2. Optional follow-up (out of test-agent scope): wire `bash scripts/smoke.sh` into `.github/workflows/ci.yml` as a dedicated job step so the runtime check is observable without relying on `pnpm test` being run.
