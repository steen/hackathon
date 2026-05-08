# Phase 9 — Read state: schema, envelopes, formulas

**Parent epic:** #860
**Decision log:** `lt -p direct-messages 3` §5, §11, L3, L5, L6
**Cross-links:** [README.md](README.md) · [dms.md](dms.md) · [ws-routing.md](ws-routing.md)

This file is the contract for read-state semantics and the `{type:"read"}` WS envelope. Every Phase 9 sub-issue that reads, writes, or renders unread badges points here.

## Tables (mirrors PRD §11 schema additions)

### `channel_reads`

| Column | Type | Notes |
|---|---|---|
| `channel_id` | TEXT (ULID) | NOT NULL; primary key part 1 |
| `user_id` | TEXT (ULID) | NOT NULL; primary key part 2 |
| `last_read_message_id` | TEXT (ULID) | **NOT NULL** — see auto-materialization below |
| `updated_at` | TIMESTAMP | NOT NULL |

`PRIMARY KEY (channel_id, user_id)` — one row per (channel, viewer).

### `dm_reads`

| Column | Type | Notes |
|---|---|---|
| `conversation_id` | TEXT (ULID) | NOT NULL; primary key part 1; FK → `dm_conversations.id` |
| `user_id` | TEXT (ULID) | NOT NULL; primary key part 2 |
| `last_read_dm_message_id` | TEXT (ULID) | **NULLABLE** — NULL means "viewer has never explicitly read" |
| `updated_at` | TIMESTAMP | NOT NULL |

`PRIMARY KEY (conversation_id, user_id)` — one row per (conversation, viewer).

The asymmetric NOT-NULL / NULLABLE distinction is the load-bearing detail. Channels and DMs differ in membership semantics, so they differ in materialization.

## Asymmetric initialization (decision-log §11)

### Channels — auto-materialize on listing

Every authenticated user is a member of every channel (PRD §9 — no per-channel ACL in MVP). A new user with no `channel_reads` rows would otherwise see "50K unread" on every channel — wrong UX.

`GET /api/channels` therefore materializes missing rows inside the listing transaction:

```sql
BEGIN;
INSERT OR IGNORE INTO channel_reads (channel_id, user_id, last_read_message_id, updated_at)
SELECT c.id, ?viewer, c.last_message_id, NOW()
FROM channels c
WHERE NOT EXISTS (SELECT 1 FROM channel_reads
                  WHERE channel_id = c.id AND user_id = ?viewer);
-- main listing query joins channel_reads (now populated for every channel)
SELECT c.id, c.name, c.created_at, c.last_message_id, c.last_message_at,
       <unread_count formula below>
FROM channels c
LEFT JOIN channel_reads r
  ON r.channel_id = c.id AND r.user_id = ?viewer
ORDER BY c.id;
COMMIT;
```

Note: the materialized baseline is **frozen at first-list** — it pins to whatever `channels.last_message_id` was at that instant, NOT to a future channels.last_message_id. New messages after first-list correctly count as unread because the baseline doesn't advance with the channel's tip.

For channels that have never had a message, `c.last_message_id IS NULL`. The auto-materialization writes NULL into `channel_reads.last_read_message_id` in that case, but the channel-reads column is NOT NULL — so the migration includes a generated/sentinel value or the materialization query is structured to skip NULL-tipped channels (the latter is simpler; channels with no messages have `unread_count = 0` regardless of materialization). The R sub-issue's `MaterializeChannelReadsTx` decides between these two; both honor the contract that `channel_reads.last_read_message_id` is non-NULL after materialization.

### DMs — lazy NULL

`GET /api/dms` does **not** auto-materialize `dm_reads`. NULL `last_read_dm_message_id` is legal and means "viewer has never explicitly read this conversation".

Rationale: DMs are 1:1. If a conversation exists in your sidebar, every message from your peer that you haven't `POST /read`-ed is genuinely unread — including ones that arrived while you were offline. Auto-materializing would silently swallow offline-arrived DMs.

The sender's `dm_reads` row IS materialized — but only inside the message-insert transaction (`InsertDMMessageTx`, see [dms.md](dms.md)), so the sender is implicitly caught up on their own send (`unread_count = 0` immediately after sending).

The recipient's row is created only by an explicit `POST /api/dms/{id}/read` call.

## `unread_count` formula (L6, asymmetric)

### Channels

```sql
unread_count = COUNT(*)
FROM messages
WHERE channel_id = c.id
  AND id > channel_reads.last_read_message_id  -- never NULL after materialization
```

Implementation note: the query joins `messages` once per channel; at N=100 users × ~10 channels × low message volume this is trivial. If it becomes a hot path, denormalize a `messages_since` counter on the join — but **not** in v1.

### DMs

