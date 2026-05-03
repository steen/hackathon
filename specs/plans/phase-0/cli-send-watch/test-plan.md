# Test plan: CLI `chatd send` and `chatd watch` (no auth)

**Feature plan:** [feature-cli-send-watch.md](../feature-cli-send-watch.md)
**Parent phase:** [Phase 0: Walking skeleton, system test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

## Note on requirement IDs

US-8 is the only requirement ID this Phase-0 feature contributes toward; the feature plan itself records that US-8 only lands *fully* in Phase 2 with the complete command set. Phase 0 ships the `send` / `watch` slice that the PRD's smoke-test check (US-8 row in §11) exercises end-to-end. Tests below carry the `US8` prefix per the workflow rule, even though several of them target acceptance criteria that are sub-behaviours of US-8 in this phase.

The end-to-end "two clients exchange a message" round-trip is the contract of the sibling **smoke-test feature** (`scripts/smoke.sh`) in this same phase. The E2E rows for the round-trip cross-reference that script rather than duplicate it; one dedicated E2E (`URL configuration`) lives here because the smoke flow does not exercise URL overrides.

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E tests |
|----------------|-------------|------------|-----------|
| US-8 | As a scripter, I want a CLI command, so I can pipe automated notifications into chat. | 9 | 3 (1 dedicated + 2 via smoke-test feature) |

## Unit tests

### US-8 — As a scripter, I want a CLI command, so I can pipe automated notifications into chat.

#### `send` behaviour (acceptance criterion 1)

- **Name:** `TestUS8_SendWritesJoinedArgsAsSingleTextFrame`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Scenario:** start an `httptest.Server` that upgrades to WebSocket and records every frame received; invoke the `send` command function against that URL with `args = ["hello", "world"]`.
  - **Asserts:**
    - exactly one frame is received by the fake server
    - frame opcode is text
    - frame payload equals `"hello world"` (positional args joined with single spaces)
    - the command function returns nil error (caller maps nil → exit 0)

- **Name:** `TestUS8_SendReturnsErrorWhenServerUnreachable`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Scenario:** invoke `send` against a `ws://127.0.0.1:0/ws` URL with no listener.
  - **Asserts:**
    - command function returns a non-nil error
    - error message references the dial failure so a scripter can diagnose without strace

#### `watch` behaviour (acceptance criterion 2)

- **Name:** `TestUS8_WatchPrintsEachFrameOnItsOwnLine`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Scenario:** start a fake WS server that, on connect, writes three text frames (`"alpha"`, `"beta"`, `"gamma"`) and then closes the connection. Run `watch` with stdout redirected to a buffer; let it return when the server closes.
  - **Asserts:**
    - buffer contents equal `"alpha\nbeta\ngamma\n"`
    - no extra framing, prefixes, or JSON wrapping in the output (the Phase-0 wire is raw text)

- **Name:** `TestUS8_WatchExitsCleanlyOnContextCancel`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Scenario:** start a fake WS server that holds the connection open and never sends. Run `watch` with a `context.Context` cancelled after a short delay (substituting for SIGINT — `os/signal` is awkward to drive in unit tests).
  - **Asserts:**
    - `watch` returns within a small bounded time after cancellation (e.g. < 1 s)
    - returned error is either nil or `context.Canceled` — not a panic, not a connection-reset error surfaced to the user

#### URL resolution (acceptance criterion 3)

- **Name:** `TestUS8_ResolveURL_FlagWinsOverEnvAndDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = "ws://flag.example/ws"`, `env = "ws://env.example/ws"`.
  - **Asserts:** returns `"ws://flag.example/ws"`.

- **Name:** `TestUS8_ResolveURL_EnvWinsOverDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = ""`, `env = "ws://env.example/ws"`.
  - **Asserts:** returns `"ws://env.example/ws"`.

- **Name:** `TestUS8_ResolveURL_FallsBackToLocalhostDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = ""`, `env = ""`.
  - **Asserts:**
    - returns a `ws://localhost:<port>/ws` URL
    - port matches the project default (pulled from a shared constant rather than hardcoded twice)

- **Name:** `TestUS8_ResolveURL_TreatsWhitespaceFlagAsUnset`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = "   "`, `env = ""` (whitespace-only flag).
  - **Asserts:** returns the default URL — does not produce `ws://   /ws` or a malformed dial target.

#### No-auth scope guard (acceptance criterion 4)

- **Name:** `TestUS8_CLISourceContainsNoAuthOrTokenSymbols`
  - **Target file:** `apps/cli/cmd/no_auth_test.go`
  - **Scenario:** walk all `.go` files under `apps/cli/`, read each, and search for token/auth-related identifiers introduced only in later phases.
  - **Asserts:**
    - no occurrence of identifiers `Login`, `Logout`, `Register`, `Token`, `Bearer`, `JWT`, `bcrypt`, `password` in the CLI source tree (identifier-bounded match — substrings inside unrelated words must not false-positive)
    - no import of `apps/server/internal/auth` or any future `packages/go-client` auth module
  - **Why this is a unit test, not a runtime check:** acceptance criterion 4 forbids the *existence* of auth code in this phase, not a particular runtime outcome. The cheapest faithful test is a static scan of the CLI source. This test must fail loudly if Phase-1/2 work leaks back into Phase-0 files.

## E2E tests

### US-8 — As a scripter, I want a CLI command, so I can pipe automated notifications into chat.

- **Name:** *(see cross-reference)* — **`scripts/smoke.sh` covers `chatd send` round-trip**
  - **Target file:** `scripts/smoke.sh` (owned by the sibling smoke-test feature in this same phase)
  - **Scenario:** boot the real `apps/server` binary, run two `chatd watch` processes against the real `/ws`, pipe a message via `chatd send`, assert both watchers see it.
  - **Asserts:**
    - `chatd send` exits 0
    - both watchers print the sent payload, one per line, within a short bounded time

- **Name:** *(see cross-reference)* — **`scripts/smoke.sh` covers `chatd watch` end-to-end**
  - **Target file:** `scripts/smoke.sh`
  - **Scenario:** as above; the same script exercises `watch` against the real server.
  - **Asserts:** watchers print received frames to stdout and are tear-down-clean.

- **Name:** `TestUS8_E2E_URLFlagAndEnvDriveDial`
  - **Target file:** `apps/cli/cmd/url_e2e_test.go`
  - **Scenario:** spin up two `httptest` WS servers on different ports (call them A and B). Invoke the `chatd send` entrypoint three times:
    1. with `--url=ws://A/ws` and `CHAT_SERVER=ws://B/ws` — A must receive, B must not.
    2. with no `--url` and `CHAT_SERVER=ws://B/ws` — B must receive, A must not.
    3. with no `--url` and no `CHAT_SERVER` — neither test server receives; the dial targets the default `ws://localhost:<default-port>/ws` (no listener) and `send` returns a dial error.
  - **Asserts:**
    - case 1: server A records exactly one frame; server B records zero
    - case 2: server B records exactly one frame; server A records zero
    - case 3: command returns a non-nil error referencing the dial failure
  - **Why a dedicated E2E:** the smoke-test feature uses defaults, so `--url` and `CHAT_SERVER` precedence is otherwise unverified end-to-end.

## Coverage rules
- US-8 has unit tests across all four acceptance criteria and E2E coverage via the smoke-test feature plus a dedicated URL-precedence E2E.
- All test names start with `US8_` for grep-ability.
- Tests describe behaviour from the acceptance criteria, not implementation details of the WebSocket library or `cobra` wiring.
- The `no auth in this phase` criterion is verified statically (existence check); flagged explicitly above so the asymmetry is not mistaken for missing coverage.
