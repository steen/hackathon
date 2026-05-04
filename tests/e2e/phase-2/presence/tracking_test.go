package presence_e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestPresenceAC1_ServerTracksConnectedAuthenticatedUsers asserts AC-1
// from specs/plans/phase-2/50-feature-presence.md verbatim:
//
//	"The server tracks the set of currently connected (authenticated)
//	users derived from active WS connections."
//
// Black-box flow:
//  1. Boot real apps/server.
//  2. Register two users (alice, bob); collect their tokens + IDs.
//  3. Each mints a ws-ticket and dials /ws — both authenticated WS
//     connections sit on the default #general channel.
//  4. Wait for /debug/subs?channel=#general to show 2 subscribers.
//  5. GET /api/presence (with alice's bearer) — assert both alice and
//     bob appear in the response (the set is derived from active WS).
//  6. Close bob's connection. Poll /api/presence until bob's id is
//     gone — alice must remain (the set tracks the *current* live
//     connections, so a dropped connection leaves the set).
func TestPresenceAC1_ServerTracksConnectedAuthenticatedUsers(t *testing.T) {
	srv := startServer(t)

	alicePassword := randomSecret(t, 12)
	bobPassword := randomSecret(t, 12)
	aliceID, aliceTok := register(t, srv, "alice", alicePassword)
	bobID, bobTok := register(t, srv, "bob", bobPassword)

	aliceConn := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn.CloseNow()
	bobConn := dialAuthenticatedWS(t, srv, bobTok)
	defer bobConn.CloseNow()

	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 2
	}) {
		t.Fatalf("debug/subs%s did not reach 2 subscribers within 2s (got %d)", defaultChannel, fetchSubscriberCount(t, srv))
	}

	users := fetchPresenceUsers(t, srv, aliceTok)
	if !containsID(users, aliceID) {
		t.Errorf("/api/presence missing alice (id=%s) while WS connection is open: got %+v", aliceID, users)
	}
	if !containsID(users, bobID) {
		t.Errorf("/api/presence missing bob (id=%s) while WS connection is open: got %+v", bobID, users)
	}

	// Drop bob's connection. The hub's remove path runs on the server's
	// read loop after Close, so poll rather than asserting once.
	if err := bobConn.Close(websocket.StatusNormalClosure, "test done"); err != nil {
		t.Fatalf("close bob conn: %v", err)
	}

	if !waitFor(3*time.Second, func() bool {
		return !containsID(fetchPresenceUsers(t, srv, aliceTok), bobID)
	}) {
		t.Fatalf("/api/presence still lists bob (id=%s) 3s after his connection was closed", bobID)
	}

	// Alice's connection is still open; she must still be tracked.
	final := fetchPresenceUsers(t, srv, aliceTok)
	if !containsID(final, aliceID) {
		t.Errorf("/api/presence dropped alice (id=%s) even though her WS connection is still open: got %+v", aliceID, final)
	}
}

type presenceUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func fetchPresenceUsers(t *testing.T, srv *runningServer, bearer string) []presenceUser {
	t.Helper()
	status, env, raw := getJSON(t, srv, "/api/presence", bearer)
	if status != http.StatusOK {
		t.Fatalf("GET /api/presence: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("GET /api/presence: envelope ok=%v data=%v body=%s", env.OK, env.Data, raw)
	}
	var data struct {
		Users []presenceUser `json:"users"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/presence data: %v body=%s", err, raw)
	}
	return data.Users
}

func containsID(users []presenceUser, id string) bool {
	for _, u := range users {
		if u.ID == id {
			return true
		}
	}
	return false
}
