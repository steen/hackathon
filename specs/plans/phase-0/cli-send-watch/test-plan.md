# Test plan: CLI `chatd send` and `chatd watch` (no auth)

**Feature plan:** [feature-cli-send-watch.md](../feature-cli-send-watch.md)
**Parent phase:** [Phase 0: Walking skeleton, system test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

## Note on requirement IDs

The feature plan carries no `requirement_ids`. The plan itself notes: *"no user-story IDs land fully here; US-8 is completed in Phase 2 with the full command set"*. US-8 (CLI scripter round-trip) is the upstream PRD requirement this feature contributes toward; full coverage of US-8 is owed by the Phase 2 feature work and the Phase 0 smoke-test feature.

Per the test-plan rules, every requirement needs both unit and E2E coverage. Here, the testable units are the four acceptance criteria from the feature plan (AC-1..AC-4), and tests carry those IDs for grep-ability. The end-to-end "two clients exchange a message" behaviour is the contract of the sibling **smoke-test feature** (`scripts/smoke.sh`); E2E rows below cross-reference it rather than duplicating it.

AC-4 is a negative criterion ("no login flow or token handling exists"). It is verified by static absence checks rather than runtime behaviour — flagged here so the asymmetry is explicit.

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E tests |
|----------------|-------------|------------|-----------|
| AC-1 | `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success | 2 | 1 (via smoke-test) |
| AC-2 | `chatd watch` connects to `/ws` and prints every received message to stdout, one per line | 2 | 1 (via smoke-test) |
| AC-3 | Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws` | 4 | 1 |
| AC-4 | No login flow or token handling exists in this phase | 1 (static absence check) | 0 (n/a — negative criterion) |

## Unit tests

### AC-1 — `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success

- **Name:** `TestAC1_SendWritesJoinedArgsAsSingleTextFrame`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Scenario:** start an `httptest.Server` that upgrades to WebSocket and records every frame received; invoke the `send` command function against that URL with `args = ["hello", "world"]`.
  - **Asserts:**
    - exactly one frame is received by the fake server
    - frame opcode is text
    - frame payload equals `"hello world"` (args joined with single spaces)
    - the command function returns nil error (caller maps nil → exit 0)

- **Name:** `TestAC1_SendReturnsErrorWhenServerUnreachable`
  - **Target file:** `apps/cli/cmd/send_test.go`
  - **Scenario:** invoke `send` against a `ws://127.0.0.1:0/ws` URL with no listener.
  - **Asserts:**
    - command function returns a non-nil error
    - error message references the dial failure (so the user sees a real cause, not a silent exit)

### AC-2 — `chatd watch` connects to `/ws` and prints every received message to stdout, one per line

- **Name:** `TestAC2_WatchPrintsEachFrameOnItsOwnLine`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Scenario:** start a fake WS server that, on connect, writes three text frames (`"alpha"`, `"beta"`, `"gamma"`) and then closes the connection. Run `watch` with stdout redirected to a buffer; let it return when the server closes.
  - **Asserts:**
    - buffer contents equal `"alpha\nbeta\ngamma\n"`
    - no extra framing, prefixes, or JSON wrapping in the output (the Phase-0 wire is raw text)

- **Name:** `TestAC2_WatchExitsCleanlyOnContextCancel`
  - **Target file:** `apps/cli/cmd/watch_test.go`
  - **Scenario:** start a fake WS server that holds the connection open and never sends. Run `watch` with a `context.Context` that is cancelled after a short delay (substituting for SIGINT in the test, since `os/signal` is awkward to drive in unit tests).
  - **Asserts:**
    - `watch` returns within a small bounded time after cancellation (e.g. < 1 s)
    - returned error is either nil or `context.Canceled` — not a panic, not a connection-reset error surfaced to the user

### AC-3 — Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`

- **Name:** `TestAC3_ResolveURL_FlagWinsOverEnvAndDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = "ws://flag.example/ws"`, `env = "ws://env.example/ws"`.
  - **Asserts:** returns `"ws://flag.example/ws"`.

- **Name:** `TestAC3_ResolveURL_EnvWinsOverDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = ""`, `env = "ws://env.example/ws"`.
  - **Asserts:** returns `"ws://env.example/ws"`.

- **Name:** `TestAC3_ResolveURL_FallsBackToLocalhostDefault`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = ""`, `env = ""`.
  - **Asserts:**
    - returns a `ws://localhost:<port>/ws` URL
    - port matches the project default (the same port the Phase-0 server binds to; pulled from a shared constant rather than hardcoded twice)

- **Name:** `TestAC3_ResolveURL_RejectsEmptyAfterTrim`
  - **Target file:** `apps/cli/cmd/url_test.go`
  - **Scenario:** call the URL-resolution helper with `flag = "   "`, `env = ""` (whitespace-only flag).
  - **Asserts:** returns the default URL — does not produce `ws://   /ws` or a malformed dial target.

### AC-4 — No login flow or token handling exists in this phase

- **Name:** `TestAC4_CLISourceContainsNoAuthOrTokenSymbols`
  - **Target file:** `apps/cli/cmd/no_auth_test.go`
  - **Scenario:** walk all `.go` files under `apps/cli/`, read each, and search for token/auth-related identifiers introduced only in later phases.
  - **Asserts:**
    - no occurrence of identifiers `Login`, `Logout`, `Register`, `Token`, `Bearer`, `JWT`, `bcrypt`, `password` in the CLI source tree (case-insensitive, identifier-bounded so substrings like `tokens` in unrelated comments don't false-positive — the test is meant to fail loudly if Phase 1/2 work leaks back into Phase 0).
    - no import of `apps/server/internal/auth` or any future `packages/go-client` auth module.
  - **Why this is a unit test, not a runtime check:** AC-4 forbids the *existence* of behaviour, not a particular runtime outcome. The cheapest faithful test is a static scan of the CLI source.

## E2E tests

The end-to-end "send produces output on watch" round-trip is the contract of the sibling **smoke-test feature** (`scripts/smoke.sh`), which boots the server, runs two `chatd watch` processes, pipes a message via `chatd send`, and asserts both watchers see it. Cross-references below avoid duplicating that work; AC-3 gets its own E2E because URL resolution is not exercised by the smoke flow (which uses the default).

### AC-1 — `chatd send` end-to-end

- **Cross-reference:** the smoke-test feature's `scripts/smoke.sh`.
  - **Coverage:** running `chatd send "<msg>"` against the real server causes both attached `chatd watch` processes to print `<msg>` and `send` exits 0.
  - **Owned by:** the smoke-test feature plan in this same phase. This test plan does not duplicate the script.

### AC-2 — `chatd watch` end-to-end

- **Cross-reference:** the smoke-test feature's `scripts/smoke.sh`.
  - **Coverage:** `chatd watch` connected to the real server prints each broadcast message to stdout, one per line, and is signal-terminable.
  - **Owned by:** the smoke-test feature plan in this same phase.

### AC-3 — URL configuration end-to-end

- **Name:** `TestAC3_E2E_URLFlagAndEnvDriveDial`
  - **Target file:** `apps/cli/cmd/url_e2e_test.go`
  - **Scenario:** spin up two `httptest` WS servers on different ports (call them A and B). Build (or invoke as a function) the `chatd send` entrypoint three times:
    1. with `--url=ws://A/ws` and `CHAT_SERVER=ws://B/ws` — A must receive, B must not.
    2. with no `--url` and `CHAT_SERVER=ws://B/ws` — B must receive, A must not.
    3. with no `--url` and no `CHAT_SERVER` — neither test server receives; the dial targets the default `ws://localhost:<default-port>/ws` (which has no listener), and `send` returns a dial error.
  - **Asserts:**
    - case 1: server A records exactly one frame; server B records zero.
    - case 2: server B records exactly one frame; server A records zero.
    - case 3: command returns a non-nil error referencing the dial failure.

## Coverage rules
- Every acceptance criterion (AC-1..AC-4) has at least one unit test. AC-1, AC-2, and AC-3 also have E2E coverage; AC-2 and AC-1's E2E rows defer to the smoke-test feature, and AC-3 gets a dedicated E2E because the smoke flow does not exercise URL overrides.
- AC-4 has no E2E test — it is a negative existence criterion verified statically. Flagged in the matrix and explained at the test definition.
- Test names start with the AC ID for grep-ability.
- Tests describe behaviour from the acceptance criteria, not implementation details of the WebSocket library or `cobra` wiring.
