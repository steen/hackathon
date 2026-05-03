package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/coder/websocket"
)

// Send dials url, writes a single text frame containing args joined by spaces,
// and closes the connection. The returned error is non-nil when the dial fails
// or when the write fails.
func Send(ctx context.Context, url string, args []string) error {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}
	defer c.CloseNow()

	payload := strings.Join(args, " ")
	if err := c.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return c.Close(websocket.StatusNormalClosure, "")
}
