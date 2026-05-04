// Package seed performs first-boot data seeding. It runs after migrations
// and before the HTTP listener accepts connections so that a fresh install
// has a #general channel to talk in (phase-3 demo flow).
//
// Every operation here is idempotent: re-running the server with an already
// seeded database is a no-op and does not error on the channels.name UNIQUE
// constraint.
package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// GeneralChannelName is the canonical name of the seeded default channel.
// Exported so callers (and tests) can reference it without retyping the
// literal.
const GeneralChannelName = "general"

// EnsureGeneralChannel inserts a channel named "general" if no channel with
// that name exists yet. Returns nil on the first successful insert and on
// every idempotent re-run; the only error path is an unexpected database
// failure.
//
// Concurrency: two processes racing the same fresh DB would both pass the
// existence check and one would lose the UNIQUE constraint on insert. That
// loser is treated as a successful no-op (the row exists by the time we
// re-check) — the post-condition the caller cares about still holds.
func EnsureGeneralChannel(ctx context.Context, r *repo.Repo) error {
	if r == nil {
		return errors.New("seed: nil *repo.Repo")
	}
	db := r.DB()
	if db == nil {
		return errors.New("seed: repo has nil *sql.DB")
	}

	exists, err := generalExists(ctx, db)
	if err != nil {
		return fmt.Errorf("seed: check general: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := r.CreateChannel(ctx, ids.NewULID(), GeneralChannelName, time.Now()); err != nil {
		if errors.Is(err, repo.ErrChannelNameTaken) {
			// Concurrent seeder won the race; the row exists.
			return nil
		}
		return fmt.Errorf("seed: insert general: %w", err)
	}
	return nil
}

// generalExists reports whether a channel named "general" is present.
// Parameterized SQL only (PRD §6).
func generalExists(ctx context.Context, db *sql.DB) (bool, error) {
	row := db.QueryRowContext(ctx,
		`SELECT 1 FROM channels WHERE name = ? LIMIT 1`, GeneralChannelName)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
