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
type Channel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrChannelNameTaken is returned by CreateChannel when the UNIQUE
// constraint on channels.name trips. Callers map this to a 409.
var ErrChannelNameTaken = errors.New("repo: channel name already taken")

// ErrChannelNotFound is returned by RenameChannel when the supplied id
// does not match any row. Callers map this to a 404.
var ErrChannelNotFound = errors.New("repo: channel not found")

// ListChannels returns every channel ordered by id (ULID — chronological).
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
