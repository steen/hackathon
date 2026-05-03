// Command chatd is a minimal phase-0 CLI for the chat server.
//
// Usage:
//
//	chatd [--url ws://host:port/ws] [--ws-ticket TICKET] send <message...>
//	chatd [--url ws://host:port/ws] [--ws-ticket TICKET] watch
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "chatd:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	url := ""
	ticket := ""
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--url":
			if len(args) < 2 {
				return fmt.Errorf("--url requires a value")
			}
			url = args[1]
			args = args[2:]
		case "--ws-ticket":
			if len(args) < 2 {
				return fmt.Errorf("--ws-ticket requires a value")
			}
			ticket = args[1]
			args = args[2:]
		default:
			return fmt.Errorf("unknown flag %q", args[0])
		}
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: chatd [--url URL] [--ws-ticket TICKET] {send <msg...>|watch}")
	}

	resolved := cmd.AppendTicket(cmd.ResolveURL(url), ticket)
	sub, rest := args[0], args[1:]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch sub {
	case "send":
		if len(rest) == 0 {
			return fmt.Errorf("send: missing message")
		}
		return cmd.Send(ctx, resolved, rest)
	case "watch":
		return cmd.Watch(ctx, resolved, os.Stdout)
	default:
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}
