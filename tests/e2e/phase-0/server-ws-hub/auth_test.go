package server_ws_hub_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-5: No authentication is required at this stage.
//
// Phase-1 wired ws-ticket auth on /ws (SEC-12), but only when the
// server is launched with CHAT_DB_PATH set (apps/server/main.go
// branches on `repository != nil` to mount auth). The phase-0 boot
// path the spec describes — `go run ./apps/server` with no DB —
// preserves the unauthenticated /ws contract. startServer launches
// in that no-DB configuration, so a bare websocket.Dial without
// Authorization, ticket, or any other credential must still upgrade.
func TestAC5_ServerWsHub_NoAuthRequired(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// websocket.Dial with nil opts sends no Authorization header. A
	// successful upgrade therefore proves the server doesn't require
	// one in the phase-0 boot path.
	c, resp, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial without auth: %v", err)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %v, want 101 (server should not require auth in phase-0 boot)", resp)
	}
}
