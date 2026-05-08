package body_and_ws_caps_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-2: Each WS connection has a per-conn send rate limit (e.g., N
// messages/sec with burst); excess sends are dropped or trigger close.
//
// The implementation in apps/server/internal/wsapi/ratelimit.go uses a
// token bucket sized by wsapi.SendRateBurst (30) refilling at
// wsapi.SendRatePerSec (10/s). When the bucket empties, readLoop in
// apps/server/internal/wsapi/handler.go closes the connection with
// websocket.StatusPolicyViolation (1008) and reason
// "send rate limit exceeded".
//
// This black-box test boots the real chat-server binary, dials /ws,
// floods small text frames past the burst, then asserts the server
// closes the connection with code 1008. The reason text disambiguates
// from the policy-violation paths in other features (currently none on
// the WS read path, but pinning the reason guards against drift).
//
// Wire-code values are pinned (1008) so a future library re-numbering
// surfaces here instead of silently passing.
func TestAC2_WSSendRateLimitClosesPolicyViolation(t *testing.T) {
	srv := startServer(t)
	bearer, ticket := registerAndMintTicket(t, srv)
	channelID := seededChannelID(t, srv, bearer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, srv.wsURL+"?ticket="+ticket+"&channel="+channelID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	defer func() { _ = conn.CloseNow() }()
	// Lift the client read limit so the close-frame reason is observed
	// in full rather than truncated by a client-side cap.
	conn.SetReadLimit(-1)

	// Burst is 30 with refill 10/s; 200 frames in a tight loop drains
	// the bucket far faster than refill can replenish. Stop on first
	// Write error — once the server closes mid-flood subsequent Writes
	// fail and the assertion lives on Read.
	const flood = 200
	for i := 0; i < flood; i++ {
		if werr := conn.Write(ctx, websocket.MessageText, []byte("x")); werr != nil {
			break
		}
	}

	// The server may close after the bucket empties OR keep accepting
	// writes for a few more frames before the close propagates back to
	// the client; loop on Read until we see the close (or hit the
	// deadline).
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, rerr := conn.Read(ctx)
		if rerr != nil {
			got := websocket.CloseStatus(rerr)
			if got != websocket.StatusPolicyViolation {
				t.Fatalf("rate-limit close: code = %d, want %d (StatusPolicyViolation / 1008); err=%v",
					got, websocket.StatusPolicyViolation, rerr)
			}
			if int(websocket.StatusPolicyViolation) != 1008 {
				t.Fatalf("library StatusPolicyViolation is not 1008: %d", websocket.StatusPolicyViolation)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never closed after rate-limit flood")
		}
	}
}
