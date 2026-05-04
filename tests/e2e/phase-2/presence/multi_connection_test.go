package presence_e2e_test

import (
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestPresenceAC5_SameUserMultipleConnectionsCountedOnceUntilLastCloses
// asserts AC-5 from specs/plans/phase-2/50-feature-presence.md verbatim:
//
//	"Presence is consistent if the same user has multiple connections
//	(counted as online while at least one connection is open)."
//
// Black-box flow:
//  1. Boot real apps/server.
//  2. Register alice (the multi-connection user) and bob (an observer
//     with his own WS connection so he can collect presence frames
//     broadcast via BroadcastAll).
//  3. Bob dials /ws first; drain his own self-join frame so the next
//     presence frame on his channel will be alice-related.
//  4. Alice opens three WS connections (three tickets, three dials).
//     Assert /api/presence returns alice exactly once (not three
//     times — set semantics, not bag).
//  5. Bob's collector must observe exactly one `kind:"join"` for alice
//     across all three opens (only the first connection ref-counts the
//     user from 0→1; the second and third must not re-broadcast).
//  6. Close alice's first connection. /api/presence still lists alice;
//     bob receives no further presence frames.
//  7. Close alice's second connection. /api/presence still lists
//     alice; bob still sees no further presence frames.
//  8. Close alice's third (last) connection. /api/presence stops
//     listing alice (poll with deadline — server-side cleanup runs on
//     the WS read loop after Close). Bob observes exactly one
//     `kind:"leave"` for alice across the three closes.
func TestPresenceAC5_SameUserMultipleConnectionsCountedOnceUntilLastCloses(t *testing.T) {
	srv := startServer(t)

	alicePassword := randomSecret(t, 12)
	bobPassword := randomSecret(t, 12)
	aliceID, aliceTok := register(t, srv, "alice", alicePassword)
	bobID, bobTok := register(t, srv, "bob", bobPassword)

	// Bob observes presence frames for alice. He dials first so his
	// subscription is in the BroadcastAll target set when alice's
	// connections come up.
	bobConn := dialAuthenticatedWS(t, srv, bobTok)
	defer bobConn.CloseNow()

	bobFrames := startPresenceCollector(t, bobConn)

	// Drain bob's own self-join so subsequent assertions see only
	// alice-related frames.
	if got := awaitPresence(t, bobFrames, 2*time.Second); got == nil {
		t.Fatalf("bob did not observe his own join frame within 2s")
	} else if got.Kind != "join" || got.UserID != bobID {
		t.Fatalf("bob's first frame: kind=%q user_id=%q, want join/%s", got.Kind, got.UserID, bobID)
	}

	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 1
	}) {
		t.Fatalf("debug/subs#general did not reach 1 subscriber within 2s before alice dials")
	}

	// Alice opens three connections. Track each so the test can close
	// them in a controlled order.
	aliceConn1 := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn1.CloseNow()
	aliceConn2 := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn2.CloseNow()
	aliceConn3 := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn3.CloseNow()

	if !waitFor(3*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 4
	}) {
		t.Fatalf("debug/subs#general did not reach 4 subscribers within 3s (got %d)", fetchSubscriberCount(t, srv))
	}

	// Step 1: only one join event must have fired for alice across
	// the three opens. AddPresence ref-counts: 0→1 broadcasts,
	// 1→2 and 2→3 do not. Assert by reading the next frame and
	// then asserting no second presence frame arrives within a small
	// window.
	join := awaitPresence(t, bobFrames, 2*time.Second)
	if join == nil {
		t.Fatalf("bob never received a presence frame for alice's first connection")
	}
	if join.Kind != "join" {
		t.Errorf("alice multi-connect first event: kind=%q, want %q", join.Kind, "join")
	}
	if join.UserID != aliceID {
		t.Errorf("alice multi-connect first event: user_id=%q, want %q (alice)", join.UserID, aliceID)
	}
	// Assert no further presence frame arrives within a quiet window —
	// the second and third connections must not re-broadcast a join.
	if extra := awaitPresence(t, bobFrames, 500*time.Millisecond); extra != nil {
		t.Errorf("alice multi-connect: extra presence frame arrived after first join (kind=%q user_id=%q); ref-counted presence must broadcast exactly once on 0→1", extra.Kind, extra.UserID)
	}

	// Step 2: /api/presence lists alice exactly once.
	users := fetchPresenceUsers(t, srv, bobTok)
	aliceCount := 0
	for _, u := range users {
		if u.ID == aliceID {
			aliceCount++
		}
	}
	if aliceCount != 1 {
		t.Errorf("/api/presence with three alice WS connections: alice (id=%s) appears %d times, want 1: full response %+v", aliceID, aliceCount, users)
	}

	// Step 3: close the first connection. Alice still online; no
	// presence frame fires (count drops 3→2).
	if err := aliceConn1.Close(websocket.StatusNormalClosure, "ac5 step 3"); err != nil {
		t.Fatalf("close aliceConn1: %v", err)
	}
	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 3
	}) {
		t.Fatalf("debug/subs#general did not drop to 3 subscribers within 2s after closing aliceConn1 (got %d)", fetchSubscriberCount(t, srv))
	}
	if !containsID(fetchPresenceUsers(t, srv, bobTok), aliceID) {
		t.Errorf("/api/presence dropped alice (id=%s) after closing 1 of 3 connections — AC-5 requires online while ≥1 connection open", aliceID)
	}
	// Assert no further presence frame arrives within a quiet window —
	// a 3→2 ref-count drop must not broadcast.
	if extra := awaitPresence(t, bobFrames, 500*time.Millisecond); extra != nil {
		t.Errorf("after closing aliceConn1: stray presence frame (kind=%q user_id=%q); ref-counted presence must not broadcast on 3→2", extra.Kind, extra.UserID)
	}

	// Step 4: close the second connection. Alice still online; no
	// presence frame fires (count drops 2→1).
	if err := aliceConn2.Close(websocket.StatusNormalClosure, "ac5 step 4"); err != nil {
		t.Fatalf("close aliceConn2: %v", err)
	}
	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 2
	}) {
		t.Fatalf("debug/subs#general did not drop to 2 subscribers within 2s after closing aliceConn2 (got %d)", fetchSubscriberCount(t, srv))
	}
	if !containsID(fetchPresenceUsers(t, srv, bobTok), aliceID) {
		t.Errorf("/api/presence dropped alice (id=%s) after closing 2 of 3 connections — AC-5 requires online while ≥1 connection open", aliceID)
	}
	// Assert no further presence frame arrives within a quiet window —
	// a 2→1 ref-count drop must not broadcast.
	if extra := awaitPresence(t, bobFrames, 500*time.Millisecond); extra != nil {
		t.Errorf("after closing aliceConn2: stray presence frame (kind=%q user_id=%q); ref-counted presence must not broadcast on 2→1", extra.Kind, extra.UserID)
	}

	// Step 5: close the third (last) connection. Alice goes offline;
	// exactly one leave event fires.
	if err := aliceConn3.Close(websocket.StatusNormalClosure, "ac5 step 5"); err != nil {
		t.Fatalf("close aliceConn3: %v", err)
	}
	if !waitFor(3*time.Second, func() bool {
		return !containsID(fetchPresenceUsers(t, srv, bobTok), aliceID)
	}) {
		t.Fatalf("/api/presence still lists alice (id=%s) 3s after closing her last connection — AC-5 requires offline once last connection closes", aliceID)
	}

	leave := awaitPresence(t, bobFrames, 3*time.Second)
	if leave == nil {
		t.Fatalf("bob never received a presence leave frame for alice after her last connection closed")
	}
	if leave.Kind != "leave" {
		t.Errorf("alice last-disconnect event: kind=%q, want %q", leave.Kind, "leave")
	}
	if leave.UserID != aliceID {
		t.Errorf("alice last-disconnect event: user_id=%q, want %q (alice)", leave.UserID, aliceID)
	}
	// Assert no further presence frame arrives within a quiet window —
	// the 1→0 leave must broadcast exactly once.
	if extra := awaitPresence(t, bobFrames, 500*time.Millisecond); extra != nil {
		t.Errorf("after alice's last disconnect: stray presence frame (kind=%q user_id=%q); ref-counted presence must broadcast leave exactly once", extra.Kind, extra.UserID)
	}
}
