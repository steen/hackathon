package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/coder/websocket"
)

func Watch(ctx context.Context, url string, out io.Writer) error {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()

	// Initiate the close handshake when the parent context is cancelled.
	// Using a derived context bound to Watch's lifetime ensures this
	// goroutine exits when Watch returns for any reason.
	closeCtx, closeCancel := context.WithCancel(ctx)
	defer closeCancel()
	go func() {
		<-closeCtx.Done()
		c.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		// Use Background here: passing the cancellable ctx would abort the
		// connection abruptly on cancel, preventing the close handshake.
		_, data, err := c.Read(context.Background())
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ce websocket.CloseError
			if errors.As(err, &ce) && ce.Code == websocket.StatusNormalClosure {
				return nil
			}
			return err
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	}
}
