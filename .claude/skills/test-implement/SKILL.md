---
name: test-implement
description: Read findings from `specs/test-analysis/` and write end-to-end tests under `tests/e2e/<phase>/<feature-slug>/` that drive the production system black-box. Tests boot real binaries on random ports with random secrets and exercise behavior via real HTTP/WS clients. Skipped tests for `deferred` ACs whose impl doesn't exist yet. Invoked by `/test-watch`, also runnable standalone.
user-invocable: true
allowed-tools: [Read, Write, Edit, Bash, Glob, Grep]
---

# /test-implement — write end-to-end tests for one feature

You read the findings produced by `/test-analyze` and write real E2E tests that drive the production system end-to-end. You do NOT modify production code under `apps/**` or `packages/**`.

The agent's tests live exclusively under `tests/e2e/<phase>/<feature-slug>/`. Do not touch any other test directory (`tests/scaffold/`, `tests/smoke-test/`, `tests/server-ws-hub/`, `tests/monorepo-scaffold/`, in-package `*_test.*`) — those belong to scaffold maintenance and feature authors respectively.

## Arguments

`$ARGUMENTS` is a free-form instruction from the orchestrator. Expected to provide:
- `$ROOT` — worktree path (defaults to current working directory).
- `$FEATURE` — `<phase>/<slug>` of the single feature to implement tests for this tick. Required. If absent (standalone invocation), pick the feature with the most missing ACs from `$ROOT/specs/test-analysis/`.

You implement tests for **one feature per invocation**. PRs stay reviewable and a single failing test doesn't block others.

## Inputs

- Findings doc for the chosen feature: `$ROOT/specs/test-analysis/<phase>/<slug>.md`. Read its `## Findings` section — each missing/deferred AC has a paragraph telling you what to assert, the layer, the file path, the setup, and reusable helpers.
- Spec: `$ROOT/specs/plans/<phase>/feature-<slug>.md`. Re-read the AC text verbatim — the test name and a leading comment must quote it.
- Existing E2E tests for this feature: `$ROOT/tests/e2e/<phase>/<slug>/**`. Read them to find helpers and avoid duplication.
- Production code: `$ROOT/apps/**`, `$ROOT/packages/**`. Read to know the real HTTP routes, WS frame shapes, env vars, and binary names you need to drive.
- Reference E2E pattern (read once, copy idioms from): `$ROOT/tests/server-ws-hub/hub_test.go`. This is the gold-standard shape — build the binary in `t.TempDir()`, pick a free port, generate random secrets via `crypto/rand`, launch with `exec.CommandContext`, dial via real `coder/websocket`, assert on observable behavior.

## What "end-to-end" means here

An E2E test boots the production binary (or binaries) and drives it through its real network surface. No mocks of production code. No imports of internal packages from `apps/**`. Acceptable couplings:
- Build the server: `go build -o "$tmpDir/chat-server" ./apps/server`.
- Build the CLI: `go build -o "$tmpDir/chatd" ./apps/cli`.
- Pick a free port via `net.Listen("tcp", "127.0.0.1:0")` (Go) or `python3 -c "import socket; ..."` (bash).
- Generate `CHAT_JWT_SECRET` (≥32 hex chars) and `CHAT_INVITE_CODE` (≥8 hex chars) per-test via `crypto/rand`. Never commit fake-secret literals — see CLAUDE.md "No hardcoded secrets".
- Set `CHAT_DB_PATH` to `t.TempDir()/chatd.sqlite` so each test has a fresh DB.
- Drive REST via `net/http` or fetch; drive WS via `github.com/coder/websocket` or the browser's WebSocket API in a vitest browser env.
- Read observable behavior only: HTTP status + body, WS frames received, files written under the tempdir, exit code of the CLI subprocess.

