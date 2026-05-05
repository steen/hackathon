package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"

	"hackathon/apps/cli/internal/config"
)

// Register implements `chatd register <username>`. It prompts for any
// credential not supplied via flag (--password, --invite-code) or env
// (CHAT_PASSWORD, CHAT_INVITE_CODE), POSTs /api/auth/register, and
// stores the returned token in the config file.
func Register(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	password := fs.String("password", "", "password (overrides $CHAT_PASSWORD; prompted if neither set)")
	invite := fs.String("invite-code", "", "invite code (overrides $CHAT_INVITE_CODE; prompted if neither set)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd register <username>\n(run 'chatd register -h' for full flags)")
	}
	username := rest[0]

	pw := *password
	if pw == "" {
		pw = os.Getenv("CHAT_PASSWORD")
	}
	pw, err := readSecret(env, pw, "Password")
	if err != nil {
		return err
	}
	if pw == "" {
		return fmt.Errorf("password is required")
	}

	code := *invite
	if code == "" {
		code = os.Getenv("CHAT_INVITE_CODE")
	}
	code, err = readSecret(env, code, "Invite code")
	if err != nil {
		return err
	}
	if code == "" {
		return fmt.Errorf("invite code is required")
	}

	client, cfg, err := newClient(env, false)
	if err != nil {
		return err
	}
	resp, err := client.Register(ctx, username, pw, code)
	if err != nil {
		return err
	}
	cfg.Server = client.BaseURL()
	cfg.Token = resp.Token
	cfg.User = &config.User{ID: string(resp.User.ID), Username: resp.User.Username}
	if err := saveConfig(env, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "Registered as %s\n", resp.User.Username)
	return nil
}
