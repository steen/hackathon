# Phase 10 — Security: threat model, residual risks, recovery flows

**Parent epic:** #975
**Decision log:** `lt -p e2e-encryption 3` §1, §2, §3, §4, §6, §9, §10, L17, L18, L19, L20, L29, L34, L35, L37, L40, L41, R1.1, R1.2
**Cross-links:** [README.md](README.md) · [encryption.md](encryption.md) · [membership.md](membership.md) · [keys.md](keys.md)

This file is the user-facing security doc for Phase 10. It states the threat model, names the accepted residual risks (R1.1 + R1.2), explains why metadata is unencrypted, and walks through the recovery flows for the operationally-visible failure modes (wrong identity passphrase, wrap failure, migration). The decision log is the source of truth; this doc is the human-readable reference downstream sub-issues cite for trust-UX copy and security-doc landing pages.

## Threat model (decision log §1 — option C)

In scope as adversaries:

- **Operator with full server access.** DB snapshot, live process access, RAM access. Can serve modified server-side JS or a modified `chatd` Go binary. Can swap published pubkeys at upload time. Can MITM the application protocol layer.
- **Passive network attacker outside the deploy.** TLS / WSS termination is mandated for any non-loopback deployment; the message-payload ciphertext layer is the primary defense, TLS is the second.

Out of scope as adversaries:

- **Compromised end-user device.** Once a member's machine is owned, that member's plaintext history is gone — no design defends against this for messages they could read in plaintext.
- **Nation-state breaking modern primitives.**
- **Side channels (timing, power) on the host.**

Implications:

- Need a trust-anchor mechanism for member pubkeys → TOFU + key-change warning UX (L20). Out-of-band fingerprint verification (QR safety numbers) is roadmap.
- Need protection against downgrade ("client says it doesn't support encryption") → no plaintext fallback at any layer (L17 + L21). Server rejects any message body without a `cipher_suite` byte; clients reject any inbound frame whose `cipher_suite ≠ 0x01` in v1.
- Mandate TLS in deploy docs + reject `CHAT_ALLOW_PUBLIC_BIND=1` without a TLS terminator declaration (decision log §1 — concrete enforcement TBD as a follow-up question).
- The trust UX is real client-side surface. The friend-group context (5-15 people who know each other, per `specs/PRD.md` §3) makes manual verification realistic.

## Named residual risks (accepted for v1)

These are the explicitly-surfaced residual risks the v1 design does NOT defend against. Each is documented rather than silently inherited (per cold-pass cycle-1 BG-4).

### R1.1 — Modified server-delivered client

A rogue operator can serve modified web JS or a modified `chatd` Go binary that:

- Exfiltrates the identity passphrase at registration / login time (the passphrase is typed into a browser field or piped via `golang.org/x/term`).
- Fakes the `sign_pubkey` validation at restore time (so a different identity is silently produced for the user).
- Poisons the TOFU cache at first contact: per L20, the receiver caches whatever `sender_sign_pubkey` is in the first message they see. Modified JS that rewrites that field on first contact installs the operator's pubkey under the legitimate user's name; subsequent real messages from the legitimate user trigger the TOFU mismatch warning, but the receiver's initial trust is already misdirected.

R1.1 also includes (per L40) the protocol-layer arm: a rogue server with DB write access can swap stored `sender_sign_pubkey` + `signature` columns on a message envelope, achieving the same TOFU-poisoning effect as modified-JS attacks. Both paths require the operator's privileged access.

Defenses (reproducible builds, pinned client checksums, code-signing, out-of-band fingerprint comparison via QR / safety numbers) are **out of scope for v1**. The encryption design defends against operator-with-DB+RAM access; it does NOT defend against operator-modifying-client.

> **User-facing copy** (security doc + first-run modal): "If you don't trust the person hosting your server, you cannot fully trust this app — the server delivers the JavaScript that does the encryption. The encryption defends against an operator who can read the database; it does not defend against one who can replace the website. For the friend-group case (the host is one of the friends), this is the same trust assumption you make every time you DM each other on Discord."

### R1.2 — Operator-as-public-channel-member

An operator who registers using their own `CHAT_INVITE_CODE` becomes a member of every channel marked `is_public = TRUE` and can read its messages once any existing member's client lazy-wraps for them (per [keys.md](keys.md) lazy-wrap-on-online flow).

For PUBLIC channels, the auto-add carries a NULL `inviter_signature` which lazy-wrap clients accept by design (the property of public channels — anyone holding an account can read).

