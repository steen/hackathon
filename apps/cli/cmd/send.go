package cmd

import (
	"context"
	"strings"

	"github.com/coder/websocket"
)

// Send writes args (joined by single spaces) as one text frame to the given
// WebSocket URL and performs a clean close handshake.
func Send(ctx context.Context, url string, args []string) error {
	c, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = c.CloseNow() }()

	if err := c.Write(ctx, websocket.MessageText, []byte(strings.Join(args, " "))); err != nil {
		return err
	}
	return c.Close(websocket.StatusNormalClosure, "")
}
