package wiring

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"hackathon/apps/server/internal/wsapi"
)

// presenceLookupTimeout caps the per-emit DB round-trip so a slow/locked
// database cannot stall the hub's broadcast goroutine. Presence emits
// fan out under the hub lock; a multi-second SQL stall would back up
// every other broadcast.
const presenceLookupTimeout = 500 * time.Millisecond

// registerPresenceUsername installs the wsapi presence-frame username
// resolver, backed by a direct query against the users table. No-op
// when deps.Repo is nil (the no-DB boot path used by smoke tests):
// without the registration, wsapi.presenceFrame emits frames whose
// `username` field is omitted, matching the pre-#490 wire shape.
//
// The resolver hits the DB once per emit. Presence rates in this app
// are low (one frame per first-connect / last-disconnect per user),
// so a TTL'd cache would be premature; revisit if/when periodic
// reseed (#496) lands.
func registerPresenceUsername(deps Deps) {
	if deps.Repo == nil {
		return
	}
	db := deps.Repo.DB()
	wsapi.SetPresenceUsernameLookup(func(userID string) string {
		ctx, cancel := context.WithTimeout(context.Background(), presenceLookupTimeout)
		defer cancel()
		var username string
		err := db.QueryRowContext(ctx,
			`SELECT username FROM users WHERE id = ?`, userID,
		).Scan(&username)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				slog.Error("presence username lookup", "user_id", userID, "err", err)
			}
			return ""
		}
		return username
	})
}
