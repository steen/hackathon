### Phase 10 — Atomic key wraps on create / invite / DM-create (#982)

The Phase-10 wrap surface ships its first slice: per-recipient root-key wraps now
ride along with channel-create, channel-invite, and DM-create handlers, and the
server validates them inside the same transaction as the membership row so the
L7 invariant (`channel_members ⇔ channel_keys`) holds atomically.

- `POST /api/channels` accepts an opt-in `(channel_id, membership, root_key_wraps)`
  block; when supplied, the server verifies the §10 inviter signature, the L30
  sender-pubkey ownership, and the L39 byte-lengths, then inserts channel +
  member + wrap atomically. The legacy bare-body path is preserved for
  pre-Phase-10 clients (#984's lazy-wrap fills the missing wrap later).
- `POST /api/channels/{id}/members` now requires both a §10-signed
  `MembershipBlock` and a `root_key_wrap` for private channels. Public channels
  keep the auto-fill path (NULL signature, no wrap row).
- `POST /api/dms` 201 path accepts an optional 2-entry `root_key_wraps` list and
  inserts both `dm_conversation_keys` rows atomically; the 200 idempotent
  re-call rejects re-supplied wraps with 409 `wraps_already_set` per L6 + L12.
- New `auth.VerifyMembershipSignature` implements the §10 `snakd-mship-v1:`
  signature scope; validation lives in one place so the future replay-wrap and
  lazy-wrap-on-online verifiers share the implementation.
- The #981 channel-membership e2e suite is rewritten to expect the new wrap
  body field and to sign membership blocks per §10.
