# Phase 9 — Direct messages: wire types + endpoints

**Parent epic:** #860
**Decision log:** `lt -p direct-messages 3`
**Cross-links:** [README.md](README.md) · [read-state.md](read-state.md) · [ws-routing.md](ws-routing.md)

This file is the contract for the DM surface. Every Phase 9 sub-issue that emits or consumes a DM type points here for the wire shape.

## Wire types

Hand-mirrored on both sides per CLAUDE.md "Wire types" — `packages/go-client/dm.go` (new) and `packages/api-client/src/types.ts` (existing file, additive). Each new file carries a top-of-file `sync with <counterpart>` comment.

### `Conversation` (TS interface, Go struct)

```ts
interface Conversation {
  id: string;                   // ULID; primary key on dm_conversations
  user_a_id: string;            // canonical-ordered: user_a_id < user_b_id (L2)
  user_b_id: string;
  last_message_id: string | null;   // null until the first message; mirrors dm_conversations.last_message_id
  last_message_at: string | null;   // ISO 8601 UTC; null until the first message
  created_at: string;
  // Listing-only fields — server attaches these on GET /api/dms responses,
  // and on the {type:"dm"} WS frame (§8 self-sufficiency).
  // POST /api/dms returns these populated for the freshly-created or
  // existing row.
  peer: { id: string; username: string };  // the OTHER participant relative to the requesting viewer
  unread_count: number;                    // see read-state.md for the formula
}
```

```go
// packages/go-client/dm.go
type Conversation struct {
    ID            string  `json:"id"`
    UserAID       string  `json:"user_a_id"`
    UserBID       string  `json:"user_b_id"`
    LastMessageID *string `json:"last_message_id"`
    LastMessageAt *string `json:"last_message_at"` // ISO 8601 string; pointer for nullability
    CreatedAt     string  `json:"created_at"`
    Peer          UserSummary `json:"peer"`        // {id, username}
    UnreadCount   int     `json:"unread_count"`
}
```

`peer` is computed per-request: when the viewer is `user_a_id` the peer is `user_b_id`'s `{id, username}` summary, and vice versa. `peer.id` therefore always equals one of `user_a_id`/`user_b_id` but never the viewer's own id.

### `DMMessage` (TS interface, Go struct)

```ts
interface DMMessage {
  id: string;              // ULID; primary key on dm_messages
  conversation_id: string; // FK → dm_conversations.id
  sender_user_id: string;
  body: string;            // ≤ MaxMessageBodyBytes = 4096 (L16)
  created_at: string;      // ISO 8601 UTC
}
```

```go
type DMMessage struct {
    ID             string `json:"id"`
    ConversationID string `json:"conversation_id"`
    SenderUserID   string `json:"sender_user_id"`
    Body           string `json:"body"`
    CreatedAt      string `json:"created_at"`
}
```

`DMMessage` is immutable on the wire (L9 — no edit/delete in v1). `body` shares its 4096-byte cap with channel messages (`MaxMessageBodyBytes`); the constant lives in `apps/server/internal/http` next to the channel-message validator and is referenced from both the DM and channel code paths.

### `{type:"dm"}` WS envelope (self-sufficient on first contact — §8)

```ts
interface DMEvent {
  type: "dm";
  data: {
    conversation: Conversation; // see above; carries peer + unread_count baseline
    dm_message: DMMessage;
  };
}
```

```go
type DMEvent struct {
    Conversation Conversation `json:"conversation"`
    DMMessage    DMMessage    `json:"dm_message"`
}
```

