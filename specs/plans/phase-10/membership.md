# Phase 10 — Membership: `channel_members` schema, inviter-signature, lazy-wrap

**Parent epic:** #975
**Decision log:** `lt -p e2e-encryption 3` §6, §8, §9, §10, L8, L13, L14, L24, L25, L33, L41
**Cross-links:** [README.md](README.md) · [encryption.md](encryption.md) · [keys.md](keys.md) · [security.md](security.md)

This file is the contract for the explicit channel-membership relation, the inviter-signature defense, and the `#general` auto-add rule. Every Phase 10 sub-issue that creates, deletes, or verifies a `channel_members` row points here for the byte-level shape.

## Schema (decision log §6 + §10 — folded into the L32 single migration)

```sql
CREATE TABLE channel_members (
    channel_id          TEXT NOT NULL REFERENCES channels(id),
    user_id             TEXT NOT NULL REFERENCES users(id),
    inviter_user_id     TEXT NOT NULL REFERENCES users(id),
    inviter_sign_pubkey BLOB NOT NULL,                -- 32 bytes; pinned at invite time so
                                                      -- inviter rotation does not invalidate
                                                      -- the stored signature.
    inviter_signature   BLOB,                         -- 64 bytes Ed25519; NULLABLE.
                                                      -- NULL is permitted ONLY for
                                                      -- public-channel server-auto-add
                                                      -- (R1.2 carve-out per security.md).
                                                      -- L33 enforces NOT NULL at the
                                                      -- application layer for any row
                                                      -- whose channel has is_public = FALSE.
    invitee_box_pubkey  BLOB NOT NULL,                -- 32 bytes; pinned at invite time
    invitee_sign_pubkey BLOB NOT NULL,                -- 32 bytes; pinned at invite time
    added_at            TIMESTAMP NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);

-- L33: non-unique partial index for forensic queries — operators auditing
-- for evil-bot-via-server-injected-NULL membership rows scan this index.
-- Uniqueness is already covered by the table's primary key.
CREATE INDEX idx_channel_members_null_sig
    ON channel_members(channel_id, user_id)
    WHERE inviter_signature IS NULL;
```

`PRIMARY KEY (channel_id, user_id)` — one row per (channel, member). The migration also adds:

```sql
ALTER TABLE channels ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE;
```

The seeded `#general` row sets `is_public = TRUE`. Per L24, the `is_public` migration AND the `repo.CreateChannel(ctx, id, name, isPublic, createdAt)` signature change AND the `apps/server/internal/seed/seed.go` update to pass `is_public: true` for `#general` ALL ship in **one** PR (Wave 1 S — schema). Splitting them produces a window where `#general` has `is_public = FALSE` and the auto-join logic breaks.

## Inviter-signature scope (decision log §10)

`crypto_sign_detached` over:

```
b"snakd-mship-v1:" || channel_id || b"|" || user_id || b"|" || inviter_user_id ||
b"|" || inviter_sign_pubkey || b"|" || invitee_box_pubkey || b"|" || invitee_sign_pubkey ||
b"|" || added_at_rfc3339
```

`inviter_user_id` and `inviter_sign_pubkey` are signed too: a rogue server cannot swap who-the-inviter-is or substitute a different pinned pubkey to bypass TOFU at lazy-wrap-verification time. The signature is self-describing about which keypair signed it.

## L33 NULL-sig application-level enforcement

SQLite CHECK constraints can't directly reference another table without a trigger, so the validation lives in Go at the repo layer and is applied at INSERT time AND on every read that's about to feed lazy-wrap verification:

> `inviter_signature IS NOT NULL` for any row whose `channel_id` references a channel with `is_public = FALSE`.

Implementation lives in `apps/server/internal/repo/channel_members.go` (Wave 1 S — schema sub-issue creates the skeleton; downstream sub-issues fill in handlers). The L33 partial index above is for fast forensic scans, not a uniqueness constraint.

## `POST /api/channels/{id}/members` (decision log §7 amended by §10)

### Request body

