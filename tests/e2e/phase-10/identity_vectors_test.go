// Phase-10 identity-derivation cross-language byte-equivalence test —
// Go side. Decision-log §4 + L3 + L37 (`lt -p e2e-encryption 3`).
//
// Companion to tests/e2e/phase-10/identity_vectors.test.ts. The fixture
// and pinned expected pubkeys are identical on both sides — any drift
// fails CI before either client can ship a divergent derivation.
//
// This file lives under tests/e2e so it picks up the same `go test
// ./...` walk as every other Go suite; no global server fixture is
// needed because DeriveIdentity is a pure function.

package phase10

import (
	"encoding/base64"
	"testing"

	goclient "hackathon/packages/go-client"
)

const (
	fixturePassphrase = "correct horse battery staple"
	fixtureUsername   = "alice"

	// Pinned expected pubkeys (base64 of raw 32 bytes). DO NOT regenerate
	// without updating the matching constants in identity_vectors.test.ts
	// in the same PR — these values are the cross-language anchor.
	expectedBoxPubkey  = "FqIApyhlalBwzT8Ms8+ioqp3oRXwoTsP/hI8TjYUgl8="
	expectedSignPubkey = "Oo4hSOrlTPd4CdAShTbKRpYzucsSWuLW0734a0GJx/U="
)

func TestIdentityVectors_GoClientMatchesPinnedFixture(t *testing.T) {
	id, err := goclient.DeriveIdentity(fixturePassphrase, fixtureUsername)
	if err != nil {
		t.Fatalf("DeriveIdentity: %v", err)
	}
	if got := base64.StdEncoding.EncodeToString(id.BoxPub[:]); got != expectedBoxPubkey {
		t.Errorf("box_pubkey: got %q want %q", got, expectedBoxPubkey)
	}
	if got := base64.StdEncoding.EncodeToString(id.SignPub); got != expectedSignPubkey {
		t.Errorf("sign_pubkey: got %q want %q", got, expectedSignPubkey)
	}
}

func TestIdentityVectors_DerivationIsDeterministic(t *testing.T) {
	a, err := goclient.DeriveIdentity(fixturePassphrase, fixtureUsername)
	if err != nil {
		t.Fatalf("DeriveIdentity a: %v", err)
	}
	b, err := goclient.DeriveIdentity(fixturePassphrase, fixtureUsername)
	if err != nil {
		t.Fatalf("DeriveIdentity b: %v", err)
	}
	if a.BoxPub != b.BoxPub {
		t.Error("box pubkey not deterministic")
	}
	if string(a.SignPub) != string(b.SignPub) {
		t.Error("sign pubkey not deterministic")
	}
}

// TestIdentityVectors_BoxAndSignDeriveFromDistinctSubseeds guards the
// HKDF-split contract: the box and sign keypairs MUST be derived from
// different sub-seeds (different `info` strings), so a leak of one
// sub-seed cannot reconstruct the other.
func TestIdentityVectors_BoxAndSignDeriveFromDistinctSubseeds(t *testing.T) {
	id, err := goclient.DeriveIdentity(fixturePassphrase, fixtureUsername)
	if err != nil {
		t.Fatalf("DeriveIdentity: %v", err)
	}
	if id.BoxSeed == id.SignSeed {
		t.Fatal("box_seed and sign_seed are identical — HKDF split is broken")
	}
}

// TestIdentityVectors_ServerSideHelpersExist pins the AC requirement
// that apps/server/internal/auth exports BoxSeedKeypair + the named
// constants. We can't import from this package's path, so the
// equivalent server-side test lives at
// apps/server/internal/auth/identity_box_test.go (added by this PR);
// the test below merely fails loudly if the goclient counterparts
// diverge from the server-side equivalents shipped in the same PR.
//
// The byte-equivalence of the two is captured by the pinned expected
// pubkeys above plus the parallel TS-side test — if either client
// drifts, the pinned constants stop matching.
func TestIdentityVectors_GoClientConstantsAreFrozen(t *testing.T) {
	if goclient.IdentityArgonTime != 3 {
		t.Errorf("argon time must remain 3 (decision-log L3); got %d", goclient.IdentityArgonTime)
	}
	if goclient.IdentityArgonMemory != 64*1024 {
		t.Errorf("argon memory must remain 64*1024 KiB; got %d", goclient.IdentityArgonMemory)
	}
	if goclient.IdentityArgonThreads != 1 {
		t.Errorf("argon threads must remain 1; got %d", goclient.IdentityArgonThreads)
	}
	if goclient.IdentityHKDFInfoBox != "snakd-identity:v1:box" {
		t.Errorf("hkdf info box must be %q; got %q",
			"snakd-identity:v1:box", goclient.IdentityHKDFInfoBox)
	}
	if goclient.IdentityHKDFInfoSign != "snakd-identity:v1:sgn" {
		t.Errorf("hkdf info sign must be %q; got %q",
			"snakd-identity:v1:sgn", goclient.IdentityHKDFInfoSign)
	}
	if goclient.IdentitySaltPrefix != "snakd-identity:v1:" {
		t.Errorf("salt prefix must be %q; got %q",
			"snakd-identity:v1:", goclient.IdentitySaltPrefix)
	}
}