Self-sufficiency rationale: when a recipient receives their first-ever DM from a sender, the recipient's client has NO `Conversation` row in its sidebar cache. Without the embedded `conversation` block, the client would have to round-trip `GET /api/dms` before rendering the new sidebar entry — and during the `GET` round-trip a second incoming DM could race the listing. Embedding the conversation block makes the frame stand alone (decision §8). The `conversation.unread_count` value is the **server-authoritative baseline at the moment of the frame**; the client's in-memory counter is then incremented locally per [§12 of the decision log](#) on subsequent frames until the next `GET /api/dms` (which OVERWRITES per §12 reconcile rule).

The frame is fanned out to BOTH participants' `user:<viewer>` topics (sender + recipient). The sender's client receives its own send-back as confirmation (and to keep multi-tab views consistent); when the sender already has the `Conversation` cached, the embedded block is redundant but cheap.

## HTTP endpoints

All endpoints sit under `Authorization: Bearer <token>`. Error envelopes follow PRD §10.

### `POST /api/dms`

**Request body:**
```json
{ "peer_user_id": "01HK...PEER" }
```

**Response (201 created or 200 existing):**
```json
{ "ok": true, "data": <Conversation>, "error": null }
```

Idempotent find-or-create. The handler:

1. Validates `peer_user_id` is a 26-char ULID and exists in `users`. Returns 400 / 404 respectively.
2. Refuses self-DM (`peer_user_id == viewer`) with 400 + `error.code = "INVALID_PEER"` (decision §6).
3. Sorts the (viewer, peer) pair so `user_a_id < user_b_id` (L2).
4. Calls `repo.FindOrCreateDMConversation(ctx, userA, userB) (Conversation, created bool, err)` — the implementation is a single SQL transaction with `INSERT ... ON CONFLICT DO NOTHING` then `SELECT`.
5. Returns 201 when `created == true`, 200 otherwise (L18). Both responses carry the full `Conversation` envelope including the `peer` summary computed for the viewer.

No rate limit on this endpoint — find-or-create is cheap and the burst pattern (50 friends in one sitting) is legitimate. The `dm-write` bucket gates the message-send endpoint instead.

### `GET /api/dms`

**Response:**
```json
{ "ok": true, "data": { "conversations": [<Conversation>] }, "error": null }
```

Lists every conversation in which the viewer is a participant **AND** which has at least one message (decision §3 — empty conversations are hidden until they have content). The query is:

```sql
SELECT c.id, c.user_a_id, c.user_b_id, c.last_message_id, c.last_message_at, c.created_at,
       <peer summary>, <unread_count>
FROM dm_conversations c
WHERE c.last_message_id IS NOT NULL
  AND (c.user_a_id = ?viewer OR c.user_b_id = ?viewer)
ORDER BY c.last_message_at DESC;
```

`unread_count` is computed via `COALESCE` over `dm_reads` per the formula in [read-state.md](read-state.md). No pagination in v1 (L12). No body preview in v1 (decision §9 — the listing is for ordering and badges, the message viewer renders bodies).

### `POST /api/dms/{id}/messages`

**Request body:**
```json
{ "body": "..." }
```

**Response (201):**
```json
{ "ok": true, "data": <DMMessage>, "error": null }
```

The handler:

1. Resolves `{id}` → `Conversation` row. 400 on malformed id, 404 on unknown id.
2. Verifies the viewer is `user_a_id` or `user_b_id`; else 404 (no membership leak — L8). The 404-on-non-participation pattern matches channel-message ACL conventions.
3. Validates `body` is non-empty after trim and ≤ `MaxMessageBodyBytes = 4096`. 400 on either failure.
4. Applies the per-user `dm-write` rate-limit bucket (burst 10 / refill 1m — L17). 429 on exceed.
5. Calls `repo.InsertDMMessageTx(ctx, conversationID, senderID, body, now) (DMMessage, error)` — single transaction:
   - `INSERT INTO dm_messages` with a fresh ULID.
   - `UPDATE dm_conversations SET last_message_id = ?, last_message_at = NOW()`.
   - `INSERT OR IGNORE INTO dm_reads (conversation_id, user_id, last_read_dm_message_id, updated_at) VALUES (?, sender, <new_dm_message_id>, NOW())`.
   - `UPDATE dm_reads SET last_read_dm_message_id = <new_dm_message_id>, updated_at = NOW() WHERE conversation_id = ? AND user_id = sender AND (last_read_dm_message_id IS NULL OR last_read_dm_message_id < <new_dm_message_id>)` (advance-only — L21).
6. Emits `{type:"dm"}` to `user:<sender>` and `user:<recipient>` Hub topics (see [ws-routing.md](ws-routing.md)).

The recipient's `dm_reads` row is NOT created here. NULL means "viewer has never explicitly read this conversation" → all peer messages count as unread → correct badge for offline-arrived DMs. The recipient's row materializes only via `POST /api/dms/{id}/read` (see [read-state.md](read-state.md)).

### `GET /api/dms/{id}/messages?limit=50&before=<msg_id>`

**Response:**
```json
{ "ok": true, "data": { "messages": [<DMMessage>] }, "error": null }
```

Paginated history mirroring `GET /api/channels/{id}/messages`:

- ULID-cursor: `?before=<msg_id>` returns up to `limit` messages with `id < before`, newest-first.
- Default `limit = 50`, max `limit = 200`.
- 400 on malformed id; 404 on unknown id; 404 on non-participation (L8).

The handler reuses `repo.ListMessages`-style pagination — same cursor semantics, different table.

## Out of scope (Phase 9 v1)

- Read receipts shown to peers (L10).
- Typing indicators (L10).
- Group DMs / multi-party rooms — explicitly 1:1 only.
- Message edit / delete (L9).
- Body previews on listing endpoints (decision §9).
- Pagination on `GET /api/dms` itself (L12; messages within a conversation ARE paginated).
- Per-conversation mute / archive — UI does not surface either in v1.
