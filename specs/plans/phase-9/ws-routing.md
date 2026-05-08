# Phase 9 — WS routing: multi-topic subscription model

**Parent epic:** #860
**Decision log:** `lt -p direct-messages 3` §4, §10, L15, C2
**Cross-links:** [README.md](README.md) · [dms.md](dms.md) · [read-state.md](read-state.md)

This file is the contract for how the Phase 9 WS endpoint subscribes to Hub topics and how server-side emitters route DM and read frames.

## Pre-Phase-9 baseline (verified at HEAD `0742b0b`)

Today, every authenticated WS connection subscribes to **exactly one** Hub topic for its lifetime — the channel topic, supplied via `?channel=<id>` on the `/ws` upgrade. The `apps/server/internal/wsapi/handler.go` Handler:

- Returns HTTP 400 if `?channel=` is missing **and** `cfg.ChannelLookup` is wired (the production path).
- Returns HTTP 404 if the supplied channel does not exist (channel-existence check runs before ws-ticket redemption — see audit #78).
- Calls `Hub.Subscribe(channel, sub)` once and `Hub.Unsubscribe(channel, sub)` on disconnect.

Channel frames (`{type:"channel"}`) bypass per-channel subscription via `Hub.BroadcastAll` — every connection across every channel receives them. No other broadcast bypass exists today.

Hub keys are opaque strings: `Subscribe(topic string, s Subscriber)` does not interpret the string. So the multi-topic extension below requires no change to the Hub itself — only to the WS upgrade handler and to the new emitters.

## Phase 9 contract

### Subscription rule

Every authenticated WS connection subscribes to **two** Hub topics for its lifetime:

1. The channel topic — same string the connection already subscribes to.
2. The user-inbox topic — `user:<viewer>`, where `<viewer>` is the authenticated user id from the redeemed ws-ticket.

```go
// apps/server/internal/wsapi/handler.go (Phase 9 shape, indicative)
sub := newConnSubscriber(userID, channel)

h.Subscribe(channel, sub)
defer h.Unsubscribe(channel, sub)

userTopic := "user:" + userID
h.Subscribe(userTopic, sub)
defer h.Unsubscribe(userTopic, sub)
```

The user-inbox topic is registered AFTER the channel topic so that an unauthenticated connection (impossible today since ws-ticket is required, but defensive) cannot end up subscribed to a `user:` topic with an empty id.

### `?channel=` parameter — optional under legacy default (L15)

`?channel=` becomes optional on the `/ws` upgrade:

- **Present, valid:** subscribe to that channel topic + `user:<viewer>`. Existing behavior preserved.
- **Absent:** subscribe to the legacy `defaultChannel = "#general"` channel topic + `user:<viewer>`. This preserves current go-client / CLI / `chatd watch` behavior — those callers historically passed empty `?channel=` and the server returned 400; under L15 the empty / absent path now resolves to `#general` so they don't regress.
- **Present, unknown id:** continue to return HTTP 404 BEFORE the WebSocket handshake (existing rule preserved).

The `defaultChannel = "#general"` lookup happens inside the upgrade handler via the existing `cfg.ChannelLookup` callback (or a new dedicated `cfg.DefaultChannelResolver` — sub-issue W picks the implementation shape). The lookup runs once per connection at upgrade; failures fall back to HTTP 500 the same way the existing channel lookup does.

**Known mismatch surfaced from README.md verification:** at `0742b0b` the production `/ws` rejects empty `?channel=` with HTTP 400. The W sub-issue is responsible for landing the L15 default-channel fallback OR flipping L15. This contract commits to the lookup-fallback path; if W proposes flipping L15 instead, this file is the single source of truth that needs editing in lockstep.

### Future strictly-DM-only mode (deferred)

A future sentinel value (e.g. `?channel=` literal empty string handled as "skip channel subscribe entirely") would let new clients open a DM-only WS without holding a channel topic open. This is **not** in v1 — the legacy default covers existing clients, and a DM-only client doesn't exist yet (the web app always has a channel selected; the CLI's `chatd dm watch` opens with `?channel=#general` because there's no harm in receiving channel frames it ignores). Defer to a follow-up if a use case appears.

## Frame routing rules

The summary below is normative. Anywhere it conflicts with [dms.md](dms.md) or [read-state.md](read-state.md), this file wins for the routing question; those files win for the wire shape.

| Frame | Topic the server emits to | Reach |
|---|---|---|
| `{type:"message"}` | channel topic (`Hub.Broadcast(channel, ...)`) | every WS subscribed to that channel |
| `{type:"presence"}` | channel topic (existing behavior; per-channel join/leave deltas) | every WS subscribed to that channel |
| `{type:"channel"}` | global (`Hub.BroadcastAll(...)`) | every connected WS |
| `{type:"dm"}` | `user:<sender>` AND `user:<recipient>` | each user's OWN connections (typically multi-tab) |
| `{type:"read"}` | `user:<viewer>` only | the originating viewer's OWN connections (cross-device sync; no peer fan-out per L10) |
| `{type:"error"}` | the originating connection only | unchanged from today |

Self-fanout note for DMs: the sender receives its own `{type:"dm"}` frame on its own `user:<sender>` topic. Multi-tab senders need this for sidebar consistency (Tab A sends, Tab B's sidebar updates without a `GET /api/dms` round-trip). The sender's client handles its own send-back idempotently — see [dms.md](dms.md) §"Self-sufficiency rationale".

## Emitter implementation (indicative, not normative — sub-issue W owns the shape)

```go
// after a successful POST /api/dms/{id}/messages handler runs InsertDMMessageTx:
frame, _ := encodeDMFrame(conv, dmMsg)
hub.Broadcast("user:"+conv.UserAID, frame)
hub.Broadcast("user:"+conv.UserBID, frame)

// after a successful POST /api/channels/{id}/read or POST /api/dms/{id}/read:
frame, _ := encodeReadFrame(scope, targetID, lastRead, unreadCount)
hub.Broadcast("user:"+viewerID, frame)
```

`encodeDMFrame` and `encodeReadFrame` live alongside the existing `channelEventFrame` helper in `apps/server/internal/http/`; the WS handler stays out of the encode path so the HTTP and WS routes share a single byte representation per frame kind.

## Topic key allocation

The `user:` prefix is reserved for inbox topics. Channel-name strings happen to start with `#` for `#general` and with a 26-char ULID for created channels — neither collides with `user:`. The Phase 9 migration adds no other reserved prefixes.

If a future feature wants a topic family (e.g. `org:<id>` for federation), the migration adds a new prefix and updates this file. The Hub's key namespace is flat and opaque — it's the prefix convention here that holds the contract.

## Backpressure / per-connection bound

No change to the existing `connSubscriber` send-channel bound. A single WS now receives traffic on two topics, so the worst-case fan-out per connection roughly doubles for users in busy channels who also receive DMs. At N=100 with the existing 256-deep buffered channel and `ws.SendCloseOnSlowConsumer` policy, the bound is comfortably below saturation. If a future profile shows the bound is tight, the fix is a per-topic queue rather than enlarging the shared one.

## Out of scope (Phase 9 v1)

- Multi-channel subscription (a single WS subscribing to >1 channel topic). The single-channel + `user:<viewer>` shape stays.
- Server-pushed pings / heartbeat frames. The existing read-deadline-driven stall detection in `go-client/ws.go:DefaultWatchReadIdleTimeout` is unchanged.
- Per-frame ack from client to server. Inbound application frames remain forbidden (PRD §"Design deviations" — the sender-spoofing rule is preserved).
- Topic-pattern subscriptions (e.g. wildcards). The Hub matches topics by exact-string equality and that stays.
