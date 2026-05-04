package server_ws_hub_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-1: `apps/server` exposes a `/ws` WebSocket endpoint.
//
// Asserts a websocket.Dial against ws://127.0.0.1:<port>/ws returns
// HTTP 101 Switching Protocols.
func TestAC1_ServerWsHub_UpgradeReturns101(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %v, want 101", resp)
	}
}
