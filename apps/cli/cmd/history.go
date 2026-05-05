package cmd

import (
	"context"
	"flag"
	"fmt"
	"time"

	goclient "hackathon/packages/go-client"
)

// History implements `chatd history <channel> [--limit N] [--before ID]`.
// Prints messages newest-first as `<rfc3339>\t<sender>\t<body>`. Body
// is single-line by contract (server rejects newlines), so this stays
// grep-friendly.
func History(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	limit := fs.Int("limit", 0, "max messages to return (0 = server default)")
	before := fs.String("before", "", "ULID cursor; only return messages older than this id")
	// stdlib flag.Parse stops at the first non-flag token, so the
	// AC-documented `chatd history <channel> --limit N` order would
	// otherwise leave --limit in fs.Args(). Split first, parse the
	// flag tail, then assign the positional ourselves.
	flagArgs, positional := splitFlagsAndPositional(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return fmt.Errorf("usage: chatd history <channel> [--limit N] [--before ID]\n(run 'chatd history -h' for full flags)")
	}
	channel := positional[0]
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("history", err)
	}
	msgs, err := client.ListMessages(ctx, channel, goclient.ListMessagesOptions{
		Limit:  *limit,
		Before: *before,
	})
	if err != nil {
		return err
	}
	for _, m := range msgs {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n",
			m.CreatedAt.UTC().Format(time.RFC3339Nano), m.SenderUserID, m.Body)
	}
	return nil
}
