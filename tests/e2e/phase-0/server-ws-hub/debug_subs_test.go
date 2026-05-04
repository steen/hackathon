package server_ws_hub_e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-6: GET /debug/subs?channel=<name> returns the current subscriber
// count for the given channel as plain text (decimal integer + `\n`).
// Internal-only — the /debug/ prefix marks it as not part of the
// product API and not on the {ok,data,error} envelope contract.
//
// Asserts:
//   - With zero dials open, count is 0.
//   - After one dial, count rises to 1.
//   - After dial closes, count returns to 0.
//   - Content-Type is text/plain (not application/json).
//   - Body matches `^\d+\n$` — NOT a JSON envelope.
func TestAC6_ServerWsHub_DebugSubsCount(t *testing.T) {
	srv := startServer(t)

	url := fmt.Sprintf("%s/debug/subs?channel=%%23general", srv.httpURL)

	// 1) zero dials → count == 0.
	if got, err := fetchSubscriberCount(url); err != nil || got != 0 {
		t.Fatalf("initial /debug/subs: got=%d err=%v want 0", got, err)
	}

	// 2) one dial → count rises to 1 (poll within deadline).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitForSubscriberCount(t, srv, "#general", 1, 2*time.Second)

	// 3) Body shape + Content-Type: must be text/plain and match
	// `^\d+\n$` — explicitly NOT the {ok,data,error} envelope.
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper, loopback URL
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix (envelope contract requires plain text for /debug/)", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !regexp.MustCompile(`^\d+\n$`).Match(raw) {
		t.Fatalf("body = %q, want match ^\\d+\\n$ (decimal integer + newline)", raw)
	}
	// Body must not be a JSON envelope.
	trim := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trim, "{") {
		t.Fatalf("body = %q starts with '{'; /debug/subs must not be on the {ok,data,error} envelope", raw)
	}

	// 4) Close → count returns to 0.
	_ = c.CloseNow()
	waitForSubscriberCount(t, srv, "#general", 0, 2*time.Second)
}