If an AC genuinely needs a TS-side test (e.g. a workspace package's published surface, or a vitest-driven web app), use vitest. For chat-server / CLI ACs, default to Go — the server is Go and the binary contract is most directly tested from Go.

## Decision: which test layer?

For each missing AC:

| AC concerns | Layer | Where the test goes |
|-------------|-------|---------------------|
| Server HTTP / WS / persistence | Go | `tests/e2e/<phase>/<slug>/<area>_test.go` (package `<slug>_e2e_test`) |
| CLI behavior (chatd subcommands, exit codes, stdout) | Go | `tests/e2e/<phase>/<slug>/<area>_test.go` (boot server + exec chatd binary) |
| TS workspace package public API surface | vitest | `tests/e2e/<phase>/<slug>/<area>.test.ts` |
| Web app via headless browser | vitest browser mode | `tests/e2e/<phase>/<slug>/<area>.test.ts` (only if findings explicitly call for it) |

If the findings doc specifies a layer, follow it. If both Go and TS could test the same AC, prefer Go for server-touching behavior.

## File and identifier conventions

- Per-feature test directory: `$ROOT/tests/e2e/<phase>/<feature-slug>/`. Create it on the first test for a feature.
- Go test file: `<area>_test.go` (e.g. `auth_test.go`, `presence_test.go`). Package name: `<feature_slug_with_underscores>_e2e_test`.
- Go test function: `TestACN_<CamelCaseShortName>` — the literal `ACN` (no hyphen between AC and N is fine for Go identifiers, but the leading comment must contain `AC-N` with the hyphen so `/test-analyze` can grep it).
- vitest test file: `<area>.test.ts`. Use `describe("AC-N: <verbatim AC>", () => { it("AC-N: <same or refinement>", () => { ... }) })`.
- Every test must include the literal token `AC-N` (with hyphen) somewhere visible to `grep`: function name, describe string, or a leading comment `// AC-N: <verbatim AC text>`. This is what `/test-analyze` looks for on the next run.

## Helpers

For the first test in a feature directory, define helpers locally in the test file (or in a sibling `harness_test.go` in the same directory):
- `startServer(t *testing.T) *runningServer` — builds, launches, waits for readiness, registers `t.Cleanup` for shutdown.
- `randomSecret(t *testing.T, byteLen int) string` — `crypto/rand` → hex.
- `freePort(t *testing.T) int` — `net.Listen("tcp", "127.0.0.1:0")`.
- `register(t, srv, username, password) (token string)` and `login(t, srv, ...) string` — once register/login E2E exists, lift these into `harness_test.go` so subsequent tests don't redo the dance.
- `mintTicket(t, srv, token) string`, `dialWS(t, srv, ticket, channelID) *websocket.Conn` — for any test that needs an authenticated WS subscriber.

Helpers live next to the tests that use them. Do **not** introduce a shared package under `tests/e2e/internal/` — that's a premature abstraction. Per CLAUDE.md: no new shared abstractions until 3+ call sites across distinct features need it. If you do hit that bar, surface it in chat output rather than introducing it silently.

Reuse the helper shape from `tests/server-ws-hub/hub_test.go` — copy the function bodies into the new file (don't import them from the existing test package; that file's package name is `server_ws_hub_test` and its helpers are intentionally local).

## Deferred ACs

For ACs whose findings status is `deferred` (impl is `stub`):
- Still write the test, but mark it skipped with a clear reason:
  - Go: `t.Skip("AC-N: deferred — apps/<x> is stub at this SHA; un-skip when <concrete file> exists")`
  - vitest: `it.skip("AC-N: …", () => { /* deferred — apps/<x> is stub */ })`
- The skipped test is a positive signal: the AC is on record, the harness is wired, and the next person who lands the impl just removes the skip.

## Partial ACs

Findings flagged `partial` mean an existing E2E test contains the `AC-N` token but doesn't fully exercise the AC (e.g. asserts only the happy path, or is currently `t.Skip`'d while impl is live). Either:
- Tighten the existing test in place — add the missing assertion.
- Add a sibling test covering the gap, with `// AC-N (gap): <what this covers that the sibling doesn't>` in a leading comment.

Don't duplicate a test that already passes the AC.

## Secrets and fixtures

- Never commit a fixed JWT secret or invite code. Generate them per test from `crypto/rand`.
- Never use credentials of the form that look real (e.g. `super-secret-key-do-not-share`). Use `randomSecret(t, 32)` and let it produce hex. If a test fixture needs a placeholder string for a non-secret field, prefix it with `test-` and keep it obviously fake.
- Per CLAUDE.md: if you spot a hardcoded secret while reading existing code, surface it in the chat output — do not silently fix it (that's a separate PR).

## After writing

Run the relevant test commands inside `$ROOT` to verify:

```
cd "$ROOT" && go test ./tests/e2e/...
cd "$ROOT" && pnpm install --frozen-lockfile && pnpm -r --if-present test
```

If `pnpm` is not on PATH, use `~/.npm-global/bin/pnpm`.

For each newly written test, the expected outcome is:
- **Skipped** if AC is deferred (test runs but is marked skipped).
- **Passing** if AC is live and the production code already satisfies the AC.
- **Failing** if AC is live and the production code does NOT satisfy it. **This is a real signal — the agent has discovered a spec/impl gap. Leave the test failing. Do not modify production code to make it pass.** Surface the failure in the chat output and append a `## Test run failures` section to the findings doc with the failure log + a one-line interpretation.

## What to return to the caller

Emit one chat line in this exact format so `/test-watch` can parse:

```
test-implement: feature=<phase>/<slug> written=<N> skipped=<S> passing=<P> failing=<F>
```

Plus, for each failing test, one extra line:

```
failing: <test path>::<test name> — <one-line reason from output>
```

If the feature had no missing/deferred ACs left to write (the orchestrator picked a feature with nothing to do), emit:

```
test-implement: feature=<phase>/<slug> written=0 — no missing or deferred ACs
```

…and exit 0 with no commit.

## Things you must NOT do

- Do not edit anything under `apps/**` or `packages/**`. Tests only.
- Do not modify tests outside `tests/e2e/<phase>/<feature-slug>/` — leave `tests/scaffold/`, `tests/smoke-test/`, `tests/server-ws-hub/`, `tests/monorepo-scaffold/`, and in-package tests strictly alone.
- Do not import internal packages from `apps/**` or `packages/**` into the E2E test (use `import _ "...";` only if you need a build-side check, never to call internals). The whole point of E2E is to test through the public network surface.
- Do not mock production code. If a test needs a fake upstream (rare), implement it as an `httptest.Server` in the test file.
- Do not delete or rewrite tests that `/test-analyze` marked as `covered`.
- Do not add new npm or Go dependencies without surfacing them in chat first.
- Do not commit fixed JWT secrets, invite codes, or other credentials. Generate per-test via `crypto/rand`.
- Do not work on more than one feature in a single invocation. One feature = one PR.
- Do not silently skip a failing test to make CI green. Failing E2E tests are the point.
