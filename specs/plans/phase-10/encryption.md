# Phase 10 — Encryption: wire types, signature scopes, KDF parameters

**Parent epic:** #975
**Decision log:** `lt -p e2e-encryption 3` §3, §4, §5, L2, L3, L5, L9, L17, L21, L23, L26, L27, L34, L37, L39
**Cross-links:** [README.md](README.md) · [membership.md](membership.md) · [keys.md](keys.md) · [security.md](security.md)

This file is the contract for the cryptographic primitives, wire types, and signature scopes used across the Phase 10 epic. Every Phase 10 sub-issue that emits or consumes encryption material points here for the byte-level shape.

## Identity model (one keypair-pair per user, per §4)

Every user publishes two long-term public keys derived deterministically from a single passphrase:

| Key             | Algorithm                          | Use                                                                            |
| --------------- | ---------------------------------- | ------------------------------------------------------------------------------ |
| `box_pubkey`    | X25519 (`crypto_box`)              | Per-recipient root-key wrapping                                                |
| `sign_pubkey`   | Ed25519 (`crypto_sign`)            | Sender authenticity + key-change detection (TOFU)                              |

Server stores only the public halves, base64-encoded as 32-byte values on the wire. Private keys exist only on devices and in the user's password-manager vault.

### Derivation pipeline (decision log §4 + L3 + L37)

1. **Identity passphrase**: ≥16 character minimum (client-side enforcement; server never sees the passphrase). 1Password / Bitwarden / Apple Passwords / Chrome inline "Suggest strong password" suffices to clear this.
2. **Salt**: `salt = SHA-256("snakd-identity:v1:" + username)[:16]`. Both clients compute identically — JS via `SubtleCrypto.digest("SHA-256", ...)` then `.slice(0, 16)`; Go via `sha256.Sum256([]byte("snakd-identity:v1:" + username))[:16]`. The username (pre-hash) is taken byte-for-byte from `users.username`; it is **already lowercase-ASCII** by the L37 registration regex (`^[a-z0-9_-]{3,32}$`). Implementations MUST NOT apply `toLowerCase()` at derivation time — that would silently mask any L37 regression.
3. **Argon2id (L3 — single named constant per language; do NOT inline at call sites)**:
    - `t = 3` iterations, `m = 64 MiB` memory, `p = 1` parallelism, `output_len = 32` bytes.
    - Single Go-side constant file (e.g. `apps/server/internal/auth/identity_params.go`) and single TS-side constant file (e.g. `packages/api-client/src/identity_params.ts`). Future cost bumps are a one-line change.
    - OWASP Password Storage Cheat Sheet (rev. 2023-11) floor is `t=2, m=19 MiB, p=1`; this profile exceeds it on memory by ~3.4× and on time by 1.5×, with `p=1` chosen because browser WebWorker parallelism is awkward and the cost is paid once per device login.
4. **HKDF-SHA256 split (RFC 5869)** — the 32-byte Argon2id output is split into two 32-byte sub-seeds with byte-identical derivations on both sides:

    ```
    box_seed  = HKDF-SHA256(root_seed, salt = empty, info = "snakd-identity:v1:box")  → 32 bytes
    sign_seed = HKDF-SHA256(root_seed, salt = empty, info = "snakd-identity:v1:sgn")  → 32 bytes
    ```

    Both clients use HKDF-SHA256 — NOT libsodium's `crypto_kdf` (which is BLAKE2b-based and would diverge from any non-libsodium implementation). JS uses `SubtleCrypto.deriveBits({name:"HKDF", hash:"SHA-256", salt: empty Uint8Array, info: utf8Encode(<info>)}, key, 256)`. Go uses `golang.org/x/crypto/hkdf.New(sha256.New, root_seed, nil /* salt */, []byte(<info>))`, then reads 32 bytes.

    The info strings MUST match byte-for-byte across clients: `"snakd-identity:v1:box"` and `"snakd-identity:v1:sgn"`. Sub-issue authors factor a `kdfInfo` constant per language alongside the Argon2id constants.
