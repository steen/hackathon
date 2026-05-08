// Command wsobserver runs as a child process under the channel-ws-drift
// e2e test. It opens a packages/go-client WebSocket, decodes inbound
// frames through goclient.Watch (so a JSON-tag rename or struct-shape
// change on the Go-client side surfaces as a missing field, not a silent
// pass), and emits each observed `channel` event as one JSON line on
// stdout for the orchestrating TS test to compare against its own
// api-client decode of the same wire frame.
//
// The observer connects to the default hub topic (#general). The server
// fans `channel` create/rename frames out via Hub.BroadcastAll — every
// connected client receives them regardless of subscribed channel — so
// no -channel flag is needed.
//
// Output schema (one JSON object per stdout line):
//
//	{"event":"ready"}
//	{"event":"channel","kind":"create"|"rename","id":"<ulid>","name":"<name>","created_at":"<rfc3339>"}
//	{"event":"error","message":"<text>"}
//
// "ready" is emitted as soon as Watch returns. Watch returns after the
// HTTP→WS upgrade completes, so the parent can safely issue the REST
// POST/PATCH on receipt without racing the subscription. Stdout is
// line-buffered; main flushes after each emit via fmt.Println +
// os.Stdout.Sync to keep the parent's read loop responsive.
//
// Exit codes: 0 on clean ctx-cancel teardown, 1 on any error reported
// before exit. Stderr is reserved for human-readable diagnostics that
// the parent surfaces on test failure.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	goclient "hackathon/packages/go-client"
)

type stdoutLine struct {
	Event     string `json:"event"`
	Kind      string `json:"kind,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Message   string `json:"message,omitempty"`
}

func main() {
	os.Exit(realMain())
}

// realMain wraps the entire program so deferred cancels run before
// os.Exit. gocritic's `exitAfterDefer` rule fires when os.Exit lives in
// the same scope as a defer; isolating the exit at the outer main ()
// keeps the wiring straightforward and lints clean.
func realMain() int {
	baseURL := flag.String("base-url", "", "REST/WS base URL (http://...)")
	username := flag.String("username", "", "username to register/login")
	password := flag.String("password", "", "password to register with")
	invite := flag.String("invite", "", "registration invite code")
	timeout := flag.Duration("timeout", 60*time.Second, "max observer lifetime")
	flag.Parse()

	if *baseURL == "" || *username == "" || *password == "" || *invite == "" {
		fail("required flags: -base-url, -username, -password, -invite")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := run(ctx, *baseURL, *username, *password, *invite); err != nil {
		fail(err.Error())
		return 1
	}
	return 0
}

func run(ctx context.Context, baseURL, username, password, invite string) error {
	client := goclient.New(baseURL)
	if _, err := client.Register(ctx, username, password, invite); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// The WS handler now requires ?channel=<id>; subscribe to the
	// seeded "general" channel. Hub.BroadcastAll fans channel:create /
	// channel:rename events to every subscriber regardless of which
	// channel they're on, so the observer still sees them.
	channels, err := client.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	var channelID string
	for _, ch := range channels {
		if ch.Name == "general" {
			channelID = string(ch.ID)
			break
		}
	}
	if channelID == "" {
		return fmt.Errorf("seeded 'general' channel not found")
	}

	events, err := client.Watch(ctx, goclient.WatchOptions{ChannelID: channelID})
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}

	emit(stdoutLine{Event: "ready"})

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if ev.Type != goclient.EventTypeChannel || ev.Channel == nil {
				continue
			}
			emit(stdoutLine{
				Event:     "channel",
				Kind:      ev.Channel.Kind,
				ID:        string(ev.Channel.Channel.ID),
				Name:      ev.Channel.Channel.Name,
				CreatedAt: ev.Channel.Channel.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}
	}
}

func emit(line stdoutLine) {
	buf, err := json.Marshal(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wsobserver: marshal: %v\n", err)
		return
	}
	fmt.Println(string(buf))
}

func fail(msg string) {
	emit(stdoutLine{Event: "error", Message: msg})
	fmt.Fprintf(os.Stderr, "wsobserver: %s\n", msg)
}
