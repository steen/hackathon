package cmd

import (
	"context"
	"strings"

	"github.com/coder/websocket"
)

func Send(ctx context.Context, url string, args []string) error {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow() // safety net — no-op once Close runs cleanly below

	if err := c.Write(ctx, websocket.MessageText, []byte(strings.Join(args, " "))); err != nil {
		return err
	}
	return c.Close(websocket.StatusNormalClosure, "")
}