```sql
unread_count = COUNT(*)
FROM dm_messages
WHERE conversation_id = c.id
  AND sender_user_id != ?viewer        -- own messages don't count
  AND id > COALESCE(dm_reads.last_read_dm_message_id, '')
```

`COALESCE(..., '')` handles the legitimate-NULL case: empty string is less than every ULID, so all peer messages count as unread when no `dm_reads` row exists. (ULIDs are case-sensitive 26-char Crockford base32; `''` < any ULID under SQLite's default collation.)

The sender-self filter (`sender_user_id != ?viewer`) avoids the edge case where the sender's `dm_reads` row was advanced by `InsertDMMessageTx` to message N, then the peer sends N+1; the sender's `unread_count` for that conversation is 1, not 2.

## HTTP endpoints

### `POST /api/channels/{id}/read`

**Request body:**
```json
{ "message_id": "01HK...MSG" }
```

**Response (200):**
```json
{ "ok": true, "data": { "ok": true }, "error": null }
```

The handler:

1. Resolves `{id}` → `channels` row. 400 on malformed id, 404 on unknown id.
2. Validates `message_id` is a 26-char ULID and refers to a message with `channel_id == {id}`. 400 / 404 respectively.
3. Applies the per-user `read-mark` bucket (burst 50 / refill 1m — L17). 429 on exceed.
4. Refuses to advance backwards (advance-only — L5): if `message_id <= current channel_reads.last_read_message_id`, the call is a no-op (still returns 200; idempotent client behavior).
5. UPSERT: `INSERT OR IGNORE` then advance-only `UPDATE` per L5.
6. Emits a `{type:"read"}` frame to `user:<viewer>` only (cross-device sync; no peer fan-out — L10).

### `POST /api/dms/{id}/read`

Same shape as the channel endpoint, against `dm_messages` / `dm_reads`. Differences:

- 404 on non-participation in the conversation (L8).
- The first call materializes the recipient's `dm_reads` row (it was NULL until now). Subsequent calls are advance-only.
- Same `read-mark` bucket as the channel-read endpoint.

## `{type:"read"}` WS envelope

```ts
interface ReadEvent {
  type: "read";
  data: {
    scope: "channel" | "dm";
    target_id: string;             // channel id when scope=channel; conversation id when scope=dm
    last_read_message_id: string;  // ULID; the new read pointer
    unread_count: number;          // server-computed unread_count after advance
  };
}
```

```go
type ReadEvent struct {
    Scope             string `json:"scope"`               // "channel" | "dm"
    TargetID          string `json:"target_id"`
    LastReadMessageID string `json:"last_read_message_id"`
    UnreadCount       int    `json:"unread_count"`
}
```

Routed only to the originating viewer's `user:<viewer>` topic (cross-device sync; peers do not see read receipts — L10).

The frame is emitted **after** the DB UPSERT commits, so a client receiving this frame on Tab B can trust that a subsequent listing-fetch on Tab A will return a matching `unread_count`. The reconcile rule from §12 (OVERWRITE on listing fetch) handles the brief window where Tab A's in-flight listing query straddles the UPSERT.

## Listing-additive fields (PRD §10)

Channel listing (`GET /api/channels`) gains:

```ts
interface Channel {                           // existing fields kept
  id: string;
  name: string;
  created_at: string;
  // additive (Phase 9):
  last_message_id: string | null;
  last_message_at: string | null;
  unread_count: number;
}
```

DM listing (`GET /api/dms`) carries `unread_count` as defined in [dms.md](dms.md)'s `Conversation` interface.

The "optional-first" wire-types coordination per L26 applies: TS/Go client types may declare these fields BEFORE the server populates them, with a coordinated wire-types PR landing in the same wave as the server populator. The Phase 9 wire-types sub-issue (T) owns the cross-package update; the server-side populator sub-issues (R, H) cite the wire-types PR.

## Client behavior — reconcile semantic (decision-log §12)

On every listing fetch (`GET /api/channels` or `GET /api/dms`), the client SHOULD overwrite its in-memory `unread_count[target]` map with the server-supplied value, replacing any local increments. Rationale: at N=100 the WS-frame-vs-listing race window is ~1 RTT; transient stale badge values self-correct on the next listing call or `POST /read`. The alternative (max(local, server)) risks overcounting when a WS frame fires during the listing's read time.

Web client debounces `POST /read` ~250ms trailing per L22 — so a fast scroll past 30 messages issues one `POST /read` for the highest-id-in-view, not 30.

## Out of scope (Phase 9 v1)

- Read receipts visible to peers / typing indicators (L10).
- Per-message read flags — only "last_read pointer" granularity (L3).
- Notification mute / DND — unread badges are the only signal in v1.
- Read-state for the seeded `#general` channel pre-Phase-9 messages — auto-materialization at first listing pins to current tip, so historical messages count as "read" for users who join after Phase 9 lands. This is the documented baseline behavior, not a bug.
