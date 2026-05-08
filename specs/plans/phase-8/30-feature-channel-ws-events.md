# Feature: Channel WebSocket events (`channel` outbound frame)

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

Today the WS surface emits `message`, `presence`, and `error` frames only. After Phase 8, when one user creates or renames a channel, every connected client must update their channel list without polling — otherwise client A sees `#general` and `#offtopic` while client B (which created `#new` ten seconds ago) sees the third channel and A does not until they refresh.

The PRD post-#835 introduces a fourth outbound frame:

```json
{ "type": "channel", "data": { "kind": "create" | "rename", "channel": { "id", "name", "created_at" } } }
```

Inbound channel frames remain forbidden — the sender-spoofing rule from PRD §"Design deviations" (`92d447f`) is unchanged. Clients never write channel events into the WS.

## Goal

Add the `channel` frame to the outbound wire and a `BroadcastChannel(kind, ch)` entry point on the hub. The endpoint features (`10-feature-channel-rename-endpoint.md` and the existing `POST /api/channels` handler) call into this on success.

## Approach

1. Hub: add `BroadcastChannel(kind ChannelEventKind, ch Channel)` to `apps/server/internal/hub/hub.go`. The hub today is channel-scoped for `message` broadcasts; this method must broadcast to **every** connected client regardless of their subscribed channel ID, because channel listings are a global concern. Use a separate fan-out path; do not reuse the per-channel `subscribers[channelID]` map.
2. Wire shape: define a `ChannelEvent` struct in the hub package and matching JSON tags in `packages/go-client/ws.go`, plus a TS interface in `packages/api-client/src/types.ts` (the cross-language types are spec'd in `80-feature-clients-channel-extensions.md`; this feature lands the server side and the schema).
3. POST integration: extend the existing `POST /api/channels` handler to call `hub.BroadcastChannel("create", ch)` on success. `kind = "create"` is emitted exactly once per successful insert.
4. PATCH integration: the rename handler from `10-feature-channel-rename-endpoint.md` calls `hub.BroadcastChannel("rename", ch)` with the post-rename channel.
5. Connection handling: the hub must tolerate slow consumers without dropping. Reuse the bounded-write-channel pattern that already exists for `message` broadcast (do not introduce a second pattern). On a slow consumer, drop the frame for that connection and log a warning at level `warn`; do not close the connection.
6. Frame ordering: a `channel` frame for a rename MUST be emitted after the DB transaction commits and before the HTTP 200 returns. Document this in code comments — the order matters for tests that PATCH then immediately query `GET /api/channels` from a second client.

## Acceptance criteria

- `POST /api/channels` success emits exactly one `{ type: "channel", data: { kind: "create", channel: {...} } }` frame to every connected WS client.
- `PATCH /api/channels/{id}` success emits exactly one `{ type: "channel", data: { kind: "rename", channel: {...} } }` frame to every connected WS client.
- A WS connection scoped to channel `A` still receives `channel` frames about channel `B`.
- A connection with a slow consumer drops `channel` frames for itself but does not close, and other connections continue to receive.
- Inbound `channel`-typed frames are still rejected (the existing inbound-frame rejection covers this; this feature must include a regression test).
- Failed channel writes (400 / 403 / 409 / 429) emit zero `channel` frames.

## Out of scope

- Channel deletion frames — channels are not deletable in MVP.
- Per-frame sequence numbers / replay on reconnect — clients re-fetch `GET /api/channels` on reconnect (see `50-feature-web-shared-ws-provider.md`).
- Filtering frames by visibility — all channels are visible to all authenticated users (PRD §9, no per-channel ACLs in MVP).

## Pointers

- `apps/server/internal/hub/hub.go` — existing per-channel broadcast; mirror its bounded-channel write pattern.
- `apps/server/internal/wsapi/handler.go` — outbound frame writer; the `channel` type registers here.
- PRD §10 (WebSocket subsection) — wire contract.
- `packages/go-client/ws.go` and `packages/api-client/src/types.ts` — `sync with <counterpart>` pair to extend (handled in `80-feature-clients-channel-extensions.md`).
