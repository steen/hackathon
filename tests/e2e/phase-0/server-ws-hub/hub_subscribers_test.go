package server_ws_hub_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-2: An in-memory hub tracks subscribers per channel; channel is
// hardcoded to `#general` for this phase.
//
// Two unauthenticated WS dials both register against `#general`;
// /debug/subs?channel=%23general reaches 2 within a deadline.
func TestAC2_ServerWsHub_SubscribersOnGeneral(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	waitForSubscriberCount(t, srv, "#general", 2, 2*time.Second)
}
