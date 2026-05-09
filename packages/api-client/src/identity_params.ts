// Phase-10 identity-derivation constants. Mirrors
// apps/server/internal/auth/identity_params.go byte-for-byte; bumping
// any constant here without the matching Go change breaks the cross-
// language equivalence test in tests/e2e/phase-10/identity_vectors.test.{ts,go}.
//
// Decision-log L3 (`lt -p e2e-encryption 3`): named constants,
// single-source-per-language so a future cost bump is one line.

// Argon2id parameters per decision-log §4 + L3.
//
//   - opsLimit: 3 iterations
//   - memLimit: 64 MiB (libsodium reports memory in bytes, so the TS-
//     side passes 64 * 1024 * 1024; the Go-side argon2.IDKey wants
//     KiB so the Go constant is 65536. Both arithmetic forms are
//     spelled out at the call site to make the unit explicit.)
//   - parallelism: 1 (libsodium's crypto_pwhash hard-codes p=1; the
//     constant is here so the Go side stays in lockstep).
//   - keyLen: 32 bytes (the root_seed fed into HKDF).
export const ARGON_TIME = 3;
export const ARGON_MEMORY_BYTES = 64 * 1024 * 1024;
export const ARGON_THREADS = 1;
export const ARGON_KEY_LEN = 32;

// HKDF-SHA256 info strings per decision-log §4. Byte-for-byte identical
// across both clients — these go into the HKDF `info` parameter when
// splitting the Argon2id root seed.
export const HKDF_INFO_BOX = "snakd-identity:v1:box";
export const HKDF_INFO_SIGN = "snakd-identity:v1:sgn";

// SaltPrefix is the constant prefix combined with the username before
// SHA-256 + truncation produces the 16-byte Argon2id salt. Both clients
// MUST use the username byte-for-byte (already lowercase-ASCII per the
// L37 registration regex).
export const SALT_PREFIX = "snakd-identity:v1:";

// SaltLen is libsodium's crypto_pwhash_SALTBYTES. The salt is SHA-256
// over (SALT_PREFIX + username) truncated to this length.
export const SALT_LEN = 16;

// Client-side minimum identity passphrase length (decision-log §4).
// The server never sees the passphrase.
export const MIN_IDENTITY_PASSPHRASE_LEN = 16;
