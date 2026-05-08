package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DMConversation mirrors a row in the dm_conversations table. JSON tags
// emit the wire shape from specs/plans/phase-9/dms.md (decision-log L2
// canonical pair ordering, L11 denormalized last_message_id /
// last_message_at). Listing-only fields (Peer, UnreadCount) live on the
// HTTP response type that wraps this struct — keeping the persisted
// shape clean of viewer-relative state.
type DMConversation struct {
	ID            string     `json:"id"`
	UserAID       string     `json:"user_a_id"`
	UserBID       string     `json:"user_b_id"`
	LastMessageID *string    `json:"last_message_id"`
	LastMessageAt *time.Time `json:"last_message_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// CanonicalDMUserOrder returns (a, b) sorted so a < b per decision-log
// L2. Callers pass (viewer, peer) in either order; the canonical pair
// is what dm_conversations stores via its UNIQUE(user_a_id, user_b_id)
// index.
func CanonicalDMUserOrder(x, y string) (string, string) {
	if x < y {
		return x, y
	}
	return y, x
}

// FindOrCreateDMConversation returns the conversation row for the
// canonical (userA, userB) pair, creating it on first call. Returns
// (conv, created, err): created == true when this call inserted the
// row, false when the row already existed. The transaction shape
// mirrors apps/server/internal/http/auth_store.go:81 (begin → deferred
// Rollback → ExecContext → SELECT → Commit).
//
// Caller responsibility: pass (a, b) already sorted via
// CanonicalDMUserOrder. The handler resolves (viewer, peer) → (a, b)
// at the top of the request so this method's contract stays narrow.
func (r *Repo) FindOrCreateDMConversation(ctx context.Context, id, userA, userB string, now time.Time) (*DMConversation, bool, error) {
	if userA >= userB {
		return nil, false, errors.New("repo: dm conversation user pair not in canonical order (user_a_id < user_b_id)")
	}
	created := now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO dm_conversations
		   (id, user_a_id, user_b_id, last_message_id, last_message_at, created_at)
		   VALUES (?, ?, ?, NULL, NULL, ?)`,
		id, userA, userB, created,
	)
	if err != nil {
		return nil, false, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	wasCreated := rowsAffected == 1

	row := tx.QueryRowContext(ctx,
		`SELECT id, user_a_id, user_b_id, last_message_id, last_message_at, created_at
		   FROM dm_conversations
		  WHERE user_a_id = ? AND user_b_id = ?`,
		userA, userB,
	)
	var c DMConversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.LastMessageID, &c.LastMessageAt, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, errors.New("repo: dm conversation vanished between INSERT OR IGNORE and SELECT")
		}
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &c, wasCreated, nil
}

// GetDMConversation returns a single dm_conversations row by id, or
// (nil, nil) when no row matches. Handlers branch on the nil result
// to emit a 404 without inspecting sql.ErrNoRows themselves.
func (r *Repo) GetDMConversation(ctx context.Context, id string) (*DMConversation, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, user_a_id, user_b_id, last_message_id, last_message_at, created_at
		   FROM dm_conversations WHERE id = ?`, id)
	var c DMConversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.LastMessageID, &c.LastMessageAt, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// DMConversationListing is the projection GET /api/dms returns: a
// dm_conversations row plus the viewer-relative peer summary and unread
// count computed in SQL. The handler renders this directly into the
// envelope's data.conversations array.
type DMConversationListing struct {
	DMConversation
	Peer        UserSummary `json:"peer"`
	UnreadCount int         `json:"unread_count"`
}

// UserSummary is the {id, username} pair the wire's Conversation.peer
// field carries. Lives in repo so the SQL projection joins users
// directly without a second round-trip.
type UserSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// ListDMConversations returns every conversation in which viewerID
// participates AND that has at least one message (decision-log §3 —
// empty conversations are hidden until they have content). Sorted
// last_message_at DESC (decision-log §9). No pagination in v1 (L12).
//
// unread_count uses the L6 DM rule: COALESCE the viewer's
// last_read_dm_message_id over ” so a NULL row counts every peer
// message as unread (decision-log §11). Sender messages are excluded
// from the unread count by the `sender_user_id != viewer` predicate.
func (r *Repo) ListDMConversations(ctx context.Context, viewerID string) ([]DMConversationListing, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id, c.user_a_id, c.user_b_id, c.last_message_id, c.last_message_at, c.created_at,
		       u.id AS peer_id, u.username AS peer_username,
		       (SELECT COUNT(*) FROM dm_messages m
		         WHERE m.conversation_id = c.id
		           AND m.sender_user_id != ?
		           AND m.id > COALESCE(
		                (SELECT last_read_dm_message_id FROM dm_reads
		                  WHERE conversation_id = c.id AND user_id = ?), '')
		       ) AS unread_count
		  FROM dm_conversations c
		  JOIN users u ON u.id = CASE WHEN c.user_a_id = ? THEN c.user_b_id ELSE c.user_a_id END
		 WHERE c.last_message_id IS NOT NULL
		   AND (c.user_a_id = ? OR c.user_b_id = ?)
		 ORDER BY c.last_message_at DESC`,
		viewerID, viewerID, viewerID, viewerID, viewerID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]DMConversationListing, 0)
	for rows.Next() {
		var l DMConversationListing
		if err := rows.Scan(
			&l.ID, &l.UserAID, &l.UserBID, &l.LastMessageID, &l.LastMessageAt, &l.CreatedAt,
			&l.Peer.ID, &l.Peer.Username,
			&l.UnreadCount,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LookupUserSummary returns the {id, username} pair for a single user,
// or (nil, nil) when no row exists. Used by the DM POST handler to
// build the peer summary on a freshly-created conversation (which is
// not yet eligible for ListDMConversations because it has no messages).
func (r *Repo) LookupUserSummary(ctx context.Context, id string) (*UserSummary, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, username FROM users WHERE id = ?`, id)
	var u UserSummary
	if err := row.Scan(&u.ID, &u.Username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}
