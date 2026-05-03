---
feature: cli-send-watch
phase: phase-0
analyzed_at: 2026-05-03T13:35:37Z
analyzed_commit: c3e7e991b84a21a648eaed1ce7188c28647079db
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: CLI `chatd send` and `chatd watch` (no auth)

**Spec:** `specs/plans/phase-0/feature-cli-send-watch.md`
**Implementation status:** implemented — `apps/cli/cmd/{send,watch,url}.go` provide the public `Send`, `Watch`, and `ResolveURL` functions. (Note: `apps/cli/main.go` and a Cobra-style root command are not yet wired; the spec's "subcommand dispatcher" wording is satisfied at the library level only. A binary entry point remains TODO and will be picked up by a follow-up feature.)

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

### Implementation gap (out of test scope)

`apps/cli/main.go` and a CLI binary entry point (Cobra root + `send`/`watch` subcommands wiring) are not yet present. The spec's "implementation steps" mention `apps/cli/main.go` and a "subcommand dispatcher". This gap doesn't affect the ACs above — they all describe library behavior — but does mean `chatd` is not yet runnable as a binary. A follow-up feature should address it. No additional tests are needed from the test-analysis agent at that time; existing tests already cover the library surface that the CLI binary will wrap.

## Recommendations

1. Replaced the skipped placeholders in `tests/cli-send-watch/cli_test.go` with live anchor tests.
2. Spec follow-up: add a separate "CLI binary wiring" feature for `apps/cli/main.go` and the Cobra root command. Until then, the spec's status of "done (PR #14)" is accurate at the library level only.
3. Spec follow-up: drop `apps/cli/go.mod` from "Files expected to be touched or created" (project uses single root go.mod).
