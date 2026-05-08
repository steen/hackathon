package presence_e2e_test

import (
	"net/http"
	"testing"
	"time"
)

// TestPresenceAC3_RESTEndpointReturnsOnlineUserIDsAndUsernames asserts
// AC-3 from specs/plans/phase-2/50-feature-presence.md verbatim:
//
//	"A REST endpoint `GET /api/presence` returns the current online
//	user IDs/usernames."
//
// AC-1's tracking_test.go only matched on ID; this test additionally
// pins the documented response shape — `{ok:true, data:{users:[{id,
// username}, ...]}}` — and requires both `id` and `username` to be
// populated for every entry.
//
// Three sub-tests cover the route contract:
//  1. unauthenticated GET → 401 (the route is wrapped in RequireJWT).
//  2. zero WS connections → 200 + empty users list.
//  3. two WS connections → 200 + both users present, each with non-
//     empty id and username matching what /api/auth/register returned.
func TestPresenceAC3_RESTEndpointReturnsOnlineUserIDsAndUsernames(t *testing.T) {
	srv := startServer(t)

	alicePassword := randomSecret(t, 12)
	bobPassword := randomSecret(t, 12)
	aliceID, aliceTok := register(t, srv, "alice", alicePassword)
	bobID, bobTok := register(t, srv, "bob", bobPassword)

	t.Run("unauthenticated_returns_401", func(t *testing.T) {
		status, _, _ := getJSON(t, srv, "/api/presence", "")
		if status != http.StatusUnauthorized {
			t.Errorf("GET /api/presence without bearer: status %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("empty_when_no_ws_connections", func(t *testing.T) {
		users := fetchPresenceUsers(t, srv, aliceTok)
		if len(users) != 0 {
			t.Errorf("GET /api/presence with no WS connections: got %d users, want 0: %+v", len(users), users)
		}
	})

	aliceConn := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn.CloseNow()
	bobConn := dialAuthenticatedWS(t, srv, bobTok)
	defer bobConn.CloseNow()

	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 2
	}) {
		t.Fatalf("debug/subs (seeded general channel) did not reach 2 subscribers within 2s (got %d)", fetchSubscriberCount(t, srv))
	}

	t.Run("returns_id_and_username_for_each_online_user", func(t *testing.T) {
		users := fetchPresenceUsers(t, srv, aliceTok)
		if len(users) != 2 {
			t.Fatalf("GET /api/presence: got %d users, want 2: %+v", len(users), users)
		}

		byID := make(map[string]presenceUser, len(users))
		for _, u := range users {
			if u.ID == "" {
				t.Errorf("GET /api/presence: entry has empty id: %+v (full response: %+v)", u, users)
			}
			if u.Username == "" {
				t.Errorf("GET /api/presence: entry id=%s has empty username (AC-3 requires both ids and usernames): full response %+v", u.ID, users)
			}
			byID[u.ID] = u
		}

		alice, ok := byID[aliceID]
		if !ok {
			t.Fatalf("GET /api/presence missing alice (id=%s): got %+v", aliceID, users)
		}
		if alice.Username != "alice" {
			t.Errorf("GET /api/presence: alice.username = %q, want %q", alice.Username, "alice")
		}

		bob, ok := byID[bobID]
		if !ok {
			t.Fatalf("GET /api/presence missing bob (id=%s): got %+v", bobID, users)
		}
		if bob.Username != "bob" {
			t.Errorf("GET /api/presence: bob.username = %q, want %q", bob.Username, "bob")
		}
	})
}
