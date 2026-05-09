package cmd

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"hackathon/apps/cli/internal/config"
)

// Register implements `chatd register <username>`. It prompts for any
// credential not supplied via flag (--password, --invite-code) or env
// (CHAT_PASSWORD, CHAT_INVITE_CODE), POSTs /api/auth/register, and
// stores the returned token in the config file.
//
// Phase-10 identity (decision-log §4 + L11): the user is also prompted
// for an identity passphrase via no-echo. The derived root seed is
// persisted at ~/.config/chatd/identity.seed (mode 0600) and the
// derived box_pubkey + sign_pubkey ride the register body so the
// server's INSERT populates the new users.box_pubkey / sign_pubkey
// columns. Passing $CHAT_IDENTITY_PASSPHRASE skips the prompt.
func Register(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	password := fs.String("password", "", "password (overrides $CHAT_PASSWORD; prompted if neither set)")
	invite := fs.String("invite-code", "", "invite code (overrides $CHAT_INVITE_CODE; prompted if neither set)")
	identity := fs.String("identity-passphrase", "", "identity passphrase (overrides $CHAT_IDENTITY_PASSPHRASE; no-echo prompt if neither set)")
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

	idPass := *identity
	if idPass == "" {
		idPass = os.Getenv("CHAT_IDENTITY_PASSPHRASE")
	}
	idPass, err = readIdentityPassphrase(env, idPass, "Identity passphrase")
	if err != nil {
		return err
	}
	// Identity passphrase is optional in this PR: pre-Phase-10 callers
	// (existing CLI fixtures, scripted automation) keep working without
	// supplying one. #983 (wave-6 cutover) tightens this once every
	// consumer of legacy users is migrated.
	var boxB64, signB64 string
	if idPass != "" {
		id, derr := deriveAndPersistIdentity(env, idPass, username)
		if derr != nil {
			return derr
		}
		boxB64 = base64.StdEncoding.EncodeToString(id.BoxPub[:])
		signB64 = base64.StdEncoding.EncodeToString(id.SignPub)
	}

	client, cfg, err := newClient(env, false)
	if err != nil {
		return err
	}
	resp, err := client.RegisterWithIdentity(ctx, username, pw, code, boxB64, signB64)
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
