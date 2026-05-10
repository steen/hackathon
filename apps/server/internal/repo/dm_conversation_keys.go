package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// ErrDMConversationKeyAlreadyExists surfaces a duplicate wrap row on
// the (conversation_id, member_user_id) primary key. L6 says wraps
// are immutable post-create; this is the L12 server-side guard.
var ErrDMConversationKeyAlreadyExists = errors.New(
	"repo: dm_conversation_keys row already exists for (conversation_id, member_user_id)",
)

// InsertDMConversationKey persists a dm_conversation_keys row. L30 +
// L39 validation lives in the handler; this layer is pure storage.
func (r *Repo) InsertDMConversationKey(ctx context.Context, k DMConversationKey) error {
	if r == nil || r.db == nil {
		return errors.New("repo.InsertDMConversationKey: nil repo or db")
	}
	created := k.CreatedAt.UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO dm_conversation_keys(
		    conversation_id, member_user_id,
		    wrapped_key, sender_box_pubkey, nonce, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.ConversationID, k.MemberUserID,
		k.WrappedKey, k.SenderBoxPubkey, k.Nonce, created,
	)
	if err != nil && isDMConversationKeyPKViolation(err) {
		return ErrDMConversationKeyAlreadyExists
	}
	return err
}

// InsertDMConversationKeyTx is the *sql.Tx variant for the atomic
// 201-path of POST /api/dms (L7: conversation row + both wraps land
// in one transaction).
func (r *Repo) InsertDMConversationKeyTx(ctx context.Context, tx *sql.Tx, k DMConversationKey) error {
	created := k.CreatedAt.UTC()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO dm_conversation_keys(
		    conversation_id, member_user_id,
		    wrapped_key, sender_box_pubkey, nonce, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.ConversationID, k.MemberUserID,
		k.WrappedKey, k.SenderBoxPubkey, k.Nonce, created,
	)
	if err != nil && isDMConversationKeyPKViolation(err) {
		return ErrDMConversationKeyAlreadyExists
	}
	return err
}

// GetDMConversationKey returns the wrap for (conversationID, memberUserID),
// or (nil, nil) when no row exists.
func (r *Repo) GetDMConversationKey(ctx context.Context, conversationID, memberUserID string) (*DMConversationKey, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT conversation_id, member_user_id,
		        wrapped_key, sender_box_pubkey, nonce, created_at
		   FROM dm_conversation_keys
		  WHERE conversation_id = ? AND member_user_id = ?`,
		conversationID, memberUserID,
	)
	var k DMConversationKey
	if err := row.Scan(&k.ConversationID, &k.MemberUserID,
		&k.WrappedKey, &k.SenderBoxPubkey, &k.Nonce, &k.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &k, nil
}

// ListDMConversationKeys returns every wrap row for conversationID,
// ordered by member_user_id. Empty slice (not nil) when no wraps
// exist.
func (r *Repo) ListDMConversationKeys(ctx context.Context, conversationID string) ([]DMConversationKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT conversation_id, member_user_id,
		        wrapped_key, sender_box_pubkey, nonce, created_at
		   FROM dm_conversation_keys
		  WHERE conversation_id = ?
		  ORDER BY member_user_id ASC`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]DMConversationKey, 0)
	for rows.Next() {
		var k DMConversationKey
		if err := rows.Scan(&k.ConversationID, &k.MemberUserID,
			&k.WrappedKey, &k.SenderBoxPubkey, &k.Nonce, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func isDMConversationKeyPKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "constraint failed") {
		return false
	}
	return strings.Contains(msg, "dm_conversation_keys.conversation_id") ||
		strings.Contains(msg, "dm_conversation_keys.member_user_id") ||
		strings.Contains(msg, "PRIMARY KEY")
}
