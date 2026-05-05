package cmd

import (
	"context"
	"flag"
	"fmt"
)

// Channels implements `chatd channels`. Lists every channel the server
// knows about, one per line, as `<id>\t<name>`. Tabular output keeps
// the command pipe-friendly without extra flags.
func Channels(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels", err)
	}
	chans, err := client.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, c := range chans {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", c.ID, c.Name)
	}
	return nil
}
