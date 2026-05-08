package presence_e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestPresenceAC2_BroadcastsJoinAndLeaveOnConnectAndDisconnect asserts
// AC-2 from specs/plans/phase-2/50-feature-presence.md verbatim:
//
//	"An event is broadcast when a user connects or disconnects
//	(presence event with kind join / leave)."
//
// Black-box flow:
//  1. Boot real apps/server.
//  2. Register alice + bob.
//  3. Alice dials /ws first (subscriber). Drain her own self-join
//     frame so it doesn't accidentally satisfy bob's assertion.
//  4. Bob dials /ws. Within 2s, alice must receive a presence frame
//     with kind="join" and user_id=bob's id.
//  5. Bob closes the connection. Within 3s, alice must receive a
//     presence frame with kind="leave" and user_id=bob's id.
//
// The server-side cleanup runs on the read loop after Close, so the
// leave deadline is wider than the join deadline.
func TestPresenceAC2_BroadcastsJoinAndLeaveOnConnectAndDisconnect(t *testing.T) {
	srv := startServer(t)

	alicePassword := randomSecret(t, 12)
	bobPassword := randomSecret(t, 12)
	aliceID, aliceTok := register(t, srv, "alice", alicePassword)
	bobID, bobTok := register(t, srv, "bob", bobPassword)

	aliceConn := dialAuthenticatedWS(t, srv, aliceTok)
	defer aliceConn.CloseNow()

	// Alice's own join frame fires first (Subscribe runs before
	// AddPresence in handler.go, so the joining user sees self-join).
	// Drain it so the next-frame assertion below is unambiguous.
	aliceFrames := startPresenceCollector(t, aliceConn)
	if got := awaitPresence(t, aliceFrames, 2*time.Second); got == nil {
		t.Fatalf("alice did not observe her own join frame within 2s")
	} else if got.Kind != "join" || got.UserID != aliceID {
		t.Fatalf("alice's first frame: kind=%q user_id=%q, want join/%s", got.Kind, got.UserID, aliceID)
	}

	// Wait for alice's subscription to register before bob dials so
	// there is no race between bob's join broadcast and alice's
	// subscription appearing in the fanout target set.
	if !waitFor(2*time.Second, func() bool {
		return fetchSubscriberCount(t, srv) == 1
	}) {
		t.Fatalf("debug/subs (seeded general channel) did not reach 1 subscriber within 2s before bob dial")
	}

	bobConn := dialAuthenticatedWS(t, srv, bobTok)

	join := awaitPresence(t, aliceFrames, 2*time.Second)
	if join == nil {
		t.Fatalf("alice did not receive a presence frame for bob's connect within 2s")
	}
	if join.Kind != "join" {
		t.Errorf("bob connect: kind=%q, want %q", join.Kind, "join")
	}
	if join.UserID != bobID {
		t.Errorf("bob connect: user_id=%q, want %q (bob)", join.UserID, bobID)
	}

	if err := bobConn.Close(websocket.StatusNormalClosure, "test done"); err != nil {
		t.Fatalf("close bob conn: %v", err)
	}

	leave := awaitPresence(t, aliceFrames, 3*time.Second)
	if leave == nil {
		t.Fatalf("alice did not receive a presence frame for bob's disconnect within 3s")
	}
	if leave.Kind != "leave" {
		t.Errorf("bob disconnect: kind=%q, want %q", leave.Kind, "leave")
	}
	if leave.UserID != bobID {
		t.Errorf("bob disconnect: user_id=%q, want %q (bob)", leave.UserID, bobID)
	}
}

// presenceFrameDecoded is the test-side decoded shape of a
// `{type:"presence", data:{kind, user_id}}` envelope as defined in
// apps/server/internal/wsapi/presence.go.
type presenceFrameDecoded struct {
	Kind   string
	UserID string
}

// startPresenceCollector spawns a goroutine that reads frames from
// `conn` and forwards parsed presence frames onto the returned
// channel. Non-presence frames are dropped so the caller can do
// next-frame assertions without filtering. The goroutine exits when
// the test ends (t.Cleanup cancels its context) or the read loop
// returns an error (typically the connection closing).
func startPresenceCollector(t *testing.T, conn *websocket.Conn) <-chan presenceFrameDecoded {
	t.Helper()
	out := make(chan presenceFrameDecoded, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer close(out)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var env struct {
				Type string `json:"type"`
				Data struct {
					Kind   string `json:"kind"`
					UserID string `json:"user_id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}
			if env.Type != "presence" {
				continue
			}
			select {
			case out <- presenceFrameDecoded{Kind: env.Data.Kind, UserID: env.Data.UserID}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// awaitPresence returns the next presence frame on `frames` or nil on
// timeout. Returning a pointer so the caller can disambiguate "no
// frame" from "zero-value frame".
func awaitPresence(t *testing.T, frames <-chan presenceFrameDecoded, timeout time.Duration) *presenceFrameDecoded {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			return nil
		}
		return &f
	case <-time.After(timeout):
		return nil
	}
}