For PRIVATE channels (`is_public = FALSE`), §10 inviter-signed membership prevents the operator from injecting themselves: any forged `channel_members` row has no valid signature, lazy-wrap clients refuse to compute the wrap, and the operator cannot decrypt. Server-side L33 enforcement is the second layer (private channels reject NULL `inviter_signature` on INSERT).

> **User-facing copy** (channel-create modal helper text): "Visible to all friends (the person hosting the server can read). Use private channels for content you don't want the host to read. Anything in `#general` or any other public channel is readable by whoever runs the server. This is the same property as Slack/Discord; we say it out loud."

The argument for accepting this: `CHAT_INVITE_CODE` already gates registration, the host is conventionally one of the friends, and the cost of the strict-§1 alternative (per-user approval gate, "P3" in decision log §9) was judged too friction-heavy for the friend-group target persona. Roadmap: if the trust model evolves toward zero-trust-host (e.g. running on a rented VPS), shipping P3 is the hardening path.

## Metadata exposure is an accepted residual (L19)

Server-stored metadata that a rogue operator can read in v1:

- `users.id`, `users.username`, `users.box_pubkey`, `users.sign_pubkey`.
- `channels.id`, `channels.name`, `channels.is_public`, `channels.created_at`.
- `channel_members.*` (the full member roster of every channel).
- `channel_keys.*` (recipient identity + wrapped-key ciphertext, but NOT the unwrapped key).
- `dm_conversations.*` (peer pairs + `last_message_at` denormalized columns).
- `dm_conversation_keys.*` (same shape as `channel_keys` but no generation column).
- `messages.*` envelope columns (sender, channel, timestamp, ULID — the latter encodes time).
- `dm_messages.*` envelope columns (sender, conversation, timestamp).
- `auth_events.*` (register/login/logout audit trail).
- Presence join/leave events (in-process only, but observable in RAM).
- `channel_reads.*` and `dm_reads.*` (read pointers).

NONE of this is encrypted in v1. The threat model defends MESSAGE CONTENT only — not the social graph.

> **User-facing copy** (security doc): "An operator who captures the database can see who talks to whom, when, and how often. They cannot read what is said in private channels or DMs. If hiding the social graph itself is a requirement, this design does not meet it."

If metadata-encryption becomes a requirement, MLS / Signal Sealed Sender / private-information-retrieval are the roadmap families to evaluate; SQLCipher does NOT solve this (operator holds the key). Decision log §1 / L19.

## Trust UX in v1: TOFU + key-change warning (L20 + L34)

First time the client sees a peer's `box_pubkey` / `sign_pubkey`, store them locally indexed by `user_id`. On every subsequent live message receive, check the sender's `sign_pubkey` on the message envelope against the locally-cached value for that `user_id`:

- **Match** → silent.
- **Mismatch** → display a prominent warning ("Bob's identity key has changed. This may be a key rotation, or it may be an attacker. Verify with Bob via another channel before trusting messages.").

The warning is rendered inline in the message list AND as a modal on first encounter. NO QR-code safety-number UX in v1. NO admin-attested signing in v1. Both are roadmap items if the trust model evolves.

CLI prints the same warning to stderr (`chatd watch`, `chatd dm watch`, `chatd history`).

### Cache locations

- **Web** — IndexedDB, scoped per logged-in user.
- **CLI** — `~/.config/chatd/known_keys.json`, mode 0600 (matches the `~/.config/chatd/identity.seed` precedent in L11).

### L34 — split rules: live vs. pinned-signature artifacts

L34 amends the simple "first-contact-pin" rule: the TOFU cache stores a list of all `(box_pubkey, sign_pubkey, first_seen_at, last_seen_at)` entries ever observed for each `user_id`, and verification rules differ by use:

- **Live-message signature verification** (`Message.envelope.signature` per [encryption.md](encryption.md) L21): verifier accepts the signature ONLY if `sender_sign_pubkey` matches the user's MOST-RECENT-SEEN entry (the current key). A signature from an old key on a new live message means the sender is using stale identity → reject. The "key changed" warning UI fires when an inbound message uses a `sender_sign_pubkey` not equal to the most-recent entry.
- **Pinned-signature artifact verification** (`channel_members.inviter_signature` over `inviter_sign_pubkey`): verifier accepts if the pinned pubkey matches ANY historical entry for `inviter_user_id`. This handles inviter rotation (decision log §10 GAP-4): the membership row was signed under the inviter's pre-rotation key, which is preserved in the historical TOFU. UI shows the two pubkeys side-by-side ("this membership was signed under A's older key Y; A currently uses key X") on demand.

This split prevents the verification-round NEW-3 attack (poisoned first-contact pubkey caching forever-validating live messages from the attacker). The poisoned key, once superseded by the legitimate key (or replaced via a logout/re-login refresh), no longer validates live messages — only stays valid for any historical artifacts that were pinned under it.

