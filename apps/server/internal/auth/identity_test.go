// Phase-10 server-side identity helper tests. The server-side helpers
// (identity_box.go's BoxSeedKeypair, identity_params.go's constants)
// are required by AC #979 even though no production server code path
// calls them in this PR — they exist for future server-side wrap
// validation (#983 + the membership / wrap PRs). The test below pins
// the constants and asserts BoxSeedKeypair on a deterministic seed.

package auth

import (
	"bytes"
	"crypto/sha512"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestIdentityParams_FrozenValues(t *testing.T) {
	if ArgonTime != 3 {
		t.Errorf("ArgonTime must be 3 (decision-log L3); got %d", ArgonTime)
	}
	if ArgonMemory != 64*1024 {
		t.Errorf("ArgonMemory must be 64*1024 KiB; got %d", ArgonMemory)
	}
	if ArgonThreads != 1 {
		t.Errorf("ArgonThreads must be 1; got %d", ArgonThreads)
	}
	if ArgonKeyLen != 32 {
		t.Errorf("ArgonKeyLen must be 32; got %d", ArgonKeyLen)
	}
	if HKDFInfoBox != "snakd-identity:v1:box" {
		t.Errorf("HKDFInfoBox must be %q; got %q", "snakd-identity:v1:box", HKDFInfoBox)
	}
	if HKDFInfoSign != "snakd-identity:v1:sgn" {
		t.Errorf("HKDFInfoSign must be %q; got %q", "snakd-identity:v1:sgn", HKDFInfoSign)
	}
	if SaltPrefix != "snakd-identity:v1:" {
		t.Errorf("SaltPrefix must be %q; got %q", "snakd-identity:v1:", SaltPrefix)
	}
	if SaltLen != 16 {
		t.Errorf("SaltLen must be 16 (libsodium crypto_pwhash_SALTBYTES); got %d", SaltLen)
	}
}

func TestBoxSeedKeypair_MatchesLibsodiumDerivation(t *testing.T) {
	// Reference: re-implement the same SHA-512 + clamp + X25519 dance
	// inline so the test independently verifies that the production
	// code does it correctly. A future refactor that "simplifies"
	// BoxSeedKeypair to box.GenerateKey(bytes.NewReader(seed)) would
	// produce different output and trip this assertion.
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	pub, priv := BoxSeedKeypair(seed)

	h := sha512.Sum512(seed)
	var refPriv [32]byte
	copy(refPriv[:], h[:32])
	refPriv[0] &= 248
	refPriv[31] &= 127
	refPriv[31] |= 64
	refPubBytes, err := curve25519.X25519(refPriv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatalf("reference X25519: %v", err)
	}

	if !bytes.Equal(priv[:], refPriv[:]) {
		t.Errorf("priv: got %x want %x", priv[:], refPriv[:])
	}
	if !bytes.Equal(pub[:], refPubBytes) {
		t.Errorf("pub: got %x want %x", pub[:], refPubBytes)
	}
}

func TestBoxSeedKeypair_PanicsOnWrongLength(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on 16-byte seed")
		}
	}()
	BoxSeedKeypair(make([]byte, 16))
}