```json
{
  "user_id": "<invitee>",
  "membership": {
    "inviter_user_id":     "<caller's user id>",
    "inviter_sign_pubkey": "<base64 32 — caller's CURRENT sign_pubkey>",
    "invitee_box_pubkey":  "<base64 32 — invitee's CURRENT box_pubkey>",
    "invitee_sign_pubkey": "<base64 32 — invitee's CURRENT sign_pubkey>",
    "added_at":            "<RFC3339>",
    "inviter_signature":   "<base64 64>"
  },
  "root_key_wrap": {
    "wrapped_key":       "<base64 48>",
    "sender_box_pubkey": "<base64 32 — caller's box_pubkey>",
    "nonce":             "<base64 24>"
  }
}
```

Note: `recipient_user_id` is omitted from `root_key_wrap` because the URL pins the invitee already (per the [encryption.md](encryption.md) `WrapEntry` shape rule).

### Server validation (atomic transaction)

1. Caller is a current `channel_members` row for `{id}`.
2. `membership.inviter_user_id == caller.id`.
3. `membership.inviter_sign_pubkey == users.sign_pubkey WHERE id = caller` — caller cannot pin a key that isn't currently theirs.
4. `inviter_signature` verifies under `inviter_sign_pubkey` over the §10 scope.
5. `membership.invitee_box_pubkey == users.box_pubkey WHERE id = user_id`.
6. `membership.invitee_sign_pubkey == users.sign_pubkey WHERE id = user_id`.
7. L30 — `root_key_wrap.sender_box_pubkey == users.box_pubkey WHERE id = caller`.
8. L39 — wrap byte-length validation (`wrapped_key == 48`, `nonce == 24`, `sender_box_pubkey == 32`, `inviter_signature == 64`, every pubkey == 32).
9. Atomic insert of `channel_members` row + `channel_keys` wrap row for the channel's current `generation_id` (resolved server-side).

`DisallowUnknownFields` per L1 — the `decodeJSON` strict-decode constraint applies. Every new field MUST be declared in the matching server-side struct.

## `POST /api/channels` (decision log §7 amended by §10)

The creator-bootstrap case carries a self-signed `MembershipBlock`:

```json
{
  "name":      "<name>",
  "is_public": false,
  "membership": {
    "inviter_user_id":     "<caller's user id>",
    "inviter_sign_pubkey": "<base64 32 — caller's CURRENT sign_pubkey>",
    "invitee_box_pubkey":  "<base64 32 — caller's CURRENT box_pubkey>",
    "invitee_sign_pubkey": "<base64 32 — caller's CURRENT sign_pubkey>",
    "added_at":            "<RFC3339>",
    "inviter_signature":   "<base64 64>"
  },
  "root_key_wraps": [
    {
      "recipient_user_id": "<creator's user id>",
      "wrapped_key":       "<base64 48>",
      "sender_box_pubkey": "<base64 32>",
      "nonce":             "<base64 24>"
    }
  ]
}
```

Server validation (decision log §10 self-bootstrap carve-out):

- `len(root_key_wraps) == 1`, recipient = caller.
- `channel_members` for the new channel has zero rows at the moment the call is processed (= caller is becoming the SOLE INITIAL MEMBER).
- `membership.inviter_user_id == membership.invitee_user_id == caller.id` (self-add).
- `membership.inviter_sign_pubkey == membership.invitee_sign_pubkey == users.sign_pubkey WHERE id = caller`.
- `membership.invitee_box_pubkey == users.box_pubkey WHERE id = caller`.
- Inviter-signature verifies over the §10 scope.
- L30 + L39 wrap validations as above.
- Generation is implicit `1`.

The `is_public` flag is irrelevant to the carve-out — both public-channel first-user-bootstrap (per [keys.md](keys.md) bootstrap mode) and private-channel creator-bootstrap are covered by the same zero-existing-members rule. Outside this window (the channel already has at least one member), self-signing is rejected.

## `DELETE /api/channels/{id}/members/{user_id}` (decision log §7 + L8 + L16)

### Authorization

- Caller must be a current member OR the user themselves (self-leave).
- L8 — `#general` membership is immutable. The handler returns 403 when `channel.name == seed.GeneralChannelName`. Mirrors the existing rename block at `apps/server/internal/http/channels_handlers.go:150`. Self-leave on `#general` is also blocked.

### Effect

1. Removes the `channel_members` row.
2. Removes the `channel_keys` wrap rows for *every* generation for that user. The leaver's local copies of root keys are not under server control (correct per decision log §2 — leaver retains pre-rotation history they could already read).
3. Triggers a key rotation: server broadcasts a `{type:"channel", data:{kind:"members_changed", channel_id, current_generation_id, members_at_rotation}}` WS frame to remaining members (L9).
4. Server inserts NO `channel_keys` rows of its own (L7 invariant preserved).

