---
feature: cli-send-watch
phase: phase-0
analyzed_at: 2026-05-03T14:06:50Z
analyzed_commit: 4902b5f55fc78b6ea268a001d0aec33d5ad34ff8
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: CLI `chatd send` and `chatd watch` (no auth)

**Spec:** `specs/plans/phase-0/feature-cli-send-watch.md`
**Implementation status:** implemented — `apps/cli/cmd/{send,watch,url}.go` provide the public `Send`, `Watch`, and `ResolveURL` functions, AND `apps/cli/main.go` (added in PR #18) wraps them as the `chatd send <msg>` / `chatd watch` binary, with optional `--url` flag handling. Verified at runtime by `scripts/smoke.sh`, which builds and exercises the binary end-to-end.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success. | covered | `apps/cli/cmd/send_test.go::TestAC_0_1_SendWritesSingleTextFrameToWebSocket` + `tests/cli-send-watch/cli_test.go::TestAC1_CliSendWatch_SendWritesPayloadAsTextFrameAndExitsZero` |
| AC-2 | `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line. | covered | `apps/cli/cmd/watch_test.go::TestAC_0_2_WatchPrintsEachFrameOnItsOwnLine` + `tests/cli-send-watch/cli_test.go::TestAC2_CliSendWatch_WatchPrintsEveryReceivedFrameOnePerLine` |
| AC-3 | Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`. | covered | `apps/cli/cmd/url_test.go` (3 tests covering flag-over-env-over-default) + `tests/cli-send-watch/cli_test.go::TestAC3_CliSendWatch_UrlPrecedenceFlagOverEnvOverDefault` |
| AC-4 | No login flow or token handling exists in this phase. | covered | `apps/cli/cmd/no_auth_test.go::TestAC_0_4_NoAuthSymbolsReferencedFromCLI` (static walk of `apps/cli/**/*.go`) + `tests/cli-send-watch/cli_test.go::TestAC4_CliSendWatch_NoAuthorizationHeaderOnUpgrade` (runtime assertion) |

## Findings

### Covered

All four ACs have direct in-package coverage in `apps/cli/cmd/*_test.go`. The bootstrap-era skipped placeholders in `tests/cli-send-watch/cli_test.go` have been replaced with thin live tests that exercise the public `cmd.Send`, `cmd.Watch`, and `cmd.ResolveURL` functions to anchor the AC IDs at the system-test layer. They complement (rather than duplicate) the deeper in-package tests:

- The in-package `send_test.go` records protocol details (frame type, close code, payload bytes); the `tests/`-layer test asserts only the system-visible behavior (one frame, contains the message text, no error).
- The in-package `watch_test.go` exercises both the happy path and the context-cancel teardown handshake; the `tests/`-layer test asserts only that N input frames produce N output lines on the writer.
- The in-package `url_test.go` covers the precedence matrix; the `tests/`-layer test re-runs the matrix as a smoke check from outside the package.
- The in-package `no_auth_test.go` is a static AST walk asserting no auth-related identifiers/imports exist; the `tests/`-layer test makes a live dial and checks the request the server receives carries no `Authorization` header.

### Implementation gap closed

PR #18 added `apps/cli/main.go`, the binary entry point that wraps the existing `cmd.Send`/`cmd.Watch`/`cmd.ResolveURL` library calls. The previous analysis's caveat ("`chatd` is not yet runnable as a binary") no longer applies. The binary is exercised end-to-end every time `scripts/smoke.sh` runs (which `pnpm test` invokes).

The `--url` flag handling in `apps/cli/main.go` is hand-rolled rather than Cobra-based — the spec mentioned a "subcommand dispatcher" but didn't pin a library. The hand-rolled parser covers the documented usage; switching to Cobra later would not break any AC.

## Recommendations

1. No new tests needed; coverage is complete.
2. Spec follow-up: drop `apps/cli/go.mod` from "Files expected to be touched or created" (project uses single root go.mod).