Storage is small: each rotation appends one row; a 5–15-person friend group with infrequent rotation accumulates at most tens of rows per peer.

## L41 — late-joiner-of-late-joiner liveness

When a verifier C's TOFU history is empty for an inviter A who invited B earlier, C SKIPS the wrap and some other online member (with A in their TOFU) eventually fills it. In a friend group with active chat this resolves quickly (every active member has all peers in TOFU after a single message exchange).

On a brand-new server with three users registered before the first message, this is a temporary deadlock until any member sends a message. UI shows "waiting for key…" per L14. Documented as expected behaviour, not a bug. Per [keys.md](keys.md) lazy-wrap section.

## Recovery flows

### Wrong identity passphrase (decision log §4)

The server-stored `sign_pubkey` is the canary. After deriving from the entered passphrase, the client compares `sodium.crypto_sign_seed_keypair(sign_seed).publicKey` against the user's stored `sign_pubkey`. Mismatch → reject with a clear error before doing anything that would publish the wrong identity.

User-facing message: "Wrong identity passphrase. This passphrase produces a different identity than the one on file for `<username>`. Try again, or — if you've genuinely forgotten the passphrase — see 'Recover from a forgotten identity passphrase' below."

### Forgotten identity passphrase

There is **no server-side recovery path**. Forgotten passphrase = account-wipe (decision log §4): the user re-registers under a fresh username, OR an admin manually deletes the row and the user re-registers under the same username with a new identity. Either way:

- Pre-existing channel + DM history under the old identity is unreadable forever.
- Other members' TOFU caches will reject the new identity's signatures as a "key changed" warning (L20). Users coordinate the rotation out-of-band: "I lost my passphrase, the new key is X" in any channel they can already write to.

This is a deliberate property of the design — server-side recovery would require server-side custody of key material, defeating the threat model.

### Cross-device passphrase change (decision log §4)

After a user rotates their identity passphrase on any client, ALL their other authenticated sessions (other browser, CLI) hold a stale derived seed. On next login on those sessions:

- Wrong-passphrase detection (above) catches the mismatch.
- Surfaces "Identity has changed — log out and log back in with the new passphrase to re-derive."

CLI: `chatd login` on a fresh seed-less state (after `chatd logout` wipes `identity.seed`) re-prompts and re-derives. Web: same flow via the login screen.

### Wrap failure recovery (L29 + L35 — see [keys.md](keys.md))

When `crypto_box_open` fails on a wrap row a member receives — either because the inviter sent garbage bytes (rogue inviter) OR the wrap is corrupted in transit (no IV, but raw bit-flips would have failed the Poly1305 MAC) — the recipient's client emits `{type:"channel", data:{kind:"wrap_failed", channel_id, generation_id}}` to its own `user:<viewer>` topic. UI surfaces "Channel key from <inviter> appears corrupted — ask another member to re-issue."

Recovery: any current member (other than the recipient) calls `POST /api/channels/{id}/members/{user_id}/replay-wrap` with a fresh `MembershipBlock` and a new wrap. See [keys.md](keys.md) for the full request shape, server validation, and rate-limit / cool-down rules.

L40 acknowledges replay-wrap can wrap an arbitrary key (DoS-only variant — the attacker never sees the real channel key). The 3-failure-in-1h cool-down (L35) bounds the impact.

### Wipe-and-reset migration (L18)

This repo has no production deployment (per memory `project_no_production_deployment.md`). All DBs are dev/test data. The encryption-epic migration drops every existing message, channel-key, and DM column from `messages` / `dm_messages` and replaces them with the L23 envelope columns ([encryption.md](encryption.md)). New schema applies to a fresh `chat.db`.

**Boot guard (must be implemented in this epic, not a future PR)**: in `apps/server/boot.go`'s `openAndMigrate`, BETWEEN `appdb.Open()` and `appdb.Apply()` (so the on-disk schema is still the pre-migration one), execute `PRAGMA table_info(messages)`. If the `cipher_suite` column is ABSENT (= pre-encryption schema present) AND `SELECT COUNT(*) FROM messages > 0` (ditto for `dm_messages`), abort startup with the error:

> `chat.db contains plaintext messages from a pre-encryption build. Encryption is destructive — back up the file if you care, then run 'rm chat.db chat.db-wal chat.db-shm' and restart.`

If `cipher_suite` is present (post-migration DB) OR if the column is absent and counts are 0 (fresh DB), proceed to `appdb.Apply()` normally.

SQLite WAL mode produces three files; README + epic body must list all three (`chat.db`, `chat.db-wal`, `chat.db-shm`) in wipe instructions. Test fixtures + e2e suite reset DBs per-run, so test surface impact is zero.

