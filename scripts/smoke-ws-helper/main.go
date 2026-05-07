// Command smoke-ws-helper drives a WebSocket round-trip for the
// docker-image smoke test (issue #795).
//
// It dials the server's /ws endpoint with a redeemed ticket, subscribes
// to a channel, and waits for a frame whose JSON envelope matches
// type=message and data.body=<expected>. Exits 0 on match within the
// timeout, non-zero on any failure (dial error, frame timeout,
// unexpected close).
//
// Posting the message is the smoke script's job (a curl call to
// POST /api/channels/{id}/messages); the helper only listens. Keeping
// post-side logic in shell mirrors scripts/smoke.sh's pattern and
// avoids re-implementing JWT/JSON header handling here.
//
// Build target:
//
//	go build -o "$WORK_DIR/ws-helper" ./scripts/smoke-ws-helper
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/coder/websocket"
)

func main() {
	wsURL := flag.String("url", "", "WebSocket URL with ticket and channel query params")
	expect := flag.String("expect", "", "exact message body the helper must observe")
	timeout := flag.Duration("timeout", 15*time.Second, "max wait for the matching frame")
	flag.Parse()

	if *wsURL == "" || *expect == "" {
		fmt.Fprintln(os.Stderr, "smoke-ws-helper: -url and -expect are required")
		os.Exit(2)
	}

	if err := run(*wsURL, *expect, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "smoke-ws-helper: %v\n", err)
		os.Exit(1)
	}
}

func run(url, expect string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.CloseNow() }()

	// Match wsapi.ReadLimitBytes (apps/server/internal/wsapi/handler.go).
	conn.SetReadLimit(64 * 1024)

	for {
		_, raw, readErr := conn.Read(ctx)
		if readErr != nil {
			if errors.Is(readErr, context.DeadlineExceeded) {
				return fmt.Errorf("did not observe message %q within %s", expect, timeout)
			}
			return fmt.Errorf("read: %w", readErr)
		}
		var env struct {
			Type string `json:"type"`
			Data struct {
				Body string `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Type == "message" && env.Data.Body == expect {
			return nil
		}
	}
}
