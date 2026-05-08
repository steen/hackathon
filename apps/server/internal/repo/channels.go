package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Channel mirrors a row in the channels table. Fields are exported so
// HTTP handlers can JSON-encode them directly without an extra DTO.
//
// LastMessageID/LastMessageAt are denormalized pointers populated by
// InsertMessageTx (decision log L11). LastReadMessageID and
// UnreadCount are populated by ListChannelsWithReadState — the
// per-viewer listing helper that materializes channel_reads then
// LEFT-JOINs it. All four use pointer types with `omitempty` so the
// system arm (ListChannels, no viewer) leaves the JSON wire shape
// unchanged. The TS mirror in packages/api-client/src/types.ts marks
// these `?: optional` per L26 (optional-first wire-types coordination).
type Channel struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	CreatedAt         time.Time  `json:"created_at"`
	LastMessageID     *string    `json:"last_message_id,omitempty"`
	LastMessageAt     *time.Time `json:"last_message_at,omitempty"`
	LastReadMessageID *string    `json:"last_read_message_id,omitempty"`
	UnreadCount       *int       `json:"unread_count,omitempty"`
}

// ErrChannelNameTaken is returned by CreateChannel when the UNIQUE
// constraint on channels.name trips. Callers map this to a 409.
var ErrChannelNameTaken = errors.New("repo: channel name already taken")

// ErrChannelNotFound is returned by RenameChannel when the supplied id
// does not match any row. Callers map this to a 404.
var ErrChannelNotFound = errors.New("repo: channel not found")

// ListChannels returns every channel ordered by id (ULID — chronological).
// System-only listing — no per-viewer state. The HTTP /api/channels
// handler uses ListChannelsWithReadState; this entrypoint is reserved
// for callers without an authenticated viewer (WS default-channel
// resolver in wiring/ws.go, seed-lookup in presence tests).
// Callers that need filtering can pass a context with a deadline.
func (r *Repo) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, created_at FROM channels ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChannelExists reports whether a channel row with the given id exists.
// Cheaper than GetChannel for hot paths (e.g. the WS upgrade lookup) that
// don't need the full row. Returns (false, nil) for an unknown id.
func (r *Repo) ChannelExists(ctx context.Context, id string) (bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM channels WHERE id = ? LIMIT 1`, id)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetChannel returns a single channel by id. Returns (nil, nil) when no
// such row exists so handlers can branch on a missing channel without
// inspecting sql.ErrNoRows themselves.
func (r *Repo) GetChannel(ctx context.Context, id string) (*Channel, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM channels WHERE id = ?`, id)
	var c Channel
	if err := row.Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// CreateChannel inserts a new channel row. The caller supplies the ULID
// so the same id can be returned in the same envelope without a second
// SELECT round-trip.
func (r *Repo) CreateChannel(ctx context.Context, id, name string, now time.Time) (*Channel, error) {
	created := now.UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels(id, name, created_at) VALUES (?, ?, ?)`,
		id, name, created,
	)
	if err != nil {
		if isChannelNameTakenErr(err) {
			return nil, ErrChannelNameTaken
		}
		return nil, err
	}
	return &Channel{ID: id, Name: name, CreatedAt: created}, nil
}

// RenameChannel updates channels.name for the row matching id and returns
// the post-rename row. Errors:
//   - ErrChannelNotFound when no row matches id (handler maps to 404).
//   - ErrChannelNameTaken when the new name collides with another row's
//     UNIQUE constraint (handler maps to 409).
//
// `_ time.Time` is accepted so the call site is symmetric with
// CreateChannel; channels carry no updated_at column today, so the value
// is unused. Keeping the signature stable lets a future migration add
// updated_at without rewiring callers.
func (r *Repo) RenameChannel(ctx context.Context, id, newName string, _ time.Time) (*Channel, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE channels SET name = ? WHERE id = ?`, newName, id)
	if err != nil {
		if isChannelNameTakenErr(err) {
			return nil, ErrChannelNameTaken
		}
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrChannelNotFound
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM channels WHERE id = ?`, id)
	var c Channel
	if err := row.Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrChannelNotFound
		}
		return nil, err
	}
	return &c, nil
}

// ListChannelsWithReadState returns every channel with per-viewer
// read-state fields populated, in `last_message_at DESC NULLS LAST`
// order (decision log §9 — activity-ordered listing).
//
// Calls MaterializeChannelReadsTx for viewerUserID first so a fresh
// user has a `channel_reads` row pinned to each channel's
// `last_message_id` (decision log §11 — auto-materialize on listing,
// never-messaged channels are skipped because the column is NOT NULL).
//
// The unread_count subquery counts messages with id > the viewer's
// last_read_message_id (decision log L6 channel formula). The
// COALESCE guards the (rare in practice after materialization) row
// where the viewer has no channel_reads entry — empty string sorts
// before every ULID so all messages would count, but a never-messaged
// channel has zero messages either way.
func (r *Repo) ListChannelsWithReadState(ctx context.Context, viewerUserID string) ([]Channel, error) {
	if err := r.MaterializeChannelReadsTx(ctx, viewerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.name, c.created_at,
		        c.last_message_id, c.last_message_at,
		        r.last_read_message_id,
		        (SELECT COUNT(*) FROM messages m
		          WHERE m.channel_id = c.id
		            AND m.id > COALESCE(r.last_read_message_id, '')) AS unread_count
		   FROM channels c
		   LEFT JOIN channel_reads r
		     ON r.channel_id = c.id AND r.user_id = ?
		  ORDER BY c.last_message_at DESC NULLS LAST, c.id ASC`,
		viewerUserID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Channel, 0)
	for rows.Next() {
		var (
			c             Channel
			lastMsgID     sql.NullString
			lastMsgAt     sql.NullTime
			lastReadMsgID sql.NullString
			unread        int
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt,
			&lastMsgID, &lastMsgAt, &lastReadMsgID, &unread); err != nil {
			return nil, err
		}
		if lastMsgID.Valid {
			s := lastMsgID.String
			c.LastMessageID = &s
		}
		if lastMsgAt.Valid {
			t := lastMsgAt.Time
			c.LastMessageAt = &t
		}
		if lastReadMsgID.Valid {
			s := lastReadMsgID.String
			c.LastReadMessageID = &s
		}
		uc := unread
		c.UnreadCount = &uc
		out = append(out, c)
	}
	return out, rows.Err()
}

// isChannelNameTakenErr maps SQLite's UNIQUE-constraint message for the
// channels.name index to ErrChannelNameTaken. Driver does not expose a
// typed sentinel, so string-match is the available signal.
func isChannelNameTakenErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, "channels.name")
}
