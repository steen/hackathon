package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// ErrChannelMemberAlreadyExists surfaces a duplicate membership row
// (handler maps to 409). InsertChannelMember sniffs the SQLite UNIQUE
// constraint message on the (channel_id, user_id) primary key.
var ErrChannelMemberAlreadyExists = errors.New(
	"repo: user is already a member of channel",
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
	if err != nil && isChannelMemberPKViolation(err) {
		return ErrChannelMemberAlreadyExists
	}
	return err
}

// InsertChannelMemberTx is the *sql.Tx variant of InsertChannelMember
// for callers that need to compose membership inserts with other writes
// in one transaction (creator-bootstrap of a new channel, registration
// auto-add to every is_public channel). Same L33 guard as the
// non-transactional path; the nil-check on tx is the caller's
// responsibility.
func (r *Repo) InsertChannelMemberTx(ctx context.Context, tx *sql.Tx, m ChannelMember, channelIsPublic bool) error {
	if !channelIsPublic && len(m.InviterSignature) == 0 {
		return ErrPrivateChannelNullSignature
	}
	added := m.AddedAt.UTC()
	var sig any
	if len(m.InviterSignature) == 0 {
		sig = nil
	} else {
		sig = m.InviterSignature
	}
	_, err := tx.ExecContext(ctx,
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
	if err != nil && isChannelMemberPKViolation(err) {
		return ErrChannelMemberAlreadyExists
	}
	return err
}

// GetChannelMember returns the channel_members row for (channelID, userID)
// or (nil, nil) when no row exists. Pubkey/signature blobs are returned
// as raw bytes; callers responsible for base64-encoding when serialising.
func (r *Repo) GetChannelMember(ctx context.Context, channelID, userID string) (*ChannelMember, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT channel_id, user_id, inviter_user_id,
		        inviter_sign_pubkey, inviter_signature,
		        invitee_box_pubkey, invitee_sign_pubkey, added_at
		   FROM channel_members
		  WHERE channel_id = ? AND user_id = ?`,
		channelID, userID,
	)
	var m ChannelMember
	var sig []byte
	if err := row.Scan(&m.ChannelID, &m.UserID, &m.InviterUserID,
		&m.InviterSignPubkey, &sig,
		&m.InviteeBoxPubkey, &m.InviteeSignPubkey, &m.AddedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(sig) > 0 {
		m.InviterSignature = sig
	}
	return &m, nil
}

// IsMember reports whether userID is a current member of channelID.
// Cheaper than GetChannelMember on the per-request authorization path
// (handlers check membership before reading the full row).
func (r *Repo) IsMember(ctx context.Context, channelID, userID string) (bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM channel_members
		  WHERE channel_id = ? AND user_id = ? LIMIT 1`,
		channelID, userID,
	)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListChannelMembers returns every channel_members row for channelID,
// ordered by added_at then user_id. Empty slice (not nil) when the
// channel has no members.
func (r *Repo) ListChannelMembers(ctx context.Context, channelID string) ([]ChannelMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT channel_id, user_id, inviter_user_id,
		        inviter_sign_pubkey, inviter_signature,
		        invitee_box_pubkey, invitee_sign_pubkey, added_at
		   FROM channel_members
		  WHERE channel_id = ?
		  ORDER BY added_at ASC, user_id ASC`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ChannelMember, 0)
	for rows.Next() {
		var m ChannelMember
		var sig []byte
		if err := rows.Scan(&m.ChannelID, &m.UserID, &m.InviterUserID,
			&m.InviterSignPubkey, &sig,
			&m.InviteeBoxPubkey, &m.InviteeSignPubkey, &m.AddedAt); err != nil {
			return nil, err
		}
		if len(sig) > 0 {
			m.InviterSignature = sig
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListChannelsForMember returns every channel id userID is a member of.
// Used by the registration auto-add flow's idempotency check and by
// debug surfaces; the listing path uses ListChannelsWithReadState which
// joins channel_members directly.
func (r *Repo) ListChannelsForMember(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT channel_id FROM channel_members WHERE user_id = ? ORDER BY channel_id ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteChannelMember removes the (channelID, userID) row. Returns
// (false, nil) when no row matches so handlers can map to 404 without
// inspecting sql results. Returns (true, nil) on a successful delete.
func (r *Repo) DeleteChannelMember(ctx context.Context, channelID, userID string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`,
		channelID, userID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListPublicChannelIDs returns every channel id where is_public = TRUE.
// Used by the registration auto-add flow (decision-log §9 / R1.2): the
// new user's CreateUser transaction inserts one channel_members row per
// public channel. Returns an empty slice when no public channels exist.
func (r *Repo) ListPublicChannelIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM channels WHERE is_public = TRUE ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// isChannelMemberPKViolation maps SQLite's UNIQUE/PRIMARY-KEY message for
// channel_members(channel_id, user_id) to the typed sentinel. Driver
// does not expose a typed sentinel so string-match is the available
// signal; mirrors isChannelNameTakenErr in channels.go.
func isChannelMemberPKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "constraint failed") {
		return false
	}
	return strings.Contains(msg, "channel_members.channel_id") ||
		strings.Contains(msg, "channel_members.user_id") ||
		strings.Contains(msg, "PRIMARY KEY")
}