## Adversarial-input handling at the protocol level (L17)

Decision log L17 fixes the protocol-layer defenses; reproduced here for the user-facing security doc.

- **Nonces**: 24-byte random per send; collision probability negligible at message volume. Server stores; receivers verify by `crypto_secretbox_open` (which fails if the nonce is wrong). Server rejects messages with nonces shorter or longer than 24 bytes at the validation layer (per [encryption.md](encryption.md) L39).
- **Replay**: detected by Ed25519 signature on `(channel_id || message_id || ciphertext || nonce || sender_user_id || ...)` — see [encryption.md](encryption.md) signature scopes. Receivers verify the signature using the sender's `sign_pubkey` (server-stored, fetched once and cached). Replayed messages have either a duplicate `message_id` (server PK rejects) or a forged `message_id` (sig verifies under wrong sender or fails entirely). No timestamp dependency.
- **Cross-protocol confusion**: distinct signature prefixes for channel vs. DM messages (`snakd-msg-v1:channel:` vs. `snakd-msg-v1:dm:`). A channel-message ciphertext cannot be replayed as a DM (signature won't verify under the DM prefix) and vice versa, even if a malicious server swaps the wire shape.
- **Downgrade**: `cipher_suite` byte is REQUIRED in every message envelope. Server rejects any message body that does not include a `cipher_suite` byte. Server NEVER accepts a plaintext-fallback frame. Clients reject any inbound frame whose `cipher_suite != 0x01` (in v1). Future suites bump the byte and add explicit support.
- **Malformed envelope**: server validates structurally (required fields present, base64 decodes, lengths sane) at the handler boundary; rejects with 400 + an error envelope. The server does NOT attempt to decrypt (it has no key) — it only structural-validates. Clients reject envelopes whose `crypto_secretbox_open` fails or whose `crypto_sign_open` fails. Bad-envelope failure on the receiving side surfaces as `{type:"error"}` WS frame; the client logs and skips, does not crash.
- **Body cap**: existing 4 KiB body cap applies to plaintext bodies pre-encryption, NOT to ciphertext envelopes. Server cannot inspect plaintext, so a paranoid attacker could send up-to-16-KiB ciphertexts; but the REST 16 KiB cap from PRD §9 still applies to the request body. Client-side: enforce the 4 KiB plaintext check before encrypting.

## L37 — usernames are lowercase ASCII only (CRYPTO-1 defense)

Registration regex tightens to `^[a-z0-9_-]{3,32}$` (currently `[A-Za-z0-9_-]`). Server rejects any registration with mixed-case username with 400 + clear error. Migration also adds `COLLATE NOCASE` to `users.username`.

The defense closes a CRYPTO-1 derivation collision: §4's Argon2id salt is `SHA-256("snakd-identity:v1:" + username)[:16]`. If two users `Bob` and `BoB` could co-exist (different rows, same lowercased salt would re-collide), they would derive identical box and sign keypairs — `BoB` could decrypt every channel `Bob` is in and sign messages indistinguishable from `Bob`. Lowercase-only at the regex level eliminates the entire class. Implementations MUST NOT apply `toLowerCase()` at derivation time — that would silently mask any L37 regression in registration validation; the byte-for-byte salt input must come straight from `users.username` ([encryption.md](encryption.md)).

## What this design does NOT defend against (recap)

1. Compromised end-user device (out of threat model).
2. Modified server-delivered client (R1.1 — accepted residual).
3. Operator reading public channels (R1.2 — accepted residual).
4. Metadata exposure (L19 — accepted residual).
5. Forgotten identity passphrase (no recovery path; account-wipe by design).
6. Forward secrecy after device compromise (L16 — rotation triggers in v1 are member-removal-only; no periodic FS).
7. Group-DM hiding (DMs are 1:1 only; no group-DM design in v1; the wrap-list shape is forward-compat).
8. QR safety-number / out-of-band fingerprint UX (L20 — TOFU + key-change warning only).

Each of these has a roadmap entry if the trust model evolves; none is silently inherited.

## Out of scope (Phase 10 v1)

- Reproducible builds / signed binaries (R1.1 mitigation — roadmap).
- Admin-attested signing of pubkeys.
- Out-of-band safety-number / QR verification UX (L20).
- Periodic / time-based / admin-triggered key rotation (L16).
- Metadata encryption (L19).
- Server-side passphrase recovery.
- Per-user "rotate my keys now" without re-registration.
- Group DMs (>2 participants).
- TLS-terminator enforcement at boot (decision log §1 — surfaced as a follow-up question; v1 documents the mandate).
