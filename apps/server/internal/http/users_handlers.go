package http

import (
	"context"
	"database/sql"
	"encoding/base64"
	stdhttp "net/http"
	"sort"
)

// UserSummary is the {id, username} pair returned by GET /api/users.
// Same shape as PresenceUser so the frontend can merge the two
// directories with a single key.
//
// BoxPubkey/SignPubkey are Phase-10 identity pubkeys (decision-log L2).
// base64 of raw 32 bytes each; omitempty so a row whose columns are
// still NULL renders {id, username} for clients that don't yet read
// pubkeys.
type UserSummary struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	BoxPubkey  string `json:"box_pubkey,omitempty"`
	SignPubkey string `json:"sign_pubkey,omitempty"`
}

// UsersDeps wires the users handler. Reads user rows directly from the
// authoritative DB; no caching layer because the table is small (per-
// invite-code seed) and the response is consumed once per session.
type UsersDeps struct {
	DB *sql.DB
}

// UsersHandlers exposes GET /api/users. Construct via NewUsersHandlers
// and wire the List method onto your mux behind auth.RequireJWT.
type UsersHandlers struct {
	deps UsersDeps
}

// NewUsersHandlers wires the dependency bag.
func NewUsersHandlers(deps UsersDeps) *UsersHandlers {
	return &UsersHandlers{deps: deps}
}

// List handles GET /api/users. Must be wrapped in auth.RequireJWT.
// Returns every registered user as a {id, username} pair, sorted by
// username (then ID) for a stable response shape. Used by the web
// client to resolve sender_user_id -> username for offline senders
// (presence only covers currently-online users).
func (h *UsersHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	users, err := listAllUsers(r.Context(), h.deps.DB)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load users")
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

// listAllUsers queries every row in the users table. The table is
// per-invite-code small (PRD §4) and the lookup is cached client-side
// for the session, so a full scan is acceptable; if the directory
// grows past hundreds we'd switch to paged or filtered queries.
func listAllUsers(ctx context.Context, db *sql.DB) ([]UserSummary, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, box_pubkey, sign_pubkey FROM users`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]UserSummary, 0, 16)
	for rows.Next() {
		var u UserSummary
		var box, sign []byte
		if err := rows.Scan(&u.ID, &u.Username, &box, &sign); err != nil {
			return nil, err
		}
		if len(box) > 0 {
			u.BoxPubkey = base64.StdEncoding.EncodeToString(box)
		}
		if len(sign) > 0 {
			u.SignPubkey = base64.StdEncoding.EncodeToString(sign)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