The first online remaining member's client races to generate a new root key, wrap it to every remaining member, and POST the wraps via the standalone keys-RPC (`POST /api/channels/{id}/keys` rotation mode — see [keys.md](keys.md)).

L16 — rotation triggers in v1 are member-removal-only. NO periodic time-based rotation. NO admin-triggered "rotate now" button. NO key-compromise-suspected manual flow.

## `GET /api/channels/{id}/members` (decision log §6)

### Response

```json
{
  "ok": true,
  "data": {
    "members": [
      {
        "user_id":              "<member ULID>",
        "username":             "<member username>",
        "box_pubkey":           "<base64 32>",
        "sign_pubkey":          "<base64 32>",
        "added_at":             "<RFC3339>",
        "membership": {
          "inviter_user_id":     "...",
          "inviter_sign_pubkey": "...",
          "invitee_box_pubkey":  "...",
          "invitee_sign_pubkey": "...",
          "added_at":            "...",
          "inviter_signature":   "..."
        }
      }
    ]
  },
  "error": null
}
```

The full `MembershipBlock` is returned alongside the convenience fields (`username`, `box_pubkey`, `sign_pubkey`, `added_at`) so clients computing wraps for new invites have the inviter-signature chain handy. Caller MUST be a current member of `{id}` (otherwise 403). The convenience `box_pubkey` / `sign_pubkey` fields reflect the user's CURRENT pubkeys (looked up from `users` at request time); the `membership` block carries the PINNED pubkeys (what was signed at invite time).

## `GET /api/channels` listing — member filter (decision log §6 + L25)

`GET /api/channels` filters to channels the viewer is a member of. The implementation also patches the `MaterializeChannelReadsTx` flow per L25:

> Both the pre-check count query (`apps/server/internal/repo/channel_reads.go:63-68`) and the sweep query (lines 82-89) gain a `JOIN channel_members USING (channel_id) WHERE user_id = ?` filter. Without this, the listing materialization keeps creating `channel_reads` rows for channels the viewer isn't a member of.

Closes the implicit-discovery hole in PRD §11's auto-materialize rule for `channel_reads`.

## `#general` auto-add and other public channels (decision log §6 + §9)

### `#general` (the seeded baseline)

The seed step (`apps/server/internal/seed/seed.go`) marks `#general` with `is_public = TRUE`. The user-registration handler (`apps/server/internal/http/auth_handlers.go`) at registration commit time inserts a `channel_members(#general.id, new_user.id, inviter_user_id = new_user.id, NULL signature, ...)` row in the same transaction. Server-side; no client wrap is computed at registration time because no `box_pubkey` is required to *be* a member. The wrap is delivered later via the lazy-wrap-on-online flow (L14).

Until a wrap arrives, the new user can see `#general` in their channel list but cannot read messages; the UI shows "waiting for key…" (the same join-handshake state described in [keys.md](keys.md) under "Lazy-wrap-on-online"). The `{type:"channel", data:{kind:"key_received", ...}}` WS frame closes the loop when the wrap is filled in.

#### Decision — self-invite (not system-invite)

The auto-add row uses **self-invite** semantics: `inviter_user_id = new_user.id`. The migration's `inviter_user_id TEXT NOT NULL REFERENCES users(id)` (`migrations/0006_encryption.sql:75`) needs a real user id, but the public-channel carve-out skips the invitation handshake — there is no actual inviter. Two consistent shapes were on the table:

1. **Self-invite** (chosen): the new user is recorded as their own inviter. Implemented in `apps/server/internal/http/auth_store.go:111-117` (`VALUES (channelID, id, id, ...)` — invitee and inviter columns are the same id). Inviter-signature stays NULL; L33's public-channel carve-out accepts it.
2. **System-invite** (rejected): pre-seed a sentinel `SYSTEM` user row, set `inviter_user_id = SYSTEM_user_id`. Audit trails read more legibly ("system-added" vs "self-added"), but it costs an extra always-present user row, an FK to a logical-not-real account, and a special case in every membership consumer that has to ignore the SYSTEM row in member listings, presence, etc.

Self-invite wins because:

