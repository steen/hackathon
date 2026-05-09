package repo

import (
	"context"
	"errors"
	"time"
)

// ChannelKey mirrors a row in the channel_keys table introduced in
// migration 0006_encryption.sql. Per specs/plans/phase-10/keys.md
// (decision-log §5 + §7): one wrapped root-key copy per
// (channel_id, generation_id, member_user_id). The wrap is a
// 48-byte crypto_box ciphertext (32-byte payload + 16-byte
// Poly1305 MAC); the sender's box pubkey is stored alongside so
// receivers can crypto_box_open without a server round-trip and so
// historical wraps remain decryptable across sender pubkey rotation.
type ChannelKey struct {
	ChannelID       string    `json:"channel_id"`
	GenerationID    int64     `json:"generation_id"`
	MemberUserID    string    `json:"member_user_id"`
	WrappedKey      []byte    `json:"wrapped_key"`
	SenderBoxPubkey []byte    `json:"sender_box_pubkey"`
	Nonce           []byte    `json:"nonce"`
	CreatedAt       time.Time `json:"created_at"`
}

// errChannelKeysNotImplemented is the sentinel every method returns
// until #982 / #984 / #985 fill the read and write paths in. The
// schema is stable as of this PR; downstream sub-issues add the
// three-mode keys-RPC validation, lazy-fill, and rotation logic.
var errChannelKeysNotImplemented = errors.New(
	"repo/channel_keys: methods not implemented (lands in #982/#984/#985)",
)

// InsertChannelKey lands in #982 (atomic-inline wraps on create /
// add-member) and #984 (standalone keys RPC bootstrap + fill-in).
func (r *Repo) InsertChannelKey(_ context.Context, _ ChannelKey) error {
	return errChannelKeysNotImplemented
}

// GetChannelKey lands in #984 (lazy-wrap-on-online query path).
func (r *Repo) GetChannelKey(_ context.Context, _ string, _ int64, _ string) (*ChannelKey, error) {
	return nil, errChannelKeysNotImplemented
}

// CurrentChannelKeyGeneration returns the channel's current
// `max(generation_id)` per decision-log §8 — the precondition the
// three keys-RPC modes branch on. Lands in #984.
func (r *Repo) CurrentChannelKeyGeneration(_ context.Context, _ string) (int64, bool, error) {
	return 0, false, errChannelKeysNotImplemented
}

// ListWrapsNeeded is the server-side compute for L22's `missing`
// list — one row per (user_id, generation_id) where channel_members
// has a row but channel_keys does not for the channel's current
// generation. Lands in #984.
func (r *Repo) ListWrapsNeeded(_ context.Context, _ string) ([]ChannelKey, error) {
	return nil, errChannelKeysNotImplemented
}

// DeleteChannelKeysForMember runs in the DELETE-member transaction
// per specs/plans/phase-10/membership.md and removes every wrap row
// for the leaver, across every generation. Lands in #985.
func (r *Repo) DeleteChannelKeysForMember(_ context.Context, _ string, _ string) error {
	return errChannelKeysNotImplemented
}
