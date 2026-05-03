package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/coder/websocket"
)

// Watch dials url, reads incoming text frames in a loop, and writes each one
// followed by a newline to out. It returns nil on a clean server-side close,
// and ctx.Err() (context.Canceled or context.DeadlineExceeded) when the caller
// cancels the context.
func Watch(ctx context.Context, url string, out io.Writer) error {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}
	defer c.CloseNow()

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			var ce websocket.CloseError
			if errors.As(err, &ce) && ce.Code == websocket.StatusNormalClosure {
				return nil
			}
			return nil
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	}
}
