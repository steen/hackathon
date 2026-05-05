package cmd

import (
	"context"
	"flag"
	"fmt"
	"time"

	goclient "hackathon/packages/go-client"
)

// initialWatchBackoff and maxWatchBackoff bound the reconnect delay.
// The cap of 30s matches the server's WS ticket TTL — past that point
// a retry has to mint a fresh ticket anyway, so a longer wait buys
// nothing.
const (
	initialWatchBackoff = 500 * time.Millisecond
	maxWatchBackoff     = 30 * time.Second
)

// Watch implements `chatd watch <channel>`. Streams each new message
// as `<rfc3339>\t<sender>\t<body>\n`. On disconnect the function
// reconnects with exponential backoff capped at maxWatchBackoff until
// ctx is cancelled.
func Watch(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	once := fs.Bool("once", false, "exit after the first stream closes (skip reconnect)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd watch <channel>\n(run 'chatd watch -h' for full flags)")
	}
	channel := rest[0]

	client, _, err := newClient(env, true)
	if err != nil {
		return err
	}

	backoff := initialWatchBackoff
	for {
		streamErr := streamOnce(ctx, env, client, channel)
		if ctx.Err() != nil {
			// Context cancellation is treated as a clean exit; the
			// underlying read error is a side-effect of the cancel.
			return nil //nolint:nilerr // intentional: ctx.Err shadows streamErr on cancel
		}
		if *once {
			return streamErr
		}
		if streamErr != nil {
			_, _ = fmt.Fprintf(env.Stderr, "watch: %v (reconnecting in %s)\n", streamErr, backoff)
		} else {
			_, _ = fmt.Fprintf(env.Stderr, "watch: stream closed (reconnecting in %s)\n", backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxWatchBackoff {
			backoff = maxWatchBackoff
		}
	}
}

// streamOnce opens a single watch session and prints messages until
// the events channel closes. Returns nil on a clean close and the
// underlying error when the upgrade fails.
func streamOnce(ctx context.Context, env *Env, client *goclient.Client, channel string) error {
	events, err := client.Watch(ctx, goclient.WatchOptions{ChannelID: channel})
	if err != nil {
		return err
	}
	for ev := range events {
		if ev.Message == nil {
			continue
		}
		m := ev.Message
		if _, err := fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n",
			m.CreatedAt.UTC().Format(time.RFC3339Nano), m.SenderUserID, m.Body); err != nil {
			return err
		}
	}
	return nil
}
