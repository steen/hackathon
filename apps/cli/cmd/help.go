package cmd

import (
	"context"
	"io"
)

// Help implements `chatd help`. It prints a usage block listing every
// subcommand with a one-line description and the env vars consulted at
// runtime. `chatd --help` / `chatd -h` / no-args bypass Dispatch and
// call WriteHelp directly from the binary entrypoint, so all four
// forms share the same byte stream.
func Help(_ context.Context, env *Env, _ []string) error {
	return WriteHelp(env.Stdout)
}

// WriteHelp emits the top-level usage block. Exported so the binary
// entrypoint can call it for `--help` / `-h` / no-args without
// constructing a full Env.
func WriteHelp(w io.Writer) error {
	_, err := io.WriteString(w, helpText)
	return err
}

const helpText = `chatd — command-line client for the chat server.

Usage:
  chatd [--server URL] <command> [args]
  chatd --help | -h | help
  chatd --version | -v | version

Commands:
  register <username>            Create an account and store a token.
                                 Reads $CHAT_PASSWORD, $CHAT_INVITE_CODE
                                 when --password / --invite-code are unset.
  login [<username>]             Log in and store a token.
                                 Reads $CHAT_PASSWORD when --password is unset.
  whoami                         Print the username of the stored token.
  logout                         Revoke the stored token server-side and
                                 clear local config.
  channels                       List every channel, tab-separated as
                                 <id> <name>.
  history <channel> [--limit N] [--before ID]
                                 Print messages newest-first, tab-separated
                                 as <rfc3339> <sender> <body>.
  send <channel> <message|->     Post a message; "-" reads body from stdin.
  watch <channel> [--once]       Stream new messages until ctx cancels;
                                 reconnects with backoff unless --once.
  help                           Show this message.
  version                        Print the chatd version, VCS revision,
                                 Go version, and OS/arch, then exit 0.

Global flags:
  --server URL    Base URL of the chat server. Overrides $CHAT_SERVER.
                  Defaults to http://localhost:8080.

Environment:
  CHAT_SERVER          Base URL fallback when --server is unset.
  CHAT_PASSWORD        Password fallback for register / login.
  CHAT_INVITE_CODE     Invite-code fallback for register.

Per-command flags: pass -h or --help to any subcommand
(e.g. ` + "`chatd send -h`" + `) to see its flag set.

Config: tokens are persisted under $XDG_CONFIG_HOME/chatd/config.json
(falls back to ~/.config/chatd/config.json).
`
