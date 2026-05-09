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
	// IsPublic mirrors channels.is_public (decision-log §9 + L24). The
	// default is FALSE so a channel created without an explicit flag is
	// invite-only. Pointer for tri-state on the wire — nil when the
	// caller used a code path that does not select the column (e.g.
	// pre-Phase-10 SELECTs in tests). Immutable after creation per L15.
	IsPublic *bool `json:"is_public,omitempty"`
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
		`SELECT id, name, is_public, created_at FROM channels ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		var isPub bool
		if err := rows.Scan(&c.ID, &c.Name, &isPub, &c.CreatedAt); err != nil {
			return nil, err
		}
		v := isPub
		c.IsPublic = &v
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
		`SELECT id, name, is_public, created_at FROM channels WHERE id = ?`, id)
	var c Channel
	var isPub bool
	if err := row.Scan(&c.ID, &c.Name, &isPub, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	v := isPub
	c.IsPublic = &v
	return &c, nil
}

// CreateChannel inserts a new channel row. The caller supplies the ULID
// so the same id can be returned in the same envelope without a second
// SELECT round-trip.
//
// isPublic toggles the channels.is_public column added by migration 0006
// (decision-log L24): the seeded #general row passes true so new-user
// auto-add at registration time targets it; every other channel passes
// false until the membership API exposes a creator-facing flag.
func (r *Repo) CreateChannel(ctx context.Context, id, name string, isPublic bool, now time.Time) (*Channel, error) {
	created := now.UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels(id, name, is_public, created_at) VALUES (?, ?, ?, ?)`,
		id, name, isPublic, created,
	)
	if err != nil {
		if isChannelNameTakenErr(err) {
			return nil, ErrChannelNameTaken
		}
		return nil, err
	}
	v := isPublic
	return &Channel{ID: id, Name: name, CreatedAt: created, IsPublic: &v}, nil
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
		`SELECT id, name, is_public, created_at FROM channels WHERE id = ?`, id)
	var c Channel
	var isPub bool
	if err := row.Scan(&c.ID, &c.Name, &isPub, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrChannelNotFound
		}
		return nil, err
	}
	v := isPub
	c.IsPublic = &v
	return &c, nil
}

// ListChannelsWithReadState returns every channel with per-viewer
// read-state fields populated, in `last_message_at DESC NULLS LAST`
// order (decision log §9 — activity-ordered listing).
//
// Correctness rests on a single combined argument tying the
// MaterializeChannelReadsTx precondition to the LEFT-JOIN-miss arm.
// MaterializeChannelReadsTx (decision log §11 — auto-materialize on
// listing) inserts a `channel_reads` row pinned to `last_message_id`
// for every channel where the viewer lacks one AND the channel has
// `last_message_id` set. The migration declares `last_read_message_id`
// NOT NULL, so never-messaged channels are the only rows that
// materialization skips. After this call, the LEFT JOIN can miss only
// for never-messaged channels, where the predicate
// `m.id > r.last_read_message_id` matches zero rows because the
// channel itself has zero messages — the count is structurally 0
// regardless of how the NULL `r.last_read_message_id` evaluates.
//
// Skipping the materialize call on a channel that does have messages
// is a precondition violation: the LEFT JOIN misses, `m.id > NULL`
// evaluates to NULL (falsy in WHERE), and the count comes back 0 —
// silently undercounting unread messages for the viewer. Issue #938
// dropped the prior empty-string COALESCE belt that masked exactly
// this case as a different wrong answer (full-channel count instead
// of 0); the belt was reachable only via this same precondition
// violation. Either way, the materialize call is load-bearing for any
// non-empty channel — see
// TestListChannelsWithReadStateRequiresMaterializeForNonEmpty.
func (r *Repo) ListChannelsWithReadState(ctx context.Context, viewerUserID string) ([]Channel, error) {
	if err := r.MaterializeChannelReadsTx(ctx, viewerUserID); err != nil {
		return nil, err
	}
	// Phase-10 §6 + L25: GET /api/channels filters to channels where the
	// viewer is a member. The JOIN against channel_members on
	// (channel_id, user_id) filters server-side so the listing reveals
	// no metadata about non-member channels.
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.name, c.is_public, c.created_at,
		        c.last_message_id, c.last_message_at,
		        r.last_read_message_id,
		        (SELECT COUNT(*) FROM messages m
		          WHERE m.channel_id = c.id
		            AND m.id > r.last_read_message_id) AS unread_count
		   FROM channels c
		   JOIN channel_members cm
		     ON cm.channel_id = c.id AND cm.user_id = ?
		   LEFT JOIN channel_reads r
		     ON r.channel_id = c.id AND r.user_id = ?
		  ORDER BY c.last_message_at DESC NULLS LAST, c.id ASC`,
		viewerUserID, viewerUserID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Channel, 0)
	for rows.Next() {
		var (
			c             Channel
			isPub         bool
			lastMsgID     sql.NullString
			lastMsgAt     sql.NullTime
			lastReadMsgID sql.NullString
			unread        int
		)
		if err := rows.Scan(&c.ID, &c.Name, &isPub, &c.CreatedAt,
			&lastMsgID, &lastMsgAt, &lastReadMsgID, &unread); err != nil {
			return nil, err
		}
		v := isPub
		c.IsPublic = &v
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
