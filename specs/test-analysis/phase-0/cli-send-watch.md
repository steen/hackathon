---
feature: cli-send-watch
phase: phase-0
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: partial
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# E2E test analysis: CLI `chatd send` and `chatd watch` (no auth)

**Spec:** `specs/plans/phase-0/feature-cli-send-watch.md`
**Implementation status:** partial (effectively superseded) — `apps/cli/main.go` exists with `register`, `login`, `whoami`, `logout`, `channels`, `history`, `send`, `watch` subcommands routed through `apps/cli/cmd/*.go`, but the entire CLI was rebuilt by phase-2 PR #88 on top of `packages/go-client`. The phase-0 contract ("raw WS, no auth, default `ws://localhost:PORT/ws`, `chatd send <message>` with no channel arg") is gone: send/watch now require a channel-id argument and an authenticated bearer token persisted under `$XDG_CONFIG_HOME/chatd/config.json`. The replacement is owned by phase-2 `feature-cli-full-commands.md`.
**E2E test directory:** `tests/e2e/phase-0/cli-send-watch/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success. | deferred | — |
| AC-2 | `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line. | deferred | — |
| AC-3 | Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`. | deferred | — |
| AC-4 | No login flow or token handling exists in this phase. | deferred | — |

## Findings

### Deferred E2E tests

All four ACs are deferred because the impl that satisfied them was removed by audit #78 / replaced by PR #88. The current `chatd` binary cannot reach the original phase-0 contract:

- AC-1's signature `chatd send <message>` is now `chatd send <channel> <message|->` and requires a logged-in token (verified by reading `apps/cli/main.go` and `apps/cli/cmd/send.go`).
- AC-2's signature `chatd watch` is now `chatd watch <channel>`, also auth-gated.
- AC-3's `--url` flag is now `--server` (and resolves to a base HTTP URL, not a `ws://` endpoint — see `apps/cli/main.go` package comment).
- AC-4 is directly contradicted: a login flow and token persistence now exist (`apps/cli/cmd/login.go`, `apps/cli/internal/config/`).

The phase-2 spec `specs/plans/phase-2/feature-cli-full-commands.md` (or its sibling) covers the replacement contract; new E2E coverage should land under `tests/e2e/phase-2/cli-full-commands/`, not here.

If /test-implement wants placeholder E2E files for the phase-0 ACs to keep the bookkeeping symmetrical, each should be a `t.Skip("phase-0 contract removed by PR #78/#88; see phase-2/cli-full-commands")` with the AC tag in a leading comment so /test-analyze can detect it:

- **AC-1 — `chatd send <message>` writes one frame and exits 0.**
  - **What to assert:** binary exits 0; the WS server received exactly one text frame with the message bytes.
  - **Layer:** Go (boot server + run CLI binary).
  - **File path:** `tests/e2e/phase-0/cli-send-watch/send_test.go`
  - **Setup it needs:** N/A — current binary cannot satisfy. If kept, a skipped placeholder.
  - **Helpers it can reuse:** none.

- **AC-2 — `chatd watch` prints received frames one-per-line.**
  - **What to assert:** stdout of the watcher process contains the published line within a deadline.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/cli-send-watch/watch_test.go`
  - **Setup it needs:** N/A.

- **AC-3 — `--url`/`CHAT_SERVER` overrides default.**
  - **What to assert:** Setting `CHAT_SERVER=ws://...` (no flag) routes the dial to that URL. Defaults to `ws://localhost:PORT/ws`.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/cli-send-watch/server_url_test.go`
  - **Setup it needs:** N/A — current `--server` flag accepts an HTTP base, not `ws://`.

- **AC-4 — No login flow or token handling.**
  - **What to assert:** `chatd send` and `chatd watch` succeed without any prior `chatd login` and without any token file on disk.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/cli-send-watch/no_auth_test.go`
  - **Setup it needs:** N/A — login is now mandatory.

### Partial / suspect coverage

(None — `tests/e2e/` does not exist yet.)

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern for booting `apps/server` in a Go test: it builds the binary in `t.TempDir()`, picks a free port via `net.Listen("tcp", "127.0.0.1:0")`, generates a random `CHAT_JWT_SECRET` and `CHAT_INVITE_CODE` via `crypto/rand`. The first E2E test for any feature should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and the `runningServer` struct verbatim into a sibling `harness_test.go` in the per-feature dir. Do not import them — that test's package is `server_ws_hub_test`, the helpers are intentionally local. For CLI tests, the same `harness_test.go` would also need a `buildChatd(t)` step that runs `go build -o $TMP/chatd ./apps/cli`.

## Recommendations for /test-implement

- Do not write the four phase-0 ACs as live E2E tests — the impl was removed. Either skip the directory entirely or land four `t.Skip(...)` placeholders with the `AC-N` tag in a leading comment so /test-analyze treats them as accounted-for-and-deferred.
- Owning coverage of the replacement CLI belongs to `tests/e2e/phase-2/cli-full-commands/`; do not stuff it into this directory.
- If skip-placeholders are written, add a `harness_test.go` only if it has a real consumer; a directory with four trivial `t.Skip` files needs no harness.
