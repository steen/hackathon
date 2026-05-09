### Added

- Phase 10 user-identity keypair derivation (`#979`): every user now publishes an X25519 `box_pubkey` and an Ed25519 `sign_pubkey` derived deterministically from an identity passphrase via Argon2id (t=3, m=64 MiB, p=1) → HKDF-SHA256 split (`info="snakd-identity:v1:box"` / `:sgn"`) → `crypto_box_seed_keypair` + `crypto_sign_seed_keypair`. The salt is `SHA-256("snakd-identity:v1:" + username)[:16]` (decision-log §4 + L3). Server-side helpers in `apps/server/internal/auth/{identity_params,identity_box}.go` mirror `packages/go-client/identity.go` and `packages/api-client/src/{identity_params,identity}.ts` byte-for-byte; the Go-side `BoxSeedKeypair` does the libsodium-equivalent SHA-512+clamp+X25519 dance so it matches `crypto_box_seed_keypair` (NOT `box.GenerateKey(bytes.NewReader)`, per BG-C5-3). `tests/e2e/phase-10/identity_vectors.test.{ts,go}` pins the cross-language byte-equivalence anchor for `passphrase="correct horse battery staple"`, `username="alice"`.

### Changed

- `POST /api/auth/register` accepts and persists `box_pubkey` / `sign_pubkey` (base64 of raw 32 bytes each); `POST /api/auth/login` and `GET /api/auth/me` echo them back so clients can verify the wrong-passphrase canary (decision-log L4) before any sensitive operation. `GET /api/users` also returns the columns when populated.
- Username regex tightened from `^[A-Za-z0-9_-]{3,32}$` to `^[a-z0-9_-]{3,32}$` (decision-log L37) so the salt input (`SHA-256("snakd-identity:v1:" + username)[:16]`) is byte-identical on both sides of the wire. Mixed-case usernames now return `400` with a clear error.
- `chatd register` and `chatd login` add an identity-passphrase prompt (`golang.org/x/term` no-echo); the derived 32-byte root seed is persisted at `~/.config/chatd/identity.seed` mode 0600 (decision-log L11). `chatd logout` extends `config.Clear` to also wipe the seed.
- Web: `apps/web/src/main.tsx` awaits `sodium.ready` before mounting React (decision-log L27); `apps/web/vite.config.ts` raises `chunkSizeWarningLimit` to 600 KB to accommodate the `libsodium-wrappers-sumo` chunk; the seed is persisted in IndexedDB via `apps/web/src/lib/identityStore.ts`. The Login form re-derives identity locally and rejects with a clear error when the derived `sign_pubkey` does not match the server-stored canary.
- `packages/api-client` and `apps/web` add `libsodium-wrappers-sumo` (the standard `libsodium-wrappers` build omits `crypto_pwhash` / Argon2id).

### Out of scope

- Key wrapping or message encryption (#7, #8) and TOFU caching (#11).
- In-place "change identity passphrase" endpoint (deferred per decision-log §4).
