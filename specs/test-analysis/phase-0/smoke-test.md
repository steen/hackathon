---
feature: smoke-test
phase: phase-0
analyzed_at: 2026-05-03T15:15:00+02:00
analyzed_commit: 206b9e265fadf27b7b59cf0f99e7db941231676a
implementation_status: stub
total_acs: 5
covered: 0
partial: 0
missing: 0
deferred: 5
---

# Test analysis: System smoke test (`scripts/smoke.sh`)

**Spec:** `specs/plans/phase-0/feature-smoke-test.md`
**Implementation status:** stub — `scripts/smoke.sh` does not exist; the file `package.json` `test` script does not yet invoke it (currently fans out to workspace `test` scripts via `pnpm -r --if-present run test`).

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `scripts/smoke.sh` boots the server, starts two `chatd watch` processes, sends a message via `chatd send`, and asserts both watchers received it. | deferred | impl is stub |
| AC-2 | The script exits 0 on success, non-zero with a clear error message on failure. | deferred | impl is stub |
| AC-3 | The script tears down all spawned processes on completion (success or failure). | deferred | impl is stub |
| AC-4 | The script is referenced by the root `package.json` `test` script (or an equivalent task) so it runs as part of the standard test workflow. | deferred | impl is stub |
| AC-5 | This test stays green for the rest of the project (validation criterion for Phase 0 and Phase 1). | deferred | impl is stub |

## Findings

### Deferred tests

The smoke test IS the test — it is itself a system test, not something we wrap in a unit test. The agent's role here is limited to verifying the script exists and is wired into `package.json`. ACs 1–3 are the script's own behavior, validated by running it. ACs 4–5 can be checked by static assertions.

- **AC-4** — vitest: assert root `package.json` `scripts.test` either invokes `bash scripts/smoke.sh` directly or fans out to a workspace whose test fans into `smoke.sh`. Location: `tests/smoke-test/wiring.test.ts`.
- **AC-5** — meta-AC; "stays green" is a validation criterion, not a test. The agent will track it on subsequent runs by recording smoke-script exit status when implementation lands.
- **AC-1, AC-2, AC-3** — depend on `apps/server` and `apps/cli` implementations. Skipped placeholder asserting `scripts/smoke.sh` exists is written so the AC IDs are anchored; once the script lands, a CI check can run it.

A skipped vitest under `tests/smoke-test/` anchors the deferred ACs.

### Cross-feature dependency

This feature gates on `feature-server-ws-hub` and `feature-cli-send-watch` both being implemented. Until those land, the script can't be written.

## Recommendations

1. Skipped placeholder under `tests/smoke-test/` written to anchor AC IDs.
2. When `apps/server` and `apps/cli` are implemented, write `scripts/smoke.sh` per the spec, then update root `package.json` `test` script to invoke it (or chain it after the unit test fan-out).
3. Add a CI job that runs `bash scripts/smoke.sh` on every commit once the script exists.
