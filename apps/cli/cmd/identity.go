// Phase-10 CLI identity helpers (decision-log §4 + L11).
//
// readIdentityPassphrase prompts on stderr with terminal echo OFF (so a
// shoulder-surfer cannot read the passphrase from the screen) and reads
// a single line from the controlling tty. The login-password prompt at
// readSecret() above keeps echo on by deliberate choice — the identity
// passphrase is treated more sensitively because (a) it cannot be reset
// server-side and (b) it protects all past channel + DM history
// permanently. When stdin is not a tty (scripted heredoc, CI fixture)
// we fall back to readLine() so existing automation keeps working.

package cmd

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"hackathon/apps/cli/internal/config"
	goclient "hackathon/packages/go-client"
)

// readIdentityPassphrase returns value when non-empty (e.g. supplied
// via $CHAT_IDENTITY_PASSPHRASE for automation); otherwise prompts on
// stderr with no echo. Returns "" with no error when stdin has no data
// — caller decides whether to skip identity derivation or fail.
func readIdentityPassphrase(env *Env, value, prompt string) (string, error) {
	if value != "" {
		return value, nil
	}
	if env.Stdin == nil {
		return "", fmt.Errorf("no stdin to read %s from", prompt)
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s: ", prompt)
	if f, ok := env.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		raw, err := term.ReadPassword(int(f.Fd()))
		// term.ReadPassword does not echo the trailing newline; print
		// our own so the next prompt (or the success line) starts on a
		// fresh row.
		_, _ = fmt.Fprintln(env.Stderr)
		if err != nil {
			return "", fmt.Errorf("read identity passphrase: %w", err)
		}
		return string(raw), nil
	}
	// Scripted / non-tty: fall through to the existing line reader so
	// `echo "passphrase" | chatd register ...` continues to work in CI.
	return readLine(env)
}

// deriveAndPersistIdentity runs the full Phase-10 derivation pipeline
// for the supplied (passphrase, username) pair and writes the resulting
// 32-byte root seed to ~/.config/chatd/identity.seed (mode 0600). It
// returns the *Identity so the caller can serialise box_pubkey /
// sign_pubkey on the wire and (on login) verify against the server-
// returned sign_pubkey. Decision-log §4 + L4 + L11.
func deriveAndPersistIdentity(env *Env, passphrase, username string) (*goclient.Identity, error) {
	if passphrase == "" {
		return nil, nil
	}
	id, err := deriveIdentityNoPersist(passphrase, username)
	if err != nil {
		return nil, err
	}
	if err := config.WriteIdentitySeed(env.ConfigDir, id.RootSeed[:]); err != nil {
		return nil, fmt.Errorf("write identity seed: %w", err)
	}
	return id, nil
}

// deriveIdentityNoPersist returns the derived *goclient.Identity without
// touching the on-disk seed file. login.go uses this to verify the
// passphrase against the server-returned sign_pubkey BEFORE the seed
// hits disk; only on a successful match does the caller persist via
// config.WriteIdentitySeed. This keeps a wrong-passphrase attempt from
// stomping a previously-good seed on the same machine.
func deriveIdentityNoPersist(passphrase, username string) (*goclient.Identity, error) {
	id, err := goclient.DeriveIdentity(passphrase, username)
	if err != nil {
		if errors.Is(err, goclient.ErrIdentityPassphraseTooShort) {
			return nil, fmt.Errorf("%w (got %d)", err, len(passphrase))
		}
		return nil, err
	}
	return id, nil
}
