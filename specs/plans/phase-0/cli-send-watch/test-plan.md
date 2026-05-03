# Test plan: CLI `chatd send` and `chatd watch` (no auth)

**Feature plan:** [feature-cli-send-watch.md](../feature-cli-send-watch.md)
**Parent phase:** [Phase 0: Walking skeleton, system test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

> **Note:** This file is a deeper, AC-tagged test specification that supersedes and expands the `## Test plan` section in the parent feature file.

## Note on requirement IDs

The feature plan's "Requirements covered" section lists **no** `US-*` or `FR-*` IDs — it states "(no user-story IDs land fully here; US-8 is completed in Phase 2 with the full command set)". Per the workflow rule "a requirement with no tests is a bug — flag it", this is flagged: there are zero requirement IDs to attach tests to.

To keep tests grep-able and traceable, this plan organises tests by **acceptance-criterion ID** (`AC-0.1` … `AC-0.4`, where `0` is the phase number). These IDs are local to this feature and disappear once Phase 2 attaches the real `US-8` ID to the full command set.

The acceptance criteria, restated with IDs:

- **AC-0.1** — `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success.
- **AC-0.2** — `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line.
- **AC-0.3** — Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`.
- **AC-0.4** — No login flow or token handling exists in this phase.

## Coverage matrix

| ID | Description | Unit tests | E2E tests |
|----|-------------|------------|-----------|
| AC-0.1 | `chatd send` writes one text frame and exits 0 | 2 | 1 (smoke) |
| AC-0.2 | `chatd watch` prints each received frame to stdout, one per line | 2 | 1 (smoke) |
| AC-0.3 | Server URL resolves from `--url` flag, then `CHAT_SERVER` env, then default | 3 | 0 |
| AC-0.4 | No auth code path exists in this phase | 1 (negative) | 0 |

E2E coverage for AC-0.1 and AC-0.2 is delegated to the sibling smoke-test feature (`scripts/smoke.sh`), which boots the server, runs two `chatd watch` processes, pipes a message via `chatd send`, and asserts both watchers see it. This plan references that scenario rather than duplicating it.

## Unit tests

### AC-0.1 — `chatd send` writes one text frame and exits 0

- **Name:** `TestAC_0_1_SendWritesSingleTextFrameToWebSocket`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Setup:** spin up an in-process `httptest` WebSocket server that records every frame received.
  - **Asserts:**
    - exactly one text frame is delivered to the server
    - the frame body equals the joined `args` string (e.g. `send hello world` → `"hello world"`)
    - the command returns a nil error (i.e. would exit 0)
    - the WebSocket connection is closed cleanly after the write

- **Name:** `TestAC_0_1_SendReturnsErrorWhenServerUnreachable`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Setup:** point the command at a closed port (`ws://127.0.0.1:1/ws`).
  - **Asserts:**
    - the command returns a non-nil error (i.e. would exit non-zero)
    - the error message mentions the dial failure so a user can diagnose it

### AC-0.2 — `chatd watch` prints each received frame to stdout, one per line

- **Name:** `TestAC_0_2_WatchPrintsEachFrameOnItsOwnLine`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Setup:** fake WebSocket server that pushes three text frames (`"a"`, `"b"`, `"c"`), then closes. Capture stdout via an injectable `io.Writer`.
  - **Asserts:**
    - stdout contains exactly three lines
    - lines are `a`, `b`, `c` in order
    - each line ends with a single `\n` (no doubled newlines, no missing trailing newline on last line)

- **Name:** `TestAC_0_2_WatchExitsCleanlyOnContextCancel`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Setup:** fake WebSocket server that holds the connection open. Cancel the command's `context.Context` (the same path SIGINT uses).
  - **Asserts:**
    - the command returns nil (clean exit, not an error)
    - the WebSocket close handshake is initiated by the client
    - no panic, no goroutine leak (verify via `goleak` or by checking the read loop returned)

### AC-0.3 — Server URL resolves from `--url` flag, then `CHAT_SERVER` env, then default

- **Name:** `TestAC_0_3_URLFlagBeatsEnvAndDefault`
  - **Target file:** `apps/cli/cmd/url_test.go` (or wherever the resolver lives)
  - **Setup:** set `CHAT_SERVER=ws://env.example/ws`, pass `--url=ws://flag.example/ws`.
  - **Asserts:** resolved URL is `ws://flag.example/ws`.

- **Name:** `TestAC_0_3_EnvBeatsDefaultWhenFlagAbsent`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Setup:** set `CHAT_SERVER=ws://env.example/ws`, no `--url`.
  - **Asserts:** resolved URL is `ws://env.example/ws`.

- **Name:** `TestAC_0_3_DefaultUsedWhenNeitherFlagNorEnvSet`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Setup:** unset `CHAT_SERVER`, no `--url`.
  - **Asserts:** resolved URL matches the default pattern `ws://localhost:<port>/ws` for whatever default port the feature picks.

### AC-0.4 — No auth code path exists in this phase

- **Name:** `TestAC_0_4_NoAuthSymbolsReferencedFromCLI`
  - **Target file:** `apps/cli/cmd/no_auth_test.go`
  - **Approach:** static check — walk the `apps/cli` package's imports and string literals.
  - **Asserts:**
    - no import path contains the substring `auth`
    - no source file under `apps/cli` contains the literal `Authorization`, `Bearer `, or `token` (case-insensitive) outside of test files
  - **Why:** AC-0.4 is a negative requirement. A static guard makes the absence enforceable in CI rather than relying on reviewer attention.

## E2E tests

### AC-0.1 + AC-0.2 — end-to-end send/watch round trip

- **Name:** `smoke.sh: send is observed by two watchers`
  - **Target file:** `scripts/smoke.sh` (owned by the sibling smoke-test feature; this plan only references it)
  - **Scenario:**
    1. boot `apps/server` on an ephemeral port
    2. start two `chatd watch` processes against that server
    3. run `chatd send hello-from-smoke`
    4. wait for both watcher stdouts to contain `hello-from-smoke`
  - **Asserts:**
    - both watcher stdouts contain the exact payload, on its own line
    - `chatd send` exit code is 0
    - watchers terminate cleanly when killed

### AC-0.3 — URL resolution end-to-end

No dedicated E2E. The smoke test exercises the `--url` path implicitly (it passes the ephemeral-port URL via `--url`), which is sufficient given the unit-test coverage of the resolver.

### AC-0.4 — absence of auth

No E2E. Negative requirements about absence are best enforced by the static unit test above; an E2E for "did not happen" adds no signal.

## Coverage rules

- Every acceptance-criterion ID has at least one unit test. AC-0.1 and AC-0.2 additionally have E2E coverage via the smoke test.
- Test names start with the AC ID for grep-ability (`grep -r AC_0_1` finds every test for AC-0.1).
- Tests describe behaviour from the acceptance criteria, not implementation — they assert on observable outputs (frames on the wire, lines on stdout, exit codes), not on private helpers.
- When Phase 2 lands `US-8`, the AC IDs in this file should be replaced or supplemented with the real `US-8` ID so the test names trace back to the PRD.