- The signature is NULL either way (public-channel carve-out), so the inviter identity is *not* a security boundary here — both shapes are equally non-forgeable to an operator (operator can always insert any row directly).
- L33's "non-NULL signature required when `channels.is_public = FALSE`" check is the actual defense; `inviter_user_id` only has to satisfy the FK.
- Self-invite avoids the SYSTEM-row exclusion logic everywhere members are enumerated (`GET /api/channels/{id}/members`, presence, lazy-wrap recipients).
- The same shape generalizes to any future `is_public = TRUE` channel without a per-channel "who pre-seeded the system row?" question.

The convention is pinned by `tests/e2e/phase-10/registration-auto-add/registration_auto_add_test.go`: it asserts `inviter_user_id == user_id` and an absent `inviter_signature` for every auto-added `#general` row. Any future change to system-invite (or another scheme) must update both this section and that test in the same PR.

### Generalized public channels (decision log §9 — P2)

`channels.is_public BOOLEAN DEFAULT FALSE`. `#general` is the special case `is_public = TRUE`. The `POST /api/channels` body gains optional `is_public: bool` (default false). Channels created without the flag are private-by-default, matching §6.

At user registration, server inserts a `channel_members` row for the new user in EVERY channel where `is_public = TRUE` (one transaction). The new user does NOT get any wrap rows at registration time (server has no plaintext keys, L7).

L15 — `is_public` is **immutable after channel creation in v1**. No `PATCH /api/channels/{id}` endpoint for `is_public`. Toggling public→private after creation would require kicking every non-creator + a key rotation; private→public would require auto-adding all users + lazy-wrap fan-out. Both flows are deferred.

### Lazy-wrap-on-online (L14)

Every existing member's client, on WS connect, queries `GET /api/channels/{id}/members/wraps-needed` for each channel they're a member of. The L22 response includes the full `MembershipBlock` for every missing-wrap row so verifiers can check the inviter-signature BEFORE computing a wrap. Client computes wraps using the missing user's `invitee_box_pubkey` returned in the membership block, then posts them via `POST /api/channels/{id}/keys` with `generation_id == current_max` (NOT `current_max + 1` — fill-in is not rotation; see [keys.md](keys.md) for the three modes).

L31 — clients debounce: `wraps-needed` is queried ONCE per WS-connection lifetime (cached); flapping reconnects do not re-query unless > 60 seconds have elapsed.

L41 — late-joiner-of-late-joiner liveness: when verifier C's TOFU is empty for an inviter A who invited B earlier, C SKIPS the wrap and some other online member (with A in their TOFU) eventually fills it. In a friend group with active chat this resolves quickly (every active member has all peers in TOFU after a single message exchange). On a brand-new server with three users registered before the first message, this is a temporary deadlock until any member sends a message. UI shows "waiting for key…" per L14. Documented as expected behaviour, not a bug.

### Self-leave is one-way for public channels (decision log §9)

`DELETE /api/channels/{id}/members/{user_id}` (self-leave) on a public channel triggers normal rotation per the DELETE flow above. The leaver loses access to future messages; the user can self-leave a public channel and *will not* be re-auto-joined unless they re-register (membership removal is "I'm out of this channel until invited back," consistent across public/private). `#general` self-leave remains blocked by L8.

## Threat-model fit (cross-reference to [security.md](security.md))

- **Operator forges `channel_members` row in a private channel (`is_public = FALSE`)**: no `inviter_signature` (operator has no member's `sign_priv`) OR a fake signature → lazy-wrap clients verify and reject → no wrap is computed → operator cannot decrypt. Defense holds.
- **Operator forges `channel_members` row in a PUBLIC channel**: NULL signature is accepted under R1.2's documented residual. No change vs. §9.
- **Modified-server-JS (R1.1)** can still bypass any client-side verification by serving JS that skips it. The defense raises the bar from "any rogue server with DB access" (current state, broken without §10) to "any rogue server who also serves modified client JS" (R1.1, accepted).

## Out of scope (Phase 10 v1)

- Public-channel "discovery" (a list of channels you could join by request). PRD §13 keeps it as roadmap. Members are added by existing members; there is no self-join flow.
- Channel rename / write rate-limit machinery — unchanged from Phase 8.
- Toggling `is_public` after creation (L15).
- Admin-attested signing of pubkeys / safety-number QR verification UX. v1 is TOFU + key-change warning only (L20). See [security.md](security.md).
