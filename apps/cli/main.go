// Command chatd is the Phase-2 CLI for the chat server. It targets a
// single base URL (--server / $CHAT_SERVER, default http://localhost:8080),
// reuses packages/go-client for every server interaction, and persists
// its bearer token under $XDG_CONFIG_HOME/chatd/config.json.
//
// Usage:
//
//	chatd [--server URL] register <username>
//	chatd [--server URL] login [<username>]
//	chatd [--server URL] whoami
//	chatd [--server URL] logout
//	chatd [--server URL] channels
//	chatd [--server URL] history <channel> [--limit N] [--before ID]
//	chatd [--server URL] send <channel> <message|->
//	chatd [--server URL] watch <channel>
//	chatd help | --help | -h
//	chatd version | --version | -v
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"hackathon/apps/cli/cmd"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "chatd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	server, rest, err := stripServerFlag(args)
	if err != nil {
		return err
	}
	// No subcommand -> print help and exit 0. A bare `help`, `--help`,
	// or `-h` as the first positional token does the same. Per-subcommand
	// `-h` is still owned by each flag.FlagSet (e.g. `chatd send -h`).
	if len(rest) == 0 || isTopLevelHelp(rest[0]) {
		return cmd.WriteHelp(os.Stdout)
	}
	// `--version` / `-v` mirror the help short-circuit: they print and
	// exit 0 without contacting any server, so we handle them here
	// rather than as a regular subcommand that would build an Env.
	if isTopLevelVersion(rest[0]) {
		return cmd.WriteVersion(os.Stdout)
	}

	env := cmd.DefaultEnv()
	env.Server = cmd.ResolveServer(server)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return Dispatch(ctx, env, rest)
}

func isTopLevelHelp(tok string) bool {
	return tok == "help" || tok == "--help" || tok == "-h"
}

func isTopLevelVersion(tok string) bool {
	return tok == "--version" || tok == "-v"
}

// stripServerFlag pulls --server / --server=URL out of args before
// reaching the subcommand. Each subcommand owns its own flag set, so a
// global is parsed by hand here.
func stripServerFlag(args []string) (string, []string, error) {
	const eqPrefix = "--server="
	server := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--server":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--server requires a value")
			}
			server = args[i+1] //nolint:gosec // bounds checked above
			i++
		case strings.HasPrefix(a, eqPrefix):
			server = a[len(eqPrefix):]
		default:
			out = append(out, a)
		}
	}
	return server, out, nil
}

// Dispatch routes the parsed subcommand. Exposed so the test suite can
// drive each command via the same path the binary entrypoint uses.
func Dispatch(ctx context.Context, env *cmd.Env, args []string) error {
	sub, rest := args[0], args[1:]
	switch sub {
	case "register":
		return cmd.Register(ctx, env, rest)
	case "login":
		return cmd.Login(ctx, env, rest)
	case "whoami":
		return cmd.Whoami(ctx, env, rest)
	case "logout":
		return cmd.Logout(ctx, env, rest)
	case "channels":
		return cmd.Channels(ctx, env, rest)
	case "history":
		return cmd.History(ctx, env, rest)
	case "send":
		return cmd.Send(ctx, env, rest)
	case "watch":
		return cmd.Watch(ctx, env, rest)
	case "version":
		return cmd.Version(ctx, env, rest)
	default:
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}
