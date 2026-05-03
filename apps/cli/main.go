// Command chatd is the phase-0 chat CLI: `chatd send <message...>` and
// `chatd watch` connect to the server WebSocket endpoint at /ws.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/jumoel/hackathon/apps/cli/cmd"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: chatd <send|watch> [--url URL] [args...]")
		return 2
	}

	sub, rest := args[0], args[1:]
	env := os.Getenv(cmd.EnvServerURL)

	switch sub {
	case "send":
		return runSend(rest, env, stderr)
	case "watch":
		return runWatch(rest, env, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q (want send or watch)\n", sub)
		return 2
	}
}

func runSend(args []string, env string, stderr *os.File) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", "", "WebSocket server URL (overrides "+cmd.EnvServerURL+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	msg := fs.Args()
	if len(msg) == 0 {
		fmt.Fprintln(stderr, "send: missing message")
		return 2
	}
	url := cmd.ResolveURL(*urlFlag, env)
	if err := cmd.Send(context.Background(), url, msg); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runWatch(args []string, env string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", "", "WebSocket server URL (overrides "+cmd.EnvServerURL+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	url := cmd.ResolveURL(*urlFlag, env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cmd.Watch(ctx, url, stdout); err != nil {
		if ctx.Err() != nil {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
