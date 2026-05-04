---
feature: channels-and-messages
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 6
covered: 0
partial: 0
missing: 6
deferred: 0
---

# E2E test analysis: Channels and messages endpoints (REST + WS)

**Spec:** `specs/plans/phase-1/feature-channels-and-messages.md`
**Implementation status:** implemented — repo helpers in `apps/server/internal/repo/channels.go` and `apps/server/internal/repo/messages.go`; HTTP handlers in `apps/server/internal/http/channels_handlers.go` and `apps/server/internal/http/messages_handlers.go`; routes wired by `ch.Routes(mux, require, msg)` at `apps/server/main.go:147`. Hub broadcast wired through `httpapi.MessagesDeps{Hub: h}`.
**E2E test directory:** `tests/e2e/phase-1/channels-and-messages/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `GET /api/channels` returns the list of channels (US-3). | missing | — |
| AC-2 | `POST /api/channels` with `{name}` creates a channel and returns it (US-4); rejects duplicate or invalid names. | missing | — |
| AC-3 | `GET /api/channels/{id}/messages?before=&limit=` returns prior messages, newest-first, paginated (US-6). | missing | — |
| AC-4 | `POST /api/channels/{id}/messages` persists a message and broadcasts it to WS subscribers of that channel (US-5). | missing | — |
| AC-5 | WS clients receive new-message events in real time, with author + timestamp + body (US-5). | missing | — |
| AC-6 | All endpoints require authentication (bearer token via REST, ticket-redeemed JWT for WS). | missing | — |

## Findings

### Missing E2E tests

**AC-1 — `GET /api/channels` returns the channel list**
- **What to assert:** Boot the server, register + login, `GET /api/channels` with bearer → 200 envelope `{ok:true, data:{channels:[...]} (or {data:[...]}; verify by reading `channels_handlers.go` once during test authoring), error:null}`. Assert the list is a JSON array. If the schema seeds a default `#general` channel, assert its presence by name. After `POST /api/channels {"name":"random-x"}`, a follow-up `GET /api/channels` includes a row whose `name == "random-x"`.
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/channels-and-messages/list_channels_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `register`, `login`, `createChannel(t,srv,tok,name)`, `listChannels(t,srv,tok)`.

**AC-2 — `POST /api/channels` creates; rejects duplicate or invalid names**
- **What to assert:** With bearer, `POST /api/channels {"name":"engineering"}` → 200/201 envelope `{ok:true, data:{channel:{id:"<ULID>", name:"engineering", ...}}}`; assert id is 26 chars, name matches. Repeat the same POST → assert 4xx envelope (likely 409) with `error.code` indicating conflict (verify exact code string by reading `channels_handlers.go` and `errors.go` — the constant `CodeConflict` exists). Try invalid names: empty string, name with whitespace, name longer than the documented cap (e.g. 64+ chars), name with `/` or `#` (the channel-name-shape rule). Each → 4xx envelope with `bad_request` (or whatever `errors.go` exposes). Open SQLite read-only and assert no row exists for the rejected names.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/channels-and-messages/create_channel_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `createChannelRaw(t,srv,tok,name) (status int, body []byte)`; `openDBReadOnly`.

**AC-3 — `GET /api/channels/{id}/messages?before=&limit=` paginates newest-first**
- **What to assert:** Create a channel, send 75 messages via `POST /api/channels/{id}/messages` with bodies `msg-001` through `msg-075`. `GET /api/channels/{id}/messages` (no params) → 200 envelope; assert `data` is an array of 50 messages (default limit per spec) ordered newest-first: index 0 has body `msg-075`, index 49 has body `msg-026`. `GET .../messages?limit=200` → assert all 75 messages, newest-first. `GET .../messages?limit=300` → assert at most 200 are returned (max-cap clamp). `GET .../messages?limit=10&before=<id-of-msg-040>` → assert 10 messages with body `msg-039` through `msg-030`. `GET .../messages?limit=10&before=<id-of-msg-001>` → assert empty array (nothing older).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/channels-and-messages/list_messages_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `sendMessage(t,srv,tok,chID,body) message`, `listMessages(t,srv,tok,chID,opts)`.

**AC-4 — `POST .../messages` persists and broadcasts to WS subscribers**
- **What to assert:** Create a channel `c1`. Open a WS connection bound to `c1` (the harness must figure out how — the gold-standard test uses `?channel=#general`; the new channel handler reads `wsCfg.ChannelLookup`, see `main.go` line 105). Subscribe a second WS observer to `c1`. `POST /api/channels/{id}/messages {"body":"hello"}` with bearer → 200 envelope. Wait up to 2s for both WS observers to receive a frame; assert the frame's JSON contains `body:"hello"`, `channel_id:"<c1>"`, `user_id:"<sender>"`, `created_at` (RFC 3339), and `id` (ULID). Open SQLite read-only; `SELECT * FROM messages WHERE channel_id='<c1>'` → assert exactly one row with matching body and id. (If the post-audit-#78 inbound WS dropping is still in effect the broadcast path is server-originated by the REST handler — that's exactly what this AC tests.)
- **Layer:** Go (boot binary, HTTP + WS, sqlite read).
- **File path:** `tests/e2e/phase-1/channels-and-messages/post_message_broadcasts_test.go`.
- **Setup it needs:** same as AC-1; `github.com/coder/websocket`.
- **Helpers it can reuse:** harness; `wsTicket`, `dialWSChannel(t,srv,ticket,channelID)`, `sendMessage`, `openDBReadOnly`.

**AC-5 — WS new-message frames carry author + timestamp + body**
- **What to assert:** Same setup as AC-4. After receiving a broadcast frame, parse it as JSON; assert it has all three fields with correct types: `user_id` is a 26-char ULID matching the sender, `created_at` parses with `time.Parse(time.RFC3339Nano, v)` and is within 5 seconds of test wall clock, `body` matches the POSTed body byte-for-byte. Try with two messages from two different users; assert each subscriber sees both frames in send order with the correct `user_id` per frame.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/channels-and-messages/ws_new_message_event_test.go`.
- **Setup it needs:** same as AC-4.
- **Helpers it can reuse:** same as AC-4.

**AC-6 — all endpoints require authentication**
- **What to assert:** For each of `GET /api/channels`, `POST /api/channels`, `GET /api/channels/<known-id>/messages`, `POST /api/channels/<known-id>/messages`: send the request with no `Authorization` header → assert 401 envelope `{ok:false, error:{code:"unauthorized", ...}}`; with a garbage bearer → 401; with a tampered bearer → 401. For WS: dial `/ws` with no `?ticket=` → not 101 (likely 401); dial with a garbage ticket → not 101 (likely 401); dial with a redeemed-once ticket → not 101 (single-use).
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/channels-and-messages/auth_required_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. The harness needs a richer surface than the gold-standard test because every assertion goes through the auth handlers — extend with `register`, `login`, `wsTicket`, `dialWSChannel`, `createChannel`, `sendMessage`, `listMessages`, `listChannels`, `openDBReadOnly`.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/channels-and-messages/harness_test.go` with copied helpers + the full HTTP helper bundle listed above.
- Add `list_channels_test.go` (AC-1), `create_channel_test.go` (AC-2), `list_messages_test.go` (AC-3), `post_message_broadcasts_test.go` (AC-4), `ws_new_message_event_test.go` (AC-5), `auth_required_test.go` (AC-6).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
