// Phase-10 identity derivation: passphrase + username → (box, sign)
// keypairs. Mirrors packages/api-client/src/identity.ts and the server-
// side helpers in apps/server/internal/auth/identity_box.go +
// identity_params.go byte-for-byte.
//
// Decision-log §4 + L3 + L37 (`lt -p e2e-encryption 3`). The cross-
// language byte-equivalence test in
// tests/e2e/phase-10/identity_vectors.test.{ts,go} pins the exact
// pubkey bytes for a known fixture.
//
// This package level is intentional: the CLI cannot import the server's
// internal/auth package, but both the CLI and the e2e tests need a
// shared derivation. go-client is the next-narrowest non-internal home
// for it.

package goclient

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// Argon2id parameters per decision-log §4 + L3. Mirror
// apps/server/internal/auth/identity_params.go and
// packages/api-client/src/identity_params.ts byte-for-byte.
const (
	IdentityArgonTime    = 3
	IdentityArgonMemory  = 64 * 1024 // KiB; libsodium expects this in bytes (64*1024*1024)
	IdentityArgonThreads = 1
	IdentityArgonKeyLen  = 32

	IdentityHKDFInfoBox  = "snakd-identity:v1:box"
	IdentityHKDFInfoSign = "snakd-identity:v1:sgn"

	IdentitySaltPrefix = "snakd-identity:v1:"
	IdentitySaltLen    = 16

	MinIdentityPassphraseLen = 16
)

// Identity bundles the four keys + intermediate seeds. BoxPriv and
// SignPriv stay on the device; BoxPub and SignPub are submitted on
// register and verified on login.
type Identity struct {
	RootSeed [32]byte
	BoxSeed  [32]byte
	SignSeed [32]byte
	BoxPub   [32]byte
	BoxPriv  [32]byte
	SignPub  ed25519.PublicKey
	SignPriv ed25519.PrivateKey
}

// ErrIdentityPassphraseTooShort is returned by DeriveIdentity when the
// supplied passphrase is below MinIdentityPassphraseLen.
var ErrIdentityPassphraseTooShort = errors.New("identity passphrase must be at least 16 characters")

// IdentitySalt computes SHA-256(SaltPrefix + username), truncated to
// IdentitySaltLen (libsodium's crypto_pwhash_SALTBYTES). Username is
// taken byte-for-byte; the L37 regex constrains it to lowercase ASCII
// so no normalisation is applied here.
func IdentitySalt(username string) []byte {
	sum := sha256.Sum256([]byte(IdentitySaltPrefix + username))
	out := make([]byte, IdentitySaltLen)
	copy(out, sum[:IdentitySaltLen])
	return out
}

// BoxSeedKeypair returns the X25519 (pub, priv) pair derived from a
// 32-byte seed in the same way libsodium's crypto_box_seed_keypair
// derives it: SHA-512 the seed, take the first 32 bytes as the X25519
// scalar (with clamping). NOT byte-equivalent to
// box.GenerateKey(bytes.NewReader(seed)) — that API does NOT pre-hash.
func BoxSeedKeypair(seed []byte) ([32]byte, [32]byte) {
	if len(seed) != 32 {
		panic("goclient.BoxSeedKeypair: seed must be 32 bytes")
	}
	h := sha512.Sum512(seed)
	var priv [32]byte
	copy(priv[:], h[:32])
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pubBytes, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		panic("goclient.BoxSeedKeypair: curve25519.X25519: " + err.Error())
	}
	var pub [32]byte
	copy(pub[:], pubBytes)
	return pub, priv
}

// DeriveIdentity runs the full pipeline. Returns
// ErrIdentityPassphraseTooShort when len(passphrase) <
// MinIdentityPassphraseLen.
func DeriveIdentity(passphrase, username string) (*Identity, error) {
	if len(passphrase) < MinIdentityPassphraseLen {
		return nil, ErrIdentityPassphraseTooShort
	}
	salt := IdentitySalt(username)

	root := argon2.IDKey(
		[]byte(passphrase),
		salt,
		IdentityArgonTime,
		IdentityArgonMemory,
		IdentityArgonThreads,
		IdentityArgonKeyLen,
	)
	if len(root) != IdentityArgonKeyLen {
		return nil, fmt.Errorf("identity: argon2 returned %d bytes, want %d", len(root), IdentityArgonKeyLen)
	}

	id := &Identity{}
	copy(id.RootSeed[:], root)
	if err := hkdfSplit(root, []byte(IdentityHKDFInfoBox), id.BoxSeed[:]); err != nil {
		return nil, err
	}
	if err := hkdfSplit(root, []byte(IdentityHKDFInfoSign), id.SignSeed[:]); err != nil {
		return nil, err
	}

	id.BoxPub, id.BoxPriv = BoxSeedKeypair(id.BoxSeed[:])

	signPriv := ed25519.NewKeyFromSeed(id.SignSeed[:])
	id.SignPriv = signPriv
	signPub, ok := signPriv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("identity: ed25519.NewKeyFromSeed did not return a PublicKey")
	}
	id.SignPub = signPub
	return id, nil
}

func hkdfSplit(root, info, out []byte) error {
	r := hkdf.New(sha256.New, root, nil, info)
	if _, err := io.ReadFull(r, out); err != nil {
		return fmt.Errorf("identity: hkdf read: %w", err)
	}
	return nil
}
