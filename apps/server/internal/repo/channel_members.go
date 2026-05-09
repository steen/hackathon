package repo

import (
	"context"
	"errors"
	"time"
)

// ChannelMember mirrors a row in the channel_members table introduced
// in migration 0006_encryption.sql. The shape is the byte-level
// MembershipBlock contract from specs/plans/phase-10/membership.md
// (decision-log §10): inviter_sign_pubkey + invitee_box/sign_pubkey
// are pinned at invite time so a later identity rotation does not
// invalidate the stored signature, and the verifier can cross-check
// the pinned values against its own TOFU history (decision-log L34).
//
// JSON tags follow the wire shape that #981 / #982 will publish on
// `GET /api/channels/{id}/members` and the `MembershipBlock` carried
// inside `wraps-needed` responses (decision-log L22).
type ChannelMember struct {
	ChannelID         string    `json:"channel_id"`
	UserID            string    `json:"user_id"`
	InviterUserID     string    `json:"inviter_user_id"`
	InviterSignPubkey []byte    `json:"inviter_sign_pubkey"`
	InviterSignature  []byte    `json:"inviter_signature,omitempty"`
	InviteeBoxPubkey  []byte    `json:"invitee_box_pubkey"`
	InviteeSignPubkey []byte    `json:"invitee_sign_pubkey"`
	AddedAt           time.Time `json:"added_at"`
}

// ErrPrivateChannelNullSignature is returned by Insert when the
// caller would persist a row with a NULL inviter_signature for a
// channel whose `is_public` flag is FALSE — the L33 application-
// level enforcement (SQLite CHECK can't reference another table
// without a trigger). Public channels are exempt because the
// `#general` auto-add and any future is_public=TRUE channel inserts
// the membership row server-side, with no client signature available
// (decision-log §9 + R1.2 carve-out).
var ErrPrivateChannelNullSignature = errors.New(
	"repo: private-channel membership requires inviter_signature (L33)",
)

// errChannelMembersNotImplemented is the sentinel every read-side
// helper returns until #981 fills them in. Bare `errors.New("not
// implemented")` would clash with the same string from sibling
// skeletons — name + path it so the failure surfaces clearly.
var errChannelMembersNotImplemented = errors.New(
	"repo/channel_members: read methods not implemented (lands in #981)",
)

// InsertChannelMember persists a `channel_members` row. The L33
// NULL-signature rule lives here because SQLite CHECK constraints
// cannot reach across to the channels.is_public column without a
// trigger; performing the validation in Go keeps the schema free of
// triggers and the failure mode explicit (typed error, not opaque
// constraint-failed string).
//
// Public-channel detection runs against the `channels.is_public`
// column added by migration 0006. Callers MUST resolve `isPublic`
// from the channel row (or pass false for safety on first contact)
// — the repo does not look it up so the call site stays in control
// of the SELECT.
//
// All read-side helpers (Get, ListForChannel, ListForUser, etc.)
// land in #981; today this skeleton publishes only the validating
// insert so #978 + #979 can build against a stable signature.
func (r *Repo) InsertChannelMember(ctx context.Context, m ChannelMember, channelIsPublic bool) error {
	if !channelIsPublic && len(m.InviterSignature) == 0 {
		return ErrPrivateChannelNullSignature
	}
	if r == nil || r.db == nil {
		return errors.New("repo.InsertChannelMember: nil repo or db")
	}
	added := m.AddedAt.UTC()
	var sig any
	if len(m.InviterSignature) == 0 {
		sig = nil
	} else {
		sig = m.InviterSignature
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_members(
		    channel_id, user_id, inviter_user_id,
		    inviter_sign_pubkey, inviter_signature,
		    invitee_box_pubkey, invitee_sign_pubkey,
		    added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ChannelID, m.UserID, m.InviterUserID,
		m.InviterSignPubkey, sig,
		m.InviteeBoxPubkey, m.InviteeSignPubkey,
		added,
	)
	return err
}

// GetChannelMember is filled in by #981.
func (r *Repo) GetChannelMember(_ context.Context, _ string, _ string) (*ChannelMember, error) {
	return nil, errChannelMembersNotImplemented
}

// ListChannelMembers is filled in by #981.
func (r *Repo) ListChannelMembers(_ context.Context, _ string) ([]ChannelMember, error) {
	return nil, errChannelMembersNotImplemented
}

// ListChannelsForMember is filled in by #981.
func (r *Repo) ListChannelsForMember(_ context.Context, _ string) ([]string, error) {
	return nil, errChannelMembersNotImplemented
}

// DeleteChannelMember is filled in by #981.
func (r *Repo) DeleteChannelMember(_ context.Context, _ string, _ string) error {
	return errChannelMembersNotImplemented
}
