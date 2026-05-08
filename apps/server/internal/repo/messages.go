package repo

import (
	"context"
	"database/sql"
	"time"
)

// Message mirrors a row in the messages table. JSON tags use the wire
// shape PRD §10 documents for `/api/channels/{id}/messages`.
type Message struct {
	ID           string    `json:"id"`
	ChannelID    string    `json:"channel_id"`
	SenderUserID string    `json:"sender_user_id"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

// MaxMessagesLimit caps page size so a malicious or buggy client cannot
// stream the entire history in one request.
const MaxMessagesLimit = 200

// DefaultMessagesLimit is used when the caller omits ?limit=.
const DefaultMessagesLimit = 50

// ListMessages returns up to limit messages from channelID, newest first.
// When before is non-empty it acts as an exclusive ULID cursor: only
// messages with id < before are returned. ULID lex order matches
// chronological order, so the same column doubles as the cursor.
func (r *Repo) ListMessages(ctx context.Context, channelID, before string, limit int) ([]Message, error) {
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
			`SELECT id, channel_id, user_id, body, created_at
			   FROM messages
			  WHERE channel_id = ?
			  ORDER BY id DESC
			  LIMIT ?`,
			channelID, limit)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, channel_id, user_id, body, created_at
			   FROM messages
			  WHERE channel_id = ? AND id < ?
			  ORDER BY id DESC
			  LIMIT ?`,
			channelID, before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.SenderUserID, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMessageTx persists a single message and atomically updates the
// owning channel's denormalized last_message_id / last_message_at so the
// channels listing can derive unread counts without a per-row scan of
// messages. Decision log `lt -p direct-messages 3` L11 + L21 mandate
// the denormalization and the transactional pattern; the BeginTx shape
// here mirrors apps/server/internal/http/auth_store.go:81 (begin →
// deferred Rollback → ExecContext → Commit).
//
// The caller supplies id (ULID) and now so the broadcast that follows
// carries the same values that landed in the DB.
func (r *Repo) InsertMessageTx(ctx context.Context, id, channelID, userID, body string, now time.Time) (*Message, error) {
	created := now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages(id, channel_id, user_id, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, channelID, userID, body, created,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE channels SET last_message_id = ?, last_message_at = ? WHERE id = ?`,
		id, created, channelID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Message{
		ID:           id,
		ChannelID:    channelID,
		SenderUserID: userID,
		Body:         body,
		CreatedAt:    created,
	}, nil
}
