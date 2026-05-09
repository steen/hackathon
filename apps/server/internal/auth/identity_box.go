// Phase-10 X25519 keypair derivation that matches libsodium's
// crypto_box_seed_keypair byte-for-byte (decision-log §4).
//
// libsodium SHA-512s the seed first and takes bytes [0:32] as the
// X25519 scalar (with clamping). Go's `box.GenerateKey(bytes.NewReader(seed))`
// does NOT pre-hash, so it produces a different scalar — and thus a
// different pubkey — for the same seed. This file's BoxSeedKeypair is
// the only correct way to derive box keys from the HKDF-split sub-seed
// on the Go side; the TS side uses sodium.crypto_box_seed_keypair, which
// performs the same SHA-512+clamp internally.
//
// The cross-language byte-equivalence test in
// tests/e2e/phase-10/identity_vectors.test.{ts,go} pins the exact
// pubkey bytes for a known fixture; any drift fails CI.

package auth

import (
	"crypto/sha512"

	"golang.org/x/crypto/curve25519"
)

// BoxSeedKeypair returns the X25519 (pub, priv) pair derived from a 32-byte
// seed in the same way libsodium's crypto_box_seed_keypair derives it:
// SHA-512 the seed, take the first 32 bytes as the scalar, apply the
// standard X25519 clamping, then multiply by the basepoint for the pub.
//
// Panics if seed is not exactly 32 bytes — the HKDF caller always
// reads exactly 32, so a wrong length is a programmer error.
func BoxSeedKeypair(seed []byte) ([32]byte, [32]byte) {
	if len(seed) != 32 {
		panic("auth.BoxSeedKeypair: seed must be 32 bytes")
	}
	h := sha512.Sum512(seed)
	var priv [32]byte
	copy(priv[:], h[:32])
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pubBytes, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		// X25519 only errors on a low-order point; deriving from a
		// SHA-512(random)-then-clamped scalar cannot land on one.
		panic("auth.BoxSeedKeypair: curve25519.X25519 returned error: " + err.Error())
	}
	var pub [32]byte
	copy(pub[:], pubBytes)
	return pub, priv
}
