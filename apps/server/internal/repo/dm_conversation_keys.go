package repo

import (
	"context"
	"errors"
	"time"
)

// DMConversationKey mirrors a row in the dm_conversation_keys table
// introduced in migration 0006_encryption.sql. Per specs/plans/
// phase-10/keys.md (decision-log §5 + L6): DMs never rotate, so the
// table has no `generation_id` — one wrap per (conversation, member).
type DMConversationKey struct {
	ConversationID  string    `json:"conversation_id"`
	MemberUserID    string    `json:"member_user_id"`
	WrappedKey      []byte    `json:"wrapped_key"`
	SenderBoxPubkey []byte    `json:"sender_box_pubkey"`
	Nonce           []byte    `json:"nonce"`
	CreatedAt       time.Time `json:"created_at"`
}

var errDMConversationKeysNotImplemented = errors.New(
	"repo/dm_conversation_keys: methods not implemented (lands in #982)",
)

// InsertDMConversationKey lands in #982 — atomic-inline wraps in
// the `POST /api/dms` 201 path. The `POST /api/dms` 200 idempotent
// re-call MUST omit wraps (decision-log L6 + L12); enforcement
// lives in the handler, not here.
func (r *Repo) InsertDMConversationKey(_ context.Context, _ DMConversationKey) error {
	return errDMConversationKeysNotImplemented
}

// GetDMConversationKey lands in #982 / #984 — read path for the DM
// receiver's crypto_box_open.
func (r *Repo) GetDMConversationKey(_ context.Context, _ string, _ string) (*DMConversationKey, error) {
	return nil, errDMConversationKeysNotImplemented
}

// ListDMConversationKeys lands in #982 / #984.
func (r *Repo) ListDMConversationKeys(_ context.Context, _ string) ([]DMConversationKey, error) {
	return nil, errDMConversationKeysNotImplemented
}
