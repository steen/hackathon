---
feature: ws-userid-binding-and-channel-existence-check
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: Bind userID onto WS connection state + validate channel existence

**Spec:** `specs/plans/phase-1/feature-ws-userid-binding-and-channel-existence-check.md`
**Implementation status:** implemented — `wsCfg.ChannelLookup = repository.ChannelExists` is wired at `apps/server/main.go:104-106`; `repository.ChannelExists` is a single-row helper in `apps/server/internal/repo/channels.go`. The WS handler in `apps/server/internal/wsapi/handler.go` calls the lookup before `websocket.Accept` and binds the redeemed `userID` onto the per-connection state.
**E2E test directory:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | A redeemed ticket's `userID` is stored on a per-connection state value (e.g., a `connState` struct) and reachable from `readLoop` so subsequent message-handling code can attribute the sender. | missing | — |
| AC-2 | On WS upgrade with a `?channel=<id>` query parameter, the handler validates the channel exists via the same `repo.ListChannels`/`repo.GetChannel`-style lookup the REST handlers use. Unknown channel IDs reject the upgrade with HTTP 404 and the standard error envelope. | missing | — |
| AC-3 | For the legacy `#general` and the seeded ULID channels, validation passes. | missing | — |
| AC-4 | The TODO at `apps/server/internal/wsapi/handler.go:148` (`_ = userID`) is removed. | missing | — |
| AC-5 | Existing `apps/server/internal/wsapi/handler_test.go` tests still pass; one new test asserts that a request to `?channel=BAD-CHANNEL-ID` returns 404 with the envelope. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — `userID` reachable from per-connection state (observable via attribution)**
- **What to assert:** Internal `connState` is not visible from outside, but its only purpose is to attribute downstream actions to the user. Concrete external proxy: register `alice` and `bob`. Each gets a ticket. Both dial `/ws?ticket=<own>&channel=#general` → both succeed (101). Open a third observer (`alice` connection works as observer). `POST /api/channels/<general-id>/messages {"body":"from-bob"}` with bob's bearer → assert the broadcast frame received by alice's WS contains `user_id == <bob's user id>`. Same in reverse with alice sending. This proves the binding works end-to-end (the message attribution would be wrong if `userID` weren't stored anywhere). Use `/api/presence` (wired at `main.go:153`) as a secondary check — both ids should appear after both WS open.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/userid_bound_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. `github.com/coder/websocket`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `register`, `login`, `wsTicket`, `dialWSChannel(t, srv, ticket, channel)`, `createChannel`, `sendMessage`, `getPresence`.

**AC-2 — `?channel=<unknown-id>` rejects WS upgrade with 404 envelope**
- **What to assert:** Register + login → ticket. Dial `ws://127.0.0.1:<port>/ws?ticket=<valid>&channel=01ABCDEFGHJKMNPQRSTVWXYZ12` (a 26-char Crockford-base32 string that's not a real channel). `coder/websocket.Dial` returns an error AND the returned `*http.Response` has `StatusCode == 404` AND `Content-Type: application/json...`. Read the response body; decode as the standard envelope; assert `ok == false`, `error.code` is `not_found` (or whatever `errors.go` exposes — verify the constant by reading `errors.go` once; `CodeNotFound = "not_found"` per the file we already read), `error.message != ""`, `data == nil`. Repeat with `?channel=` empty (some impls treat empty as `#general` default; verify by reading `handler.go` once during test authoring). Repeat with a non-ULID-shaped string like `?channel=lol` → expect 404 (not 500) for any non-existent channel id.
- **Layer:** Go (boot binary, raw WS dial).
- **File path:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/channel_404_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `dialWSChannelRaw(t, srv, ticket, channel) (*http.Response, error)`.

**AC-3 — `#general` and seeded/created ULID channels pass validation**
- **What to assert:** Dial `/ws?ticket=<valid>&channel=%23general` → 101. Then `POST /api/channels {"name":"random-channel"}` with bearer → capture the new channel id. Dial `/ws?ticket=<fresh>&channel=<that ulid>` → 101 (a fresh ticket because the ws-ticket is single-use). Then verify a non-existent channel still 404s as in AC-2 (cross-check that the lookup actually does some work; not a no-op).
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/known_channels_pass_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `createChannel`, `dialWSChannel`.

**AC-4 — TODO at `handler.go:148` is removed**
- **What to assert:** This is a source-code hygiene claim, not a runtime behaviour. The E2E proxy: `os.ReadFile("apps/server/internal/wsapi/handler.go")` from the repo root; assert the file does NOT contain the literal substring `_ = userID` AND does NOT contain a TODO comment that mentions `userID` discard. (If the line numbers have moved since the spec was written, the substring match is the durable check.) Skip if the file is absent (would mean a refactor renamed the package, separate concern).
- **Layer:** Go (file read).
- **File path:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/handler_no_todo_test.go`.
- **Setup it needs:** `repoRoot(t)` helper; no server boot.
- **Helpers it can reuse:** harness; `repoRoot(t)`.

**AC-5 — `?channel=BAD-CHANNEL-ID` returns 404 with envelope**
- **What to assert:** Subset of AC-2 with the exact spec wording pinned. Boot, get a ticket, dial `/ws?ticket=<valid>&channel=BAD-CHANNEL-ID`. Assert `*http.Response.StatusCode == 404`. Decode body as envelope; assert `ok == false`, `error.code == "not_found"` (verify), `error.message != ""`. (The duplication with AC-2 is intentional to keep AC-5's literal text testable on its own.)
- **Layer:** Go (boot binary, raw WS dial).
- **File path:** `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/bad_channel_envelope_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `dialWSChannelRaw`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. AC-2/AC-5 need a `dialWSChannelRaw` helper that returns the raw `*http.Response` (not just the connection) so the body can be decoded — `coder/websocket.Dial`'s second return is exactly this; the test must read it before the connection is closed.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/harness_test.go` with copied helpers + `register`, `login`, `wsTicket`, `dialWSChannel`, `dialWSChannelRaw`, `createChannel`, `sendMessage`, `getPresence`, `repoRoot(t)`.
- Add `userid_bound_test.go` (AC-1), `channel_404_test.go` (AC-2), `known_channels_pass_test.go` (AC-3), `handler_no_todo_test.go` (AC-4 — file read, no boot), `bad_channel_envelope_test.go` (AC-5).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
