package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"

	"hackathon/apps/cli/internal/config"
)

// Login implements `chatd login [<username>]`. Username may also be
// passed via --username; password via --password / $CHAT_PASSWORD;
// missing values are prompted. The returned token is persisted.
func Login(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	usernameFlag := fs.String("username", "", "username (positional argument also accepted)")
	password := fs.String("password", "", "password (overrides $CHAT_PASSWORD; prompted if neither set)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	username := *usernameFlag
	if rest := fs.Args(); len(rest) > 0 {
		if username != "" && rest[0] != username {
			return fmt.Errorf("login: --username and positional username disagree")
		}
		username = rest[0]
	}
	username, err := readVisible(env, username, "Username")
	if err != nil {
		return err
	}
	if username == "" {
		return fmt.Errorf("username is required")
	}

	pw := *password
	if pw == "" {
		pw = os.Getenv("CHAT_PASSWORD")
	}
	pw, err = readSecret(env, pw, "Password")
	if err != nil {
		return err
	}
	if pw == "" {
		return fmt.Errorf("password is required")
	}

	client, cfg, err := newClient(env, false)
	if err != nil {
		return err
	}
	resp, err := client.Login(ctx, username, pw)
	if err != nil {
		return err
	}
	cfg.Server = client.BaseURL()
	cfg.Token = resp.Token
	cfg.User = &config.User{ID: resp.User.ID, Username: resp.User.Username}
	if err := saveConfig(env, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "Logged in as %s\n", resp.User.Username)
	return nil
}
