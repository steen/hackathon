package repo

import (
	"context"
	"database/sql"
	"time"
)

// DMMessage mirrors a row in the dm_messages table. Wire shape from
// specs/plans/phase-9/dms.md "DMMessage" — immutable on the wire (L9),
// body shares MaxMessageBodyBytes = 4096 with channel messages (L16).
type DMMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SenderUserID   string    `json:"sender_user_id"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListDMMessages mirrors ListMessages: ULID-cursor newest-first
// pagination over a single conversation's history. before is an
// exclusive ULID cursor; limit is clamped to MaxMessagesLimit and
// defaults to DefaultMessagesLimit.
func (r *Repo) ListDMMessages(ctx context.Context, conversationID, before string, limit int) ([]DMMessage, error) {
	if limit <= 0 {
		limit = DefaultMessagesLimit
	}
	if limit > MaxMessagesLimit {
		limit = MaxMessagesLimit
	}
	var (
		rows *sql.Rows
		err  error
	)
	if before == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, conversation_id, sender_user_id, body, created_at
			   FROM dm_messages
			  WHERE conversation_id = ?
			  ORDER BY id DESC
			  LIMIT ?`,
			conversationID, limit)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, conversation_id, sender_user_id, body, created_at
			   FROM dm_messages
			  WHERE conversation_id = ? AND id < ?
			  ORDER BY id DESC
			  LIMIT ?`,
			conversationID, before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]DMMessage, 0, limit)
	for rows.Next() {
		var m DMMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderUserID, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertDMMessageTx persists a single DM, atomically updates the
// owning conversation's denormalized last_message_id / last_message_at,
// and advances the sender's dm_reads row to the new message id —
// decision-log §11 + L21. The shape mirrors InsertMessageTx; the
// extra UPSERT branch lives in dm_reads.go via upsertDMReadAdvance.
//
// Returns the persisted message and the *post-update* DMConversation
// row so the broadcast emitter can build a self-sufficient
// {type:"dm"} frame (decision-log §8) without a second SELECT.
func (r *Repo) InsertDMMessageTx(ctx context.Context, id, conversationID, senderID, body string, now time.Time) (*DMMessage, *DMConversation, error) {
	created := now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO dm_messages(id, conversation_id, sender_user_id, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, conversationID, senderID, body, created,
	); err != nil {
		return nil, nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE dm_conversations SET last_message_id = ?, last_message_at = ? WHERE id = ?`,
		id, created, conversationID,
	); err != nil {
		return nil, nil, err
	}

	if err := upsertDMReadAdvanceTx(ctx, tx, conversationID, senderID, id, created); err != nil {
		return nil, nil, err
	}

	row := tx.QueryRowContext(ctx,
		`SELECT id, user_a_id, user_b_id, last_message_id, last_message_at, created_at
		   FROM dm_conversations WHERE id = ?`, conversationID)
	var c DMConversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.LastMessageID, &c.LastMessageAt, &c.CreatedAt); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return &DMMessage{
		ID:             id,
		ConversationID: conversationID,
		SenderUserID:   senderID,
		Body:           body,
		CreatedAt:      created,
	}, &c, nil
}
