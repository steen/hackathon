---
feature: cli-send-watch
phase: phase-0
analyzed_at: 2026-05-03T15:15:00+02:00
analyzed_commit: 206b9e265fadf27b7b59cf0f99e7db941231676a
implementation_status: stub
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# Test analysis: CLI `chatd send` and `chatd watch` (no auth)

**Spec:** `specs/plans/phase-0/feature-cli-send-watch.md`
**Implementation status:** stub — `apps/cli/` contains only `doc.go` (`package cli` declaration, no `main`, no `send`/`watch` subcommands).

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success. | deferred | impl is stub |
| AC-2 | `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line. | deferred | impl is stub |
| AC-3 | Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`. | deferred | impl is stub |
| AC-4 | No login flow or token handling exists in this phase. | deferred | impl is stub (asserted by absence) |

## Findings

### Deferred tests

- **AC-1** — Go unit test against a fake `httptest.Server`/`gorilla/websocket` that captures the frame written by `chatd send`. Assertion: payload equals the joined args. Location: `tests/cli-send-watch/cli_test.go`.
- **AC-2** — Go unit test where the fake WS server emits N frames; assert stdout from `chatd watch` contains all N lines in order, separated by newline. Location: same file.
- **AC-3** — Go unit test exercising URL precedence: `--url` flag wins over `CHAT_SERVER` env var, both win over the default. Location: same file.
- **AC-4** — Negative assertion test: `chatd send` makes no `Authorization` header on the upgrade request. Location: same file.

A skipped placeholder test per AC has been written so the IDs are anchored.

### Implementation gap

Spec mentions `apps/cli/go.mod` but per `CLAUDE.md` no per-app `go.mod` should exist. Update spec to drop that entry.

The smoke test (separate feature) depends on this implementation; tests there are also deferred.

## Recommendations

1. Skipped test placeholders written under `tests/cli-send-watch/`.
2. When `apps/cli/main.go`, `cmd/send.go`, and `cmd/watch.go` are written, remove the `t.Skip` and import from `hackathon/apps/cli/...`.
3. Update spec to remove `apps/cli/go.mod` from "Files expected to be touched or created".
