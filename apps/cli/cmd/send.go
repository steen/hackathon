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
	if err := c.Write(ctx, websocket.MessageText, []byte(strings.Join(args, " "))); err != nil {
		c.CloseNow()
		return err
	}
	return c.Close(websocket.StatusNormalClosure, "")
}