5. **NaCl seed-keypair operations**:
    - **Sign keypair (Ed25519)** — byte-equivalent across libsodium and `crypto/ed25519`. JS: `sodium.crypto_sign_seed_keypair(sign_seed)`. Go: `ed25519.NewKeyFromSeed(sign_seed)`. Empirically verified across libraries (decision log §4).
    - **Box keypair (X25519)** — NOT byte-equivalent if Go uses `box.GenerateKey(bytes.NewReader(seed))`. libsodium's `crypto_box_seed_keypair(seed)` SHA-512-hashes the seed first, then takes bytes `[0:32]` as the X25519 scalar (with clamping). The Go-side helper (e.g. `apps/server/internal/auth/identity_box.go`'s `BoxSeedKeypair(seed []byte) (pub, priv [32]byte)`) computes:

        ```go
        h := sha512.Sum512(boxSeed)
        var scalar [32]byte
        copy(scalar[:], h[:32])
        scalar[0]  &= 248
        scalar[31] &= 127
        scalar[31] |= 64
        pub, _ := curve25519.X25519(scalar[:], curve25519.Basepoint)
        ```

        JS uses `sodium.crypto_box_seed_keypair(box_seed)` directly. The cross-language byte-equivalence test fixture (Wave 2 I — User identity sub-issue AC) verifies the match.
6. **Wrong-passphrase detection**: server-stored `sign_pubkey` is the canary. After deriving on login, if `sodium.crypto_sign_seed_keypair(sign_seed).publicKey ≠ stored sign_pubkey`, the client rejects with a clear "wrong identity passphrase" error rather than silently producing a different identity.
7. **`libsodium-wrappers-sumo` async-init constraint (L27)**: the sumo build is required because `crypto_pwhash` / Argon2id only lives there. Every entry-point that exercises crypto MUST `await sodium.ready` before any API call — `apps/web/src/main.tsx` awaits sodium.ready before mounting React; `apps/cli/cmd/login.go` and `register.go` await before doing identity work; `packages/api-client/src/identity.ts` exports a `ready()` helper. Tests await ready in `beforeAll`.

### CLI parity (decision log §4 + L11)

- `chatd register` and `chatd login` prompt for the identity passphrase via `golang.org/x/term` no-echo prompt — separate from the existing login-password prompt at `apps/cli/cmd/prompt.go` (which DOES echo). The identity passphrase is treated more sensitively because it protects all past channel + DM history permanently and cannot be reset server-side; the shoulder-surfing risk justifies no-echo even though the login-password prompt does echo. Wave 2 I adds `golang.org/x/term` to `go.mod` (the CLI identity sub-issue).
- `chatd` does NOT persist the passphrase. It persists the *derived seed* at `~/.config/chatd/identity.seed` mode 0600 so day-2 invocations don't re-prompt. `chatd logout` wipes the file (extending `apps/cli/cmd/logout.go`'s existing `config.Clear(env.ConfigDir)` call). Web stores the equivalent in IndexedDB.

## Cipher suite byte (decision log §3 + L17)

```
cipher_suite = 0x01   →   naclbox-v1
```

Every message envelope and every wrap row carries `cipher_suite` explicitly. Server rejects any message body that does NOT include a `cipher_suite` byte at the structural validation layer. Server NEVER accepts a plaintext-fallback frame. Clients reject any inbound frame whose `cipher_suite ≠ 0x01` in v1. Future suites bump the byte (`0x02`, …) and add explicit support; no implicit accept-and-pass-through path exists.

## `WrapEntry` shape (decision log §7 + L5 + L30 + L39)

Every per-recipient wrap on every endpoint uses the same JSON object:

```ts
interface WrapEntry {
  recipient_user_id: string;   // ULID; OMIT only when the wrap-list singularity
                               // already pins it (e.g. POST /api/channels/{id}/members
                               // takes a single "root_key_wrap" by name).
  wrapped_key:       string;   // base64; crypto_box ciphertext = 48 bytes
                               // (32-byte payload + 16-byte Poly1305 MAC)
  sender_box_pubkey: string;   // base64; 32 raw bytes — wrapper's box_pubkey
                               // at wrap time (server-validated against
                               // users.box_pubkey for the caller per L30).
  nonce:             string;   // base64; 24 random bytes (XSalsa20 nonce)
}
```

