// Package auth — membership_verify implements the §10 inviter-signed
// membership signature scope. Decision-log §10:
//
//	b"snakd-mship-v1:" || channel_id || b"|" || user_id || b"|" ||
//	inviter_user_id || b"|" || inviter_sign_pubkey || b"|" ||
//	invitee_box_pubkey || b"|" || invitee_sign_pubkey || b"|" ||
//	added_at_rfc3339
//
// The signature ties (channel, invitee, inviter, pinned pubkeys,
// timestamp) together so a rogue server cannot swap the inviter
// claim or substitute pinned pubkeys without invalidating the
// signature. Pinning the inviter's `sign_pubkey` AT SIGNING TIME
// (verification-round GAP-4) survives later identity rotation.

package auth

import (
	"crypto/ed25519"
	"errors"
	"time"
)

// MembershipSignatureScopePrefix is the domain-separation tag from
// decision-log §10. Matches the TS-side constant in
// packages/api-client/src/membership_signature.ts (lands in #984's
// client-side wrap loop). Any drift fails the cross-language fixture
// test added in this PR.
const MembershipSignatureScopePrefix = "snakd-mship-v1:"

// InviteePubkeys bundles the pinned-at-invite-time invitee pubkeys.
// Held as raw bytes so callers don't have to re-decode base64 a
// second time inside the handler.
type InviteePubkeys struct {
	BoxPubkey  []byte
	SignPubkey []byte
}

// ErrMembershipSignatureInvalid signals an Ed25519 verify failure on
// the §10 scope. Handlers map to 400 `invalid_membership_signature`.
var ErrMembershipSignatureInvalid = errors.New(
	"auth: membership inviter_signature does not verify under inviter_sign_pubkey",
)

// ErrMembershipSignatureBadInputs signals one of the byte-length
// preconditions failed BEFORE the verify call (caller-side bug or
// malicious payload — tighter check than the L39 byte-length guard
// in the handler). Handlers map to 400.
var ErrMembershipSignatureBadInputs = errors.New(
	"auth: membership signature inputs have unexpected byte lengths",
)

// MembershipSignatureMessage builds the byte sequence covered by the
// §10 signature. Exposed so handler tests + the e2e harness can
// produce well-formed signatures without re-implementing the scope.
//
// addedAt MUST already be in UTC; the caller is responsible for
// `now.UTC()` at signing time so the message is byte-stable across
// timezones.
func MembershipSignatureMessage(
	channelID, userID, inviterUserID string,
	inviterSignPubkey []byte,
	invitee InviteePubkeys,
	addedAt time.Time,
) []byte {
	// Per the scope prose, components join with '|'. Pubkeys are the
	// raw 32-byte values; ids are the ASCII ULID strings; the
	// timestamp is RFC3339 (the same shape the request body carries).
	stamp := addedAt.UTC().Format(time.RFC3339)
	sep := []byte("|")
	out := make([]byte, 0, 256)
	out = append(out, []byte(MembershipSignatureScopePrefix)...)
	out = append(out, []byte(channelID)...)
	out = append(out, sep...)
	out = append(out, []byte(userID)...)
	out = append(out, sep...)
	out = append(out, []byte(inviterUserID)...)
	out = append(out, sep...)
	out = append(out, inviterSignPubkey...)
	out = append(out, sep...)
	out = append(out, invitee.BoxPubkey...)
	out = append(out, sep...)
	out = append(out, invitee.SignPubkey...)
	out = append(out, sep...)
	out = append(out, []byte(stamp)...)
	return out
}

// VerifyMembershipSignature implements the §10 scope verification.
// Returns nil on a valid signature; ErrMembershipSignatureBadInputs
// for byte-length precondition failures; ErrMembershipSignatureInvalid
// for a verify failure.
//
// The caller MUST have already established that:
//  1. inviterSignPubkey == users.sign_pubkey WHERE id = inviter (or,
//     for self-bootstrap, == caller's current sign_pubkey).
//  2. invitee.BoxPubkey == users.box_pubkey WHERE id = invitee.
//  3. invitee.SignPubkey == users.sign_pubkey WHERE id = invitee.
//
// This function only checks the signature; the pinned-pubkey
// equality with the live `users` row stays at the handler so the
// failure-mode messages stay specific.
func VerifyMembershipSignature(
	inviterSignPubkey, inviterSignature []byte,
	channelID, inviterUserID, inviteeUserID string,
	invitee InviteePubkeys,
	addedAt time.Time,
) error {
	if len(inviterSignPubkey) != ed25519.PublicKeySize {
		return ErrMembershipSignatureBadInputs
	}
	if len(inviterSignature) != ed25519.SignatureSize {
		return ErrMembershipSignatureBadInputs
	}
	if len(invitee.BoxPubkey) != 32 || len(invitee.SignPubkey) != 32 {
		return ErrMembershipSignatureBadInputs
	}
	msg := MembershipSignatureMessage(
		channelID, inviteeUserID, inviterUserID,
		inviterSignPubkey, invitee, addedAt,
	)
	if !ed25519.Verify(ed25519.PublicKey(inviterSignPubkey), msg, inviterSignature) {
		return ErrMembershipSignatureInvalid
	}
	return nil
}
