package http

import (
	"context"
	"database/sql"
	stdhttp "net/http"
	"sort"
	"strings"

	"hackathon/apps/server/internal/hub"
)

// PresenceUser is the {id, username} pair returned by GET /api/presence.
// Username is empty when the row was deleted between the hub snapshot
// and the DB lookup — the user is still online by the hub, so we keep
// the entry rather than silently dropping it; callers can render the
// id in that case.
type PresenceUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// PresenceDeps wires the presence handler. Hub is the source of truth
// for who is online (driven by /ws subscribe/unsubscribe). DB is used
// for the userID → username lookup.
type PresenceDeps struct {
	Hub *hub.Hub
	DB  *sql.DB
}

// PresenceHandlers exposes GET /api/presence. Construct with
// NewPresenceHandlers and wire the List method onto your mux behind
// auth.RequireJWT.
type PresenceHandlers struct {
	deps PresenceDeps
}

// NewPresenceHandlers wires the dependency bag.
func NewPresenceHandlers(deps PresenceDeps) *PresenceHandlers {
	return &PresenceHandlers{deps: deps}
}

// List handles GET /api/presence. Must be wrapped in auth.RequireJWT.
// Returns the current online set sorted by username (then ID) for a
// stable response shape.
func (h *PresenceHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	ids := h.deps.Hub.OnlineUserIDs()
	users, err := lookupUsernames(r.Context(), h.deps.DB, ids)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load presence")
		return
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].Username != users[j].Username {
			return users[i].Username < users[j].Username
		}
		return users[i].ID < users[j].ID
	})
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"users": users})
}

// lookupUsernames fetches usernames for the given IDs in one query.
// Returns one entry per input ID — when a row is missing the username
// field is empty so callers can render the id instead of dropping the
// user. Empty input returns an empty (non-nil) slice.
func lookupUsernames(ctx context.Context, db *sql.DB, ids []string) ([]PresenceUser, error) {
	if len(ids) == 0 {
		return []PresenceUser{}, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	// gosec G202: `placeholders` is built from a fixed alphabet of "?,"
	// — no user input enters the SQL string; every id is bound through
	// the parametrized args slice. Suppression is the standard Go
	// idiom for IN (?,...) when len is dynamic.
	query := `SELECT id, username FROM users WHERE id IN (` + placeholders + `)` //nolint:gosec // G202: see comment above
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	known := make(map[string]string, len(ids))
	for rows.Next() {
		var id, username string
		if err := rows.Scan(&id, &username); err != nil {
			return nil, err
		}
		known[id] = username
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]PresenceUser, 0, len(ids))
	for _, id := range ids {
		out = append(out, PresenceUser{ID: id, Username: known[id]})
	}
	return out, nil
}
