// Phase-10 identity-derivation constants. Mirrors
// packages/api-client/src/identity_params.ts byte-for-byte; bumping any
// constant here without the matching TS change breaks the cross-language
// equivalence test in tests/e2e/phase-10/identity_vectors.test.{ts,go}.
//
// Decision-log L3 (`lt -p e2e-encryption 3`): named constants,
// single-source-per-language so a future cost bump is one line.

package auth

// Argon2id parameters per decision-log §4 + L3.
//
//   - Time: t = 3 iterations
//   - Memory: m = 64 MiB (libsodium reports memory in bytes; argon2.IDKey
//     in `golang.org/x/crypto/argon2` reports it in KiB so the Go-side
//     constant is 65536, while the TS-side `crypto_pwhash` call passes
//     `64 * 1024 * 1024` as the memlimit byte count.)
//   - Parallelism: p = 1
//   - Output length: 32 bytes (the root_seed fed into HKDF).
const (
	ArgonTime    = 3
	ArgonMemory  = 64 * 1024
	ArgonThreads = 1
	ArgonKeyLen  = 32
)

// HKDF-SHA256 info strings per decision-log §4. Byte-for-byte identical
// across both clients — these are the strings that go into the HKDF
// `info` parameter when splitting the Argon2id root seed.
const (
	HKDFInfoBox  = "snakd-identity:v1:box"
	HKDFInfoSign = "snakd-identity:v1:sgn"
)

// SaltPrefix is the constant prefix combined with the username before
// SHA-256 + truncation produces the 16-byte Argon2id salt. Both clients
// MUST use the username byte-for-byte (already lowercase-ASCII per the
// L37 registration regex).
const SaltPrefix = "snakd-identity:v1:"

// SaltLen is libsodium's crypto_pwhash_SALTBYTES. The salt is SHA-256
// over (SaltPrefix + username) truncated to this length.
const SaltLen = 16
