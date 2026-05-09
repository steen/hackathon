# Phase 10 — Keys: `channel_keys` / `dm_conversation_keys` schema and the three-mode keys-RPC

**Parent epic:** #975
**Decision log:** `lt -p e2e-encryption 3` §5, §7, §8, L5, L6, L7, L9, L12, L14, L16, L22, L29, L30, L31, L35, L36, L39, L40, L41
**Cross-links:** [README.md](README.md) · [encryption.md](encryption.md) · [membership.md](membership.md) · [security.md](security.md)

This file is the contract for the per-recipient key-wrap tables (`channel_keys`, `dm_conversation_keys`), the three modes of the standalone keys-RPC (`POST /api/channels/{id}/keys`), the lazy-wrap-on-online flow's `wraps-needed` response (L22), and the wrap-integrity recovery endpoint (`POST /api/channels/{id}/members/{user_id}/replay-wrap`). Wire shapes for `WrapEntry` and `MembershipBlock` are in [encryption.md](encryption.md).

## Storage shape (decision log §5 + §7 — folded into the L32 single migration)

```sql
CREATE TABLE channel_keys (
    channel_id        TEXT      NOT NULL REFERENCES channels(id),
    generation_id     INTEGER   NOT NULL,
    member_user_id    TEXT      NOT NULL REFERENCES users(id),
    wrapped_key       BLOB      NOT NULL,                 -- 48 bytes (crypto_box: 32 payload + 16 Poly1305 MAC)
    sender_box_pubkey BLOB      NOT NULL,                 -- 32 bytes (the wrapper's box_pubkey at wrap time)
    nonce             BLOB      NOT NULL,                 -- 24 bytes (XSalsa20)
    created_at        TIMESTAMP NOT NULL,
    PRIMARY KEY (channel_id, generation_id, member_user_id)
);

CREATE TABLE dm_conversation_keys (
    conversation_id   TEXT      NOT NULL REFERENCES dm_conversations(id),
    member_user_id    TEXT      NOT NULL REFERENCES users(id),
    wrapped_key       BLOB      NOT NULL,                 -- 48 bytes
    sender_box_pubkey BLOB      NOT NULL,                 -- 32 bytes
    nonce             BLOB      NOT NULL,                 -- 24 bytes
    created_at        TIMESTAMP NOT NULL,
    PRIMARY KEY (conversation_id, member_user_id)
);
```

DM has no `generation_id` column — DMs never rotate (decision log §5: 1:1, no membership changes possible). `crypto_box` produces a 48-byte ciphertext (32-byte payload + 16-byte Poly1305 MAC); the `wrapped_key` BLOB is exactly that ciphertext. `sender_box_pubkey` is stored alongside the wrap so receivers compute `crypto_box_open` without a server round-trip and so a wrapper rotating their `box_pubkey` later does not break decryption of historical wraps.

## L7 — `channel_keys` / `dm_conversation_keys` server-side invariant

> After every successful create or add-member transaction: `channel_members` row exists ⇔ matching `channel_keys` row for the current generation exists. After `POST /api/dms` 201: each member of `dm_conversations` has exactly one `dm_conversation_keys` row. **Server NEVER inserts wrap rows of its own initiative — it has no plaintext key.**

The L7 invariant is enforced inside the atomic transactions of the four wrap-creating handlers — `POST /api/channels`, `POST /api/dms` (201 path), `POST /api/channels/{id}/members`, and `POST /api/channels/{id}/keys` (all three modes below). Lazy fill-in (L14) is the orthogonal mechanism by which `channel_members` rows that exist *without* a wrap (server-only `#general`/public-channel auto-add per [membership.md](membership.md)) eventually get one.

## Three modes of `POST /api/channels/{id}/keys` (decision log §8)

The endpoint validates its own precondition based on `generation_id`'s relationship to `max(channel_keys.generation_id)` for the channel. The body shape is identical across all three modes — only the precondition differs:

```json
{
  "generation_id": <int>,
  "wraps": [<WrapEntry>, ...]
}
```

| Mode      | Trigger                                               | Precondition                                                   | `generation_id`           | Wrap-list                                                        |
| --------- | ----------------------------------------------------- | -------------------------------------------------------------- | ------------------------- | ---------------------------------------------------------------- |
| Bootstrap | First user of `#general` (or any auto-join channel)   | `max(channel_keys.generation_id) IS NULL` for the channel      | `1`                       | Exactly one entry; recipient = caller (wrap-to-self)             |
| Fill-in   | Existing member fills a missing wrap (L14 lazy-wrap)  | `current_max` exists; wrap row missing for `(gen, recipient)`  | `== current_max`          | Exactly one entry; recipient is a current member without a wrap  |
| Rotation  | `members_changed` after a `DELETE …/members/…`        | `current_max` exists; caller is a current member               | `== current_max + 1`      | Covers every current `channel_members` row exactly once          |

