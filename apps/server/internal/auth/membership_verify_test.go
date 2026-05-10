package auth

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

// TestVerifyMembershipSignature_HappyPath signs over a valid §10
// scope and verifies the round-trip succeeds.
func TestVerifyMembershipSignature_HappyPath(t *testing.T) {
	t.Parallel()
	seed := bytesRepeat(0xCC, 32)
	priv := ed25519.NewKeyFromSeed(seed)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519.NewKeyFromSeed did not return a PublicKey")
	}
	added := time.Now().UTC().Truncate(time.Second)
	channelID := "01JX0000000000000000000000"
	inviter := "01JX1AAAAAAAAAAAAAAAAAAAAA"
	invitee := "01JX1BBBBBBBBBBBBBBBBBBBBB"
	inviteeBox := bytesRepeat(0x01, 32)
	inviteeSign := bytesRepeat(0x02, 32)

	msg := MembershipSignatureMessage(channelID, invitee, inviter, pub, InviteePubkeys{
		BoxPubkey: inviteeBox, SignPubkey: inviteeSign,
	}, added)
	sig := ed25519.Sign(priv, msg)

	if err := VerifyMembershipSignature(pub, sig, channelID, inviter, invitee,
		InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign}, added,
	); err != nil {
		t.Fatalf("expected nil err on valid sig; got %v", err)
	}
}

// TestVerifyMembershipSignature_TamperedSignature flips a byte in
// the signature and asserts ErrMembershipSignatureInvalid surfaces.
func TestVerifyMembershipSignature_TamperedSignature(t *testing.T) {
	t.Parallel()
	priv := ed25519.NewKeyFromSeed(bytesRepeat(0xCC, 32))
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519.NewKeyFromSeed did not return a PublicKey")
	}
	added := time.Now().UTC().Truncate(time.Second)
	inviteeBox := bytesRepeat(0x01, 32)
	inviteeSign := bytesRepeat(0x02, 32)
	msg := MembershipSignatureMessage("ch", "invitee", "inviter", pub,
		InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign}, added)
	sig := ed25519.Sign(priv, msg)
	sig[0] ^= 0x01 // tamper

	err := VerifyMembershipSignature(pub, sig, "ch", "inviter", "invitee",
		InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign}, added)
	if !errors.Is(err, ErrMembershipSignatureInvalid) {
		t.Fatalf("expected ErrMembershipSignatureInvalid; got %v", err)
	}
}

// TestVerifyMembershipSignature_BadByteLength rejects with the
// bad-inputs sentinel before the verify call when pubkeys or
// signatures are the wrong size.
func TestVerifyMembershipSignature_BadByteLength(t *testing.T) {
	t.Parallel()
	priv := ed25519.NewKeyFromSeed(bytesRepeat(0xCC, 32))
	pub, _ := priv.Public().(ed25519.PublicKey)
	added := time.Now().UTC()
	inviteeBox := bytesRepeat(0x01, 32)
	inviteeSign := bytesRepeat(0x02, 32)

	cases := []struct {
		name         string
		signPub, sig []byte
		invitee      InviteePubkeys
	}{
		{"short pubkey", pub[:31], make([]byte, 64), InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign}},
		{"short signature", pub, make([]byte, 63), InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign}},
		{"short invitee box", pub, make([]byte, 64), InviteePubkeys{BoxPubkey: inviteeBox[:31], SignPubkey: inviteeSign}},
		{"short invitee sign", pub, make([]byte, 64), InviteePubkeys{BoxPubkey: inviteeBox, SignPubkey: inviteeSign[:31]}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyMembershipSignature(tc.signPub, tc.sig, "ch", "inviter", "invitee", tc.invitee, added)
			if !errors.Is(err, ErrMembershipSignatureBadInputs) {
				t.Fatalf("%s: expected ErrMembershipSignatureBadInputs; got %v", tc.name, err)
			}
		})
	}
}

// TestMembershipSignatureScopePrefix pins the domain-separation tag.
// Drift fails the cross-language identity_vectors test (TS-side
// constant must match).
func TestMembershipSignatureScopePrefix(t *testing.T) {
	t.Parallel()
	if MembershipSignatureScopePrefix != "snakd-mship-v1:" {
		t.Fatalf("scope prefix drift: got %q want %q", MembershipSignatureScopePrefix, "snakd-mship-v1:")
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
