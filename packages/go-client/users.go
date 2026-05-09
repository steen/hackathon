// sync with apps/server/internal/http/users_handlers.go::UserSummary
// sync with packages/api-client/src/types.ts::User
package goclient

import (
	"context"
	"net/http"
)

// UserSummary is the {id, username} pair returned by GET /api/users.
// Mirrors apps/server/internal/http/users_handlers.go::UserSummary —
// kept identical so a caller that fetches the directory can pass rows
// straight into presence/sender lookups (same key shape).
//
// BoxPubkey and SignPubkey are Phase-10 identity pubkeys (decision-log
// L2). base64 of raw 32 bytes each; emitted by the server as part of the
// directory response. Omitempty under the L26 optional-first rule until
// every server populator + every consumer is wired up — the JSON tag
// stays `omitempty` so older server builds that have not yet populated
// the columns continue to round-trip without an empty-string sentinel.
type UserSummary struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	BoxPubkey  string `json:"box_pubkey,omitempty"`
	SignPubkey string `json:"sign_pubkey,omitempty"`
}

// usersListResponse is the envelope payload for GET /api/users.
type usersListResponse struct {
	Users []UserSummary `json:"users"`
}

// ListUsers returns every registered user as a {id, username} pair.
// The server sorts by username (then id) for a stable response shape;
// the directory is per-invite-code small (PRD §4) so the response is
// unpaged. Used by callers that need to resolve a username to a user id
// for offline peers (presence only covers currently-online users).
func (c *Client) ListUsers(ctx context.Context) ([]UserSummary, error) {
	var out usersListResponse
	if err := c.do(ctx, http.MethodGet, "/api/users", nil, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}
