// Command chatd is a minimal phase-0 CLI for the chat server.
//
// Usage:
//
//	chatd [--url ws://host:port/ws] send <message...>
//	chatd [--url ws://host:port/ws] watch
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	for len(args) > 0 && args[0] == "--url" {
		if len(args) < 2 {
			return fmt.Errorf("--url requires a value")
		}
		url = args[1]
		args = args[2:]
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: chatd [--url URL] {send <msg...>|watch}")
	}

	resolved := cmd.ResolveURL(url)
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
