package cmd

import (
	"io"
)

// WriteHelp emits the top-level usage block. `chatd help`,
// `chatd --help`, `chatd -h`, and `chatd` (no args) all route here so
// every form writes the same helpText. The dispatched `chatd help`
// case writes to env.Stdout; the other three short-circuit in
// apps/cli/main.go and pass os.Stdout directly. Per-subcommand `-h`
// is owned by each flag.FlagSet (e.g. `chatd send -h`).
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
  CHATD_CONFIG_DIR     Override the directory holding config.json.

Per-command flags: pass -h or --help to any subcommand
(e.g. ` + "`chatd send -h`" + `) to see its flag set.

Config: tokens are persisted under $CHATD_CONFIG_DIR/config.json, or
$XDG_CONFIG_HOME/chatd/config.json, or ~/.config/chatd/config.json
(default).
`
