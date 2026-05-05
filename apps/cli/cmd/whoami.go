package cmd

import (
	"context"
	"flag"
	"fmt"
)

// Whoami implements `chatd whoami`. Prints the cached username when a
// token is stored locally and the server still accepts it; returns
// non-nil error (driving exit-1) when not logged in or the token is stale.
func Whoami(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("whoami", err)
	}
	user, err := client.Me(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(env.Stdout, user.Username)
	return nil
}