```go
// packages/go-client/wraps.go (illustrative; sub-issue authors pick the file)
type WrapEntry struct {
    RecipientUserID string `json:"recipient_user_id,omitempty"`
    WrappedKey      string `json:"wrapped_key"`
    SenderBoxPubkey string `json:"sender_box_pubkey"`
    Nonce           string `json:"nonce"`
}
```

**Server-side length validation (L39)**: every wrap-insert path validates `len(base64-decode(wrapped_key)) == 48` and `len(base64-decode(nonce)) == 24` and `len(base64-decode(sender_box_pubkey)) == 32`. Reject with `400 "wrap_size_invalid"`. Same applies to MembershipBlock fields where applicable: `inviter_signature` is 64 bytes when non-NULL; each `*_pubkey` is 32 bytes.

**Server-side `sender_box_pubkey` validation (L30)**: every wrap-insert handler additionally validates `wrap.sender_box_pubkey == users.box_pubkey WHERE id = caller`. Reject with `400 "sender_pubkey_mismatch"`. Closes the protocol-layer arm of adversarial-review ADV-4; the modified-client-JS arm remains R1.1 residual.

## `MembershipBlock` shape (decision log §10)

The signed-membership row that lives both in `channel_members` (server-stored) and on every endpoint that creates / replays one (`POST /api/channels`, `POST /api/channels/{id}/members`, `POST /api/channels/{id}/members/{user_id}/replay-wrap`, `GET /api/channels/{id}/members/wraps-needed` response).

```ts
interface MembershipBlock {
  inviter_user_id:     string;   // ULID — who signed this row
  inviter_sign_pubkey: string;   // base64 32 — pinned at invite time so
                                 // inviter rotation does not break verification
                                 // (verifier-side TOFU history per L34)
  invitee_box_pubkey:  string;   // base64 32 — pinned at invite time;
                                 // verifier checks against the invitee's
                                 // current TOFU OR accepts-and-caches on first
                                 // contact for the invitee (per L22 step 3)
  invitee_sign_pubkey: string;   // base64 32 — pinned at invite time
  added_at:            string;   // RFC3339 UTC — covered by the signature
  inviter_signature:   string | null;
                                 // base64 64 (Ed25519); NULL ONLY for
                                 // public-channel server-auto-add (R1.2 carve-
                                 // out, see security.md). Non-NULL signatures
                                 // are required for any channel where
                                 // channels.is_public = FALSE; application-
                                 // level enforcement lives in
                                 // repo/channel_members.go per L33.
}
```

```go
type MembershipBlock struct {
    InviterUserID     string  `json:"inviter_user_id"`
    InviterSignPubkey string  `json:"inviter_sign_pubkey"`
    InviteeBoxPubkey  string  `json:"invitee_box_pubkey"`
    InviteeSignPubkey string  `json:"invitee_sign_pubkey"`
    AddedAt           string  `json:"added_at"`
    InviterSignature  *string `json:"inviter_signature"` // pointer for nullability
}
```

The full `channel_members` schema (including the partial index for NULL-sig forensic queries) is in [membership.md](membership.md). The verifier-side flow for lazy-wrap (`wraps-needed` response) is in [keys.md](keys.md) under "Lazy-wrap-on-online (L14)".

## Signature scopes

