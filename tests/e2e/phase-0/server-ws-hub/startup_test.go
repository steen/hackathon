package server_ws_hub_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-4: Server starts via `go run ./apps/server` and listens on a
// configurable port (env var or default).
//
// startServer launches the built binary with CHAT_SERVER_PORT=<port>
// and waitForPort blocks until the chosen port is listening. If the
// env var were ignored the dial below would either hit the wrong port
// or waitForPort would have already failed. The dial is a
// belt-and-suspenders check that the server is on EXACTLY that port.
func TestAC4_ServerWsHub_ListensOnConfiguredPort(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial port %d: %v", srv.port, err)
	}
	defer c.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status on configured port %d = %d, want 101", srv.port, resp.StatusCode)
	}
}
