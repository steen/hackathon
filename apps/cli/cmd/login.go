package cmd

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"hackathon/apps/cli/internal/config"
)

// Login implements `chatd login [<username>]`. Username may also be
// passed via --username; password via --password / $CHAT_PASSWORD;
// missing values are prompted. The returned token is persisted.
//
// Phase-10 identity (decision-log §4 + L4 + L11): the user is also
// prompted for an identity passphrase via no-echo. After the server
// authenticates the login password, the CLI re-derives the identity
// locally and compares the resulting sign_pubkey against the server-
// returned one — if they disagree, the user typed the wrong identity
// passphrase, and the CLI rejects with a clear error before the
// derived seed is written to disk. Passing $CHAT_IDENTITY_PASSPHRASE
// skips the prompt.
func Login(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	usernameFlag := fs.String("username", "", "username (positional argument also accepted)")
	password := fs.String("password", "", "password (overrides $CHAT_PASSWORD; prompted if neither set)")
	identity := fs.String("identity-passphrase", "", "identity passphrase (overrides $CHAT_IDENTITY_PASSPHRASE; no-echo prompt if neither set)")
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

	idPass := *identity
	if idPass == "" {
		idPass = os.Getenv("CHAT_IDENTITY_PASSPHRASE")
	}
	idPass, err = readIdentityPassphrase(env, idPass, "Identity passphrase")
	if err != nil {
		return err
	}
	// Identity passphrase is optional on login: pre-Phase-10 users still
	// log in with a username/password while their pubkey columns are
	// NULL. When the server returns a populated sign_pubkey we'll
	// require a matching local derivation; otherwise we skip the
	// derivation so legacy CI fixtures keep working until #983.

	client, cfg, err := newClient(env, false)
	if err != nil {
		return err
	}
	resp, err := client.Login(ctx, username, pw)
	if err != nil {
		return err
	}

	if idPass != "" {
		id, derr := deriveIdentityNoPersist(idPass, username)
		if derr != nil {
			return derr
		}
		if resp.User.SignPubkey != "" {
			localSign := base64.StdEncoding.EncodeToString(id.SignPub)
			if subtle.ConstantTimeCompare([]byte(localSign), []byte(resp.User.SignPubkey)) != 1 {
				return fmt.Errorf("wrong identity passphrase: derived sign_pubkey does not match server record")
			}
		}
		if err := config.WriteIdentitySeed(env.ConfigDir, id.RootSeed[:]); err != nil {
			return fmt.Errorf("write identity seed: %w", err)
		}
	}

	cfg.Server = client.BaseURL()
	cfg.Token = resp.Token
	cfg.User = &config.User{ID: string(resp.User.ID), Username: resp.User.Username}
	if err := saveConfig(env, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "Logged in as %s\n", resp.User.Username)
	return nil
}
