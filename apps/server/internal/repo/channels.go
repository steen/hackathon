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
		if strings.Contains(err.Error(), "UNIQUE constraint failed") &&
			strings.Contains(err.Error(), "channels.name") {
			return nil, ErrChannelNameTaken
		}
		return nil, err
	}
	return &Channel{ID: id, Name: name, CreatedAt: created}, nil
}