Three distinct prefixes — channel messages, DM messages, and membership rows — each binding the relevant context fields. The prefix is the **cross-protocol-confusion defense**: a channel-message ciphertext cannot be replayed as a DM (signature won't verify under the DM prefix) and vice versa, even if a malicious server swaps the wire shape.

### `snakd-msg-v1:channel:` (L21)

Channel messages: `crypto_sign_detached` over

```
b"snakd-msg-v1:channel:" || channel_id || b"|" || message_id || b"|" || sender_user_id ||
b"|" || u32_be(key_generation_id) || b"|" || u8(cipher_suite) || b"|" || nonce (24) ||
b"|" || ciphertext || b"|" || client_created_at_rfc3339
```

### `snakd-msg-v1:dm:` (L21)

DM messages: identical shape but with prefix `b"snakd-msg-v1:dm:"` and `target_id = conversation_id`:

```
b"snakd-msg-v1:dm:" || conversation_id || b"|" || message_id || b"|" || sender_user_id ||
b"|" || u32_be(key_generation_id) || b"|" || u8(cipher_suite) || b"|" || nonce (24) ||
b"|" || ciphertext || b"|" || client_created_at_rfc3339
```

`client_created_at` is signed so a rogue server cannot forge ordering inside a conversation. The unsigned, server-stamped `created_at` exists as a delivery timestamp for display only; clients show both when they diverge.

### `snakd-mship-v1:` (decision log §10)

Membership rows: `crypto_sign_detached` over

```
b"snakd-mship-v1:" || channel_id || b"|" || user_id || b"|" || inviter_user_id ||
b"|" || inviter_sign_pubkey || b"|" || invitee_box_pubkey || b"|" || invitee_sign_pubkey ||
b"|" || added_at_rfc3339
```

`inviter_user_id` and `inviter_sign_pubkey` are signed too: this stops a rogue server from swapping who-the-inviter-is or substituting a different pinned pubkey to bypass TOFU at lazy-wrap-verification time. The signature is self-describing about which keypair signed it.

### Self-bootstrap carve-out (decision log §10)

Self-signing (`inviter_user_id == user_id`, `inviter_sign_pubkey == invitee_sign_pubkey == caller's current sign_pubkey`, `invitee_box_pubkey == caller's current box_pubkey`) is permitted iff `channel_members` for the target channel has zero rows at the moment the call is processed (= the user is becoming the SOLE INITIAL MEMBER). The `is_public` flag is irrelevant to this carve-out — both public-channel first-user-bootstrap (per [keys.md](keys.md) bootstrap mode) and private-channel creator-bootstrap are covered by the same zero-existing-members rule. Outside this window (= the channel already has at least one member), self-signing is rejected.

## Encrypted message envelope (L21 — Channel + DM)

Channel `Message`:

```ts
interface Message {
  id:             string;     // ULID; server-assigned
  channel_id:     string;
  sender_user_id: string;
  envelope: {
    cipher_suite:        1;            // 0x01 = naclbox-v1; lockstep with cipher_suite byte
    key_generation_id:   number;       // u32; per-channel monotonic
    nonce:               string;       // base64 24 (XSalsa20)
    ciphertext:          string;       // base64; secretbox(plaintext, nonce, channel_root_key)
    sender_sign_pubkey:  string;       // base64 32 — signer's CURRENT sign_pubkey
    signature:           string;       // base64 64 — Ed25519 over the snakd-msg-v1:channel: scope
    client_created_at:   string;       // RFC3339 UTC; client-stamped, signed
  };
  created_at:     string;     // RFC3339 UTC; server-stamped, NOT signed (display-only)
}
```

DM `DMMessage`: identical envelope but the parent fields are `conversation_id` instead of `channel_id`:

```ts
interface DMMessage {
  id:              string;
  conversation_id: string;
  sender_user_id:  string;
  envelope:        Envelope;   // same shape — see snakd-msg-v1:dm: scope above
  created_at:      string;
}
```

`body: string` is removed — there is no plaintext fallback path. Wave 5 (E) lands the cutover (L26 — every consumer of `.body` updates in the same PR).

```go
// packages/go-client/messages.go (illustrative)
type Envelope struct {
    CipherSuite       byte   `json:"cipher_suite"`
    KeyGenerationID   uint32 `json:"key_generation_id"`
    Nonce             string `json:"nonce"`
    Ciphertext        string `json:"ciphertext"`
    SenderSignPubkey  string `json:"sender_sign_pubkey"`
    Signature         string `json:"signature"`
    ClientCreatedAt   string `json:"client_created_at"`
}

type Message struct {
    ID            string   `json:"id"`
    ChannelID     string   `json:"channel_id"`
    SenderUserID  string   `json:"sender_user_id"`
    Envelope      Envelope `json:"envelope"`
    CreatedAt     string   `json:"created_at"`
}

type DMMessage struct {
    ID             string   `json:"id"`
    ConversationID string   `json:"conversation_id"`
    SenderUserID   string   `json:"sender_user_id"`
    Envelope       Envelope `json:"envelope"`
    CreatedAt      string   `json:"created_at"`
}
```

Mirrored in `packages/api-client/src/types.ts` and `packages/go-client/{messages,dms}.go` per CLAUDE.md "Wire types".

### Nonce-collision risk

24-byte (192-bit) random nonces. The birthday bound for 50 % collision probability is ~2^96 messages under one key. At 10^6 lifetime messages under one channel root key (10× a 5–15-person friend group across many years), collision probability ≈ 10^-43. Negligible at any plausible deployment scale.

## `ChannelEvent` kinds — Phase-10 extensions (L9 + L29)

Phase 10 extends `ChannelEventKind` from `"create" | "rename"` (Phase 8) to:

```ts
type ChannelEventKind =
  | "create"            // Phase 8 — unchanged
  | "rename"            // Phase 8 — unchanged
  | "members_changed"   // Phase 10 — fired after a successful POST/DELETE on
                        // /api/channels/{id}/members; recipients race to
                        // rotate or compute lazy-wrap fill-ins
  | "key_received"      // Phase 10 — fanned out to the receiving user's
                        // user:<viewer> topic when a wrap arrives
  | "wrap_failed";      // Phase 10 — fired to the user's own user:<viewer>
                        // topic when crypto_box_open on their wrap fails;
                        // recovery path is replay-wrap (L29 + L35)
```

Discriminated-union `data` shape (decision log L9):

```ts
type ChannelEventData =
  | { kind: "create";          channel: Channel }
  | { kind: "rename";          channel: Channel }
  | { kind: "members_changed"; channel_id: string; current_generation_id: number; members_at_rotation: User[] }
  | { kind: "key_received";    channel_id: string; generation_id: number }
  | { kind: "wrap_failed";     channel_id: string; generation_id: number };
```

Both clients (`packages/api-client/src/types.ts`, `packages/go-client/ws.go`) update under CLAUDE.md "Wire types" with mirrored e2e drift assertion in the same PR (L10). The Go side may continue the existing typed-channel + passthrough-rest pattern from Phase 9 (DM/Read are passthrough); sub-issue Wave 1 (T — wire types) picks the implementation shape.

## `User` wire-type extensions (L2)

`POST /api/auth/register` body adds `box_pubkey: string` and `sign_pubkey: string` (each base64-encoded 32-byte). Login response (`POST /api/auth/login`), `GET /api/auth/me`, and the `User` wire type ALL return both pubkeys for client-side seed validation:

```ts
interface User {
  id:           string;
  username:     string;
  box_pubkey:   string;     // base64 32; server-stored, public
  sign_pubkey:  string;     // base64 32; server-stored, public
}
```

The mirroring rule from CLAUDE.md "Wire types" applies: change `packages/go-client/{auth,users}.go` and `packages/api-client/src/types.ts` in the same PR as the server change, with an e2e drift assertion (L10).

## Out of scope (Phase 10 v1)

- Per-message forward secrecy (decision log §5: static root key per channel/per DM is the chosen group-key model; epoch-rotated keys / Signal double-ratchet are deferred).
- HPKE-style ciphersuite negotiation. `cipher_suite = 0x01` is the only suite in v1; `0x02+` is future work.
- QR-code safety-number / out-of-band fingerprint verification. v1 ships TOFU + key-change warning only (L20). See [security.md](security.md).
- Admin-attested signing of pubkeys. Not in v1; if the trust model evolves toward zero-trust-host, this is the hardening path.
- Periodic / time-based / admin-triggered key rotation. v1 rotates ONLY on member removal (L16); see [keys.md](keys.md).
- Metadata encryption. v1 leaves message metadata (sender, target, timestamp, channel name, member roster) plaintext — see [security.md](security.md) and L19.
