package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	goclient "hackathon/packages/go-client"
)

// Send implements `chatd send <channel> <message>`. When <message> is
// "-", the body is read from stdin (terminating newline trimmed). The
// server's posted-message id is printed to stdout so scripts can chain.
func Send(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: chatd send <channel> <message|->\n(run 'chatd send -h' for full flags)")
	}
	channel := rest[0]
	body := strings.Join(rest[1:], " ")

	if body == "-" {
		raw, err := io.ReadAll(env.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body = strings.TrimRight(string(raw), "\r\n")
		if body == "" {
			return fmt.Errorf("send: stdin produced an empty message")
		}
	}

	client, _, err := newClient(env, true)
	if err != nil {
		return err
	}
	msg, err := client.PostMessage(ctx, channel, goclient.PostMessageOptions{Body: body})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(env.Stdout, msg.ID)
	return nil
}