Any other combination → 400 `"invalid_generation"`.

### Bootstrap mode (decision log §8)

The first user to land in `#general` (or any future auto-add public channel) bootstraps the channel themselves. Server inserts the membership row at registration with NULL `inviter_signature` (per [membership.md](membership.md)); the user's client detects "member of channel, zero generations exist" on first authenticated session and posts `generation_id: 1` with a single wrap-to-self.

Server validation for bootstrap mode:

1. `max(channel_keys.generation_id) IS NULL` for the channel — independent of `channel_members` row count (decision log §8 corrected per CONTR-2: race where a second user registers before the first user's session has bootstrapped is handled by lazy-fill once bootstrap lands).
2. Caller is a current `channel_members` row.
3. `len(wraps) == 1`.
4. `wraps[0].recipient_user_id == caller.id` (wrap-to-self).
5. L30 — `wraps[0].sender_box_pubkey == users.box_pubkey WHERE id = caller`.
6. L39 — wrap byte-length validation.
7. Atomic insert of the `channel_keys` row.

### Fill-in mode (decision log §8 + L14)

Triggered by the lazy-wrap-on-online loop ([membership.md](membership.md) "Lazy-wrap-on-online (L14)"): existing member sees a `wraps-needed` row for some recipient, verifies the inviter-signature chain (L22), computes the wrap, and posts it.

Server validation for fill-in mode:

1. `current_max` exists (`max(channel_keys.generation_id) IS NOT NULL`).
2. `generation_id == current_max`.
3. Caller is a current `channel_members` row.
4. `len(wraps) == 1`.
5. Recipient (`wraps[0].recipient_user_id`) is a current `channel_members` row.
6. No `channel_keys` row exists for `(channel_id, generation_id, recipient_user_id)` — race-loss returns 409.
7. L30 — `wraps[0].sender_box_pubkey == users.box_pubkey WHERE id = caller`.
8. L39 — wrap byte-length validation.
9. Atomic insert of the `channel_keys` row.
10. On success, server emits `{type:"channel", data:{kind:"key_received", channel_id, generation_id}}` to the recipient's `user:<viewer>` topic (L9 + L14 — closes the "waiting for key…" UI loop).

### Rotation mode (decision log §7 + L9 + L16)

Triggered by `DELETE /api/channels/{id}/members/{user_id}` (L16: rotation triggers in v1 are member-removal-only). After the DELETE, server broadcasts `{type:"channel", data:{kind:"members_changed", channel_id, current_generation_id, members_at_rotation}}` to remaining members; the first online remaining member's client races to generate a new root key, wrap it to every remaining member, and POST.

Server validation for rotation mode:

1. `current_max` exists.
2. `generation_id == current_max + 1`.
3. Caller is a current `channel_members` row.
4. `wraps` covers every current `channel_members` row exactly once (no duplicates, no extras, no missing).
5. Every wrap obeys the per-entry rules:
    - L30 — `wrap.sender_box_pubkey == users.box_pubkey WHERE id = caller`.
    - L39 — `wrapped_key == 48` bytes, `nonce == 24` bytes, `sender_box_pubkey == 32` bytes.
6. Atomic insert of all `channel_keys` rows for the new generation. Race losers get 409 (someone else already rotated to `current_max + 1`).

## Standalone keys-RPC vs. inline wraps (decision log §7 — locked-in default L7)

The standalone `POST /api/channels/{id}/keys` exists ONLY for the three modes above (bootstrap, fill-in, rotation). It is **NOT** used for create or add-member — those are atomic-inline (decision log §7):

- `POST /api/channels` carries the bootstrap wrap inline (per [membership.md](membership.md) "POST /api/channels"). Generation is implicit `1`.
- `POST /api/dms` 201 carries both wraps inline (one for each peer; see [encryption.md](encryption.md) `WrapEntry` shape — `recipient_user_id` is required because the wrap-list has two entries).
- `POST /api/dms` 200 (idempotent re-call) MUST omit `root_key_wraps`; if present, server returns 409 (L6 — wraps are immutable post-create; L12 — server-side enforcement). DMs never rotate.
- `POST /api/channels/{id}/members` carries the wrap inline (per [membership.md](membership.md)). Wrap recipient is implicit (`= user_id` from the body). Generation is the channel's current `current_max`.

Splitting these into "membership row first, wrap later via standalone RPC" was rejected (decision log §7 — closes BG-3, BG-5, contradiction C-1) because it admits a window where `channel_members` exists without a `channel_keys` row, breaking L7.

## `GET /api/channels/{id}/members/wraps-needed` — L22 contract

Required by the lazy-wrap-on-online flow (L14). The verifier-side flow is enforced client-side; the server is just the inviter-signature chain courier.

### Authorization

- JWT required.
- Caller MUST be a current `channel_members` row for `{id}`. Otherwise 403.

### Response shape (L22)

```json
{
  "ok": true,
  "data": {
    "channel_id": "...",
    "is_public":  <bool>,
    "missing": [
      {
        "user_id":       "<missing recipient ULID>",
        "generation_id": <int>,
        "membership": {
          "inviter_user_id":     "...",
          "inviter_sign_pubkey": "<base64 32>",
          "invitee_box_pubkey":  "<base64 32>",
          "invitee_sign_pubkey": "<base64 32>",
          "added_at":            "<RFC3339>",
          "inviter_signature":   "<base64 64> | null"
        }
      }
    ]
  },
  "error": null
}
```

`is_public` is server-resolved from `channels.is_public` per L38 — verifier needs it to decide NULL-signature handling without a dependency on the channel-listing cache (NEW-2 fix: first-session new users would otherwise hit a stale-listing race). One `is_public` value per response, since each call is scoped to one channel.

`missing` is the server-computed list of `(user_id, generation_id)` pairs where `channel_members` has a row but `channel_keys` does not for the channel's CURRENT generation. The full `MembershipBlock` (per [encryption.md](encryption.md)) is included so the verifier can run the L22 flow before computing a wrap.

### L22 verifier-side flow (BEFORE computing a wrap)

1. Validate `membership.inviter_signature` (when non-NULL) under `membership.inviter_sign_pubkey` over the `snakd-mship-v1:` scope (see [encryption.md](encryption.md)).
2. Verify the pinned `inviter_sign_pubkey` is in the verifier's TOFU history for `inviter_user_id` — ANY historical entry is acceptable (L34 split: pinned-signature artifact verification accepts any historical pubkey, unlike live-message verification which requires most-recent-only).
3. Verify the `invitee_*_pubkey` fields match the verifier's TOFU history for the invitee, OR (first-contact) accept-and-cache.
4. For public channels (`is_public = TRUE`), `inviter_signature` MAY be NULL — verifier accepts under R1.2's documented residual (see [security.md](security.md)).
5. For private channels, NULL `inviter_signature` → REFUSE to compute the wrap. (L33 — server-side INSERT enforcement is the second layer; the client also refuses.)
6. If all checks pass, compute the wrap and POST via fill-in mode.

Verifier-side rejection is silent — the verifier just doesn't compute the wrap. Some other online member with the right TOFU history eventually does (L41 — late-joiner-of-late-joiner liveness). On a brand-new server with three users registered before any message exchange, this is a temporary deadlock until any member sends a message; expected behaviour, not a bug.

### Debouncing (L31)

- **Client-side**: each WS-connection lifetime queries `wraps-needed` ONCE per channel; flapping reconnects do NOT re-query unless > 60 seconds have elapsed since the last query.
- **Server-side**: per-user rate-limit bucket `wraps-needed-read` (burst 10 / refill 1 minute) lives in `apps/server/internal/ratelimit/config.go` per L36. The per-IP/per-user shape mirrors the existing `ChannelWriteUserConfig`, `DMWriteUserConfig`, `ReadMarkUserConfig` pattern.

## `POST /api/channels/{id}/members/{user_id}/replay-wrap` — L29 + L35

Wrap-integrity recovery (decision log L29 + L35). A rogue inviter can submit a syntactically-valid wrap (passes signature checks per [membership.md](membership.md)) whose `wrapped_key` is garbage bytes — the recipient's `crypto_box_open` fails. The recipient's client emits a `{type:"channel", data:{kind:"wrap_failed", channel_id, generation_id}}` WS frame to its OWN `user:<viewer>` topic (per [encryption.md](encryption.md) L9 entry); the UI surfaces "Channel key from <inviter> appears corrupted — ask another member to re-issue."

Recovery: ANY current member (other than the recipient) calls `replay-wrap` with a fresh `MembershipBlock` (the re-issuer is treated as a fresh inviter for this generation) AND a new wrap.

### Request body (mirrors `POST /api/channels/{id}/members` shape)

```json
{
  "membership": {
    "inviter_user_id":     "<re-issuer's user id>",
    "inviter_sign_pubkey": "<base64 32 — re-issuer's CURRENT sign_pubkey>",
    "invitee_box_pubkey":  "<base64 32 — recipient's CURRENT box_pubkey>",
    "invitee_sign_pubkey": "<base64 32 — recipient's CURRENT sign_pubkey>",
    "added_at":            "<RFC3339>",
    "inviter_signature":   "<base64 64>"
  },
  "root_key_wrap": {
    "wrapped_key":       "<base64 48>",
    "sender_box_pubkey": "<base64 32 — re-issuer's box_pubkey>",
    "nonce":             "<base64 24>"
  }
}
```

`recipient_user_id` is implicit (the URL pins it).

### Authorization (L35)

- Caller MUST be a current member.
- Caller MUST NOT be the recipient (`{user_id}` in the URL) — recipients can't self-cure.

### Server validation (atomic transaction)

1. Inviter-signature verifies under `inviter_sign_pubkey` over the `snakd-mship-v1:` scope.
2. `membership.inviter_user_id == caller.id`.
3. `membership.inviter_sign_pubkey == users.sign_pubkey WHERE id = caller`.
4. `membership.invitee_box_pubkey == users.box_pubkey WHERE id = {user_id}` and `invitee_sign_pubkey == users.sign_pubkey WHERE id = {user_id}`.
5. L30 + L39 wrap validations.
6. UPSERT (`INSERT … ON CONFLICT (channel_id, generation_id, member_user_id) DO UPDATE SET wrapped_key, sender_box_pubkey, nonce, created_at`) on the existing `channel_keys` row for the channel's current `generation_id`. Replaces the bad wrap with the new one. Replaces the `channel_members` inviter-signature columns with the re-issuer's signature so future verifiers (L22) trust the row under the re-issuer's chain.

### Rate-limit (L35)

- Dedicated `replay-wrap` per-user-per-pair bucket: burst 3 / refill 5 minutes / per `(channel_id, member_user_id)` pair.
- After 3 consecutive `wrap_failed` events on the same `(channel_id, member_user_id)` pair within 1 hour, the server disables `replay-wrap` for that pair for 24 hours and surfaces "key issuance degraded — escalate to channel admin" in the recipient's UI.
- The original inviter's ability to call `replay-wrap` is NOT restricted in v1 — the social-engineering / kick-and-re-add flow is the workaround; admin-grade restrictions are roadmap.
- Bucket lives in `apps/server/internal/ratelimit/config.go` alongside `wraps-needed-read` per L36 (same `IPLimiterConfig` shape as the existing buckets).

### L40 — replay-wrap can wrap an arbitrary key (DoS-only variant)

A malicious replay-wrap re-issuer can wrap an arbitrary 32-byte value as the "channel root key." The victim's `crypto_box_open` produces this attacker-chosen key; their `crypto_secretbox_open` calls on legitimate channel messages then fail (wrong key). Effect on the victim: persistent decryption errors. Effect on the attacker: NONE for read access (the attacker never sees the real channel key; their fake key cannot decrypt messages encrypted by other members). The attack is a DoS-only variant of L29's garbage-bytes case; the 3-failure-in-1h cool-down (L35) bounds it. Documented in [security.md](security.md).

## Cross-feature invariants (recap)

- **L5** — every wrap on every endpoint uses the same `WrapEntry` shape (`{recipient_user_id?, wrapped_key, sender_box_pubkey, nonce}`).
- **L6 + L12** — DMs never rotate; `POST /api/dms` 200 with non-empty `root_key_wraps` returns 409.
- **L7** — server NEVER inserts wrap rows of its own initiative.
- **L9** — `ChannelEvent` kinds extended to `"create" | "rename" | "members_changed" | "key_received" | "wrap_failed"` (see [encryption.md](encryption.md)).
- **L16** — rotation triggers in v1 are member-removal-only.
- **L30** — every wrap-insert handler validates `wrap.sender_box_pubkey == users.box_pubkey WHERE id = caller`.
- **L39** — every wrap-insert handler validates byte lengths (`wrapped_key == 48`, `nonce == 24`, `sender_box_pubkey == 32`, `inviter_signature == 64`, every pubkey == 32).

## Out of scope (Phase 10 v1)

- Periodic / time-based / admin-triggered key rotation. v1 rotates ONLY on member removal (L16).
- Per-user "rotate my keys now" flow. Identity-key rotation = passphrase change = full membership-key re-issue (decision log §4); no in-place "change identity passphrase" endpoint in v1.
- DM rotation. DMs never rotate (L6) — recovery is "create a new conversation."
- Server-side wrap content validation. The server cannot decrypt (L7); it only structural-validates length and `sender_box_pubkey` ownership.
