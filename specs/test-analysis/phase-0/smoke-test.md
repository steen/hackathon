---
feature: smoke-test
phase: phase-0
analyzed_at: 2026-05-03T13:35:37Z
analyzed_commit: c3e7e991b84a21a648eaed1ce7188c28647079db
implementation_status: stub
total_acs: 5
covered: 0
partial: 1
missing: 0
deferred: 4
---

# Test analysis: System smoke test (`scripts/smoke.sh`)

**Spec:** `specs/plans/phase-0/feature-smoke-test.md`
**Implementation status:** stub — `scripts/smoke.sh` does not exist. Root `package.json` `test` script fans out via `pnpm -r --if-present run test`; nothing invokes a smoke script.

The two upstream features this gates on (`feature-server-ws-hub`, `feature-cli-send-watch`) are now both implemented (PR #14 and PR #17), so the smoke script can finally be written. Until it is, ACs 1–3 remain deferred.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `scripts/smoke.sh` boots the server, starts two `chatd watch` processes, sends a message via `chatd send`, and asserts both watchers received it. | deferred | impl is stub (script not written) |
| AC-2 | The script exits 0 on success, non-zero with a clear error message on failure. | deferred | impl is stub |
| AC-3 | The script tears down all spawned processes on completion (success or failure). | deferred | impl is stub |
| AC-4 | The script is referenced by the root `package.json` `test` script (or an equivalent task) so it runs as part of the standard test workflow. | partial | `tests/smoke-test/wiring.test.ts` (vacuous: passes when script absent, asserts wiring only when script exists) |
| AC-5 | This test stays green for the rest of the project (validation criterion for Phase 0 and Phase 1). | deferred | meta-AC; not a unit test |

## Findings

### Deferred — script not yet written

ACs 1–3 are runtime properties of `scripts/smoke.sh`. Until the script lands, no Go or vitest test can substitute. Skipped placeholders in `tests/smoke-test/wiring.test.ts` anchor the AC IDs.

Now that both upstream features are implemented (CLI `Send`/`Watch` library at `apps/cli/cmd`, server at `apps/server`), the agent recommends the next implementer:

1. Write `apps/cli/main.go` exposing `chatd send` / `chatd watch` as a binary (Cobra root + subcommands wrapping `cmd.Send` / `cmd.Watch`). The library functions exist; the binary entry point does not.
2. Write `scripts/smoke.sh` per the spec: build server + cli, start server in background with `CHAT_SERVER_PORT=<random>`, start two `chatd watch` processes redirecting to temp files, run `chatd send hello`, poll the temp files until both contain "hello", trap EXIT to teardown.
3. Wire root `package.json` `scripts.test` to invoke `bash scripts/smoke.sh` before/after the per-workspace fan-out.

### Partial — AC-4 wiring test is vacuous today

`tests/smoke-test/wiring.test.ts` returns early when `scripts/smoke.sh` does not exist, so it currently passes without enforcing anything. This was a deliberate bootstrap choice (don't fail CI for a not-yet-written script). The test will start enforcing the wiring as soon as the script appears. No change recommended now.

### Cross-feature dependency closed

The bootstrap analysis flagged "this feature gates on `feature-server-ws-hub` and `feature-cli-send-watch`". Both are now done at the library level. Only the CLI binary entry point and the script itself remain — both belong in a follow-up feature, not in test analysis.

## Recommendations

1. No new tests this cycle (the script is what would be tested, and it doesn't exist).
2. When `scripts/smoke.sh` lands, the agent will detect it on the next tick and re-evaluate ACs 1–3.
3. The `tests/smoke-test/wiring.test.ts` early-return guard should be removed at the same time `scripts/smoke.sh` is committed, so AC-4 starts enforcing live.
