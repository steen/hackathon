package cmd

import (
	"context"
	"flag"
	"fmt"

	"hackathon/apps/cli/internal/config"
)

// Logout implements `chatd logout`. POSTs /api/auth/logout to bump the
// server token version, then clears the local config file. The local
// clear runs even when the server call fails so a stale-token state on
// disk does not strand the user; the server-side error is surfaced.
func Logout(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, cfg, err := newClient(env, false)
	if err != nil {
		return err
	}
	var serverErr error
	if cfg.Token != "" {
		serverErr = client.Logout(ctx)
	}
	if err := config.Clear(env.ConfigDir); err != nil {
		return fmt.Errorf("clear config: %w", err)
	}
	if serverErr != nil {
		return fmt.Errorf("server logout: %w", serverErr)
	}
	_, _ = fmt.Fprintln(env.Stdout, "Logged out")
	return nil
}
