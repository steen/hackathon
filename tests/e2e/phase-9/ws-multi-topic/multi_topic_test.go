// Package multitopic_e2e_test exercises the Phase 9 WS multi-topic
// subscription contract end-to-end (decision log §10 / L15): an
// authenticated /ws upgrade subscribes to BOTH its channel topic AND
// its `user:<viewer>` inbox topic.
//
// Black-box harness: builds and boots the production chat-server
// binary on a loopback port (via testsupport per L27), upgrades a real
// WebSocket connection with a redeemed ws-ticket, and inspects hub
// state via the `/debug/subs?channel=<topic>` endpoint. /debug/subs
// answers SubscriberCount for any opaque topic string the hub is
// keyed by — including `user:<viewer>` — which is enough to prove the
// auto-subscribe lands at upgrade time without poking through the
// internal-package boundary.
//
// Frame-delivery on `user:<viewer>` is asserted by the in-package
// unit test TestHandlerSubscribesToUserInboxTopic in
// apps/server/internal/wsapi/handler_test.go (same handler, same hub
// — the internal-package rule blocks importing hub from tests/e2e/, so
// black-box assertion of frame receipt has to wait for the DM/read
// emitters to wire a publish path through the binary's HTTP surface).
package multitopic_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/tests/e2e/internal/testsupport"
)

// TestWSMultiTopicAutoSubscribesUserInbox is the AC-aligned e2e for
// issue #863: an authenticated /ws upgrade must register the
// connection on the `user:<viewer>` inbox topic alongside the channel
// topic. The assertion is a /debug/subs poll for both topics.
func TestWSMultiTopicAutoSubscribesUserInbox(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	username := "phase9-multi-" + testsupport.RandomSecret(t, 4)
	password := testsupport.RandomSecret(t, 12)
	viewerID, token := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, username, password)

	channelID := lookupSeededGeneralChannelID(t, srv, token)

	ticket := testsupport.MintTicket(t, srv.HTTPURL, token)
	wsURL := fmt.Sprintf("%s?ticket=%s&channel=%s", srv.WSURL, ticket, channelID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial /ws status=%v want 101", resp)
	}

	// AC-1: subscribed to the channel topic.
	if !waitForSubCount(srv.HTTPURL, channelID, 1, 2*time.Second) {
		t.Fatalf("channel topic %s never reached 1 subscriber within 2s", channelID)
	}

	// AC-2: subscribed to `user:<viewer>` inbox topic. The subscriber
	// count is exactly 1 — this is the only WS connection in the test
	// and the inbox topic is per-viewer, so a count of >1 would mean
	// the topic is leaking subscribers across users (spec violation).
	inbox := "user:" + viewerID
	if !waitForSubCount(srv.HTTPURL, inbox, 1, 2*time.Second) {
		got := fetchSubCount(t, srv.HTTPURL, inbox)
		t.Fatalf("inbox topic %s subscriber count: got %d want 1 within 2s", inbox, got)
	}

	// AC-3: close path tears down both topics.
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !waitForSubCount(srv.HTTPURL, channelID, 0, 2*time.Second) {
		t.Fatalf("channel topic %s did not drop to 0 subscribers within 2s after close", channelID)
	}
	if !waitForSubCount(srv.HTTPURL, inbox, 0, 2*time.Second) {
		t.Fatalf("inbox topic %s did not drop to 0 subscribers within 2s after close", inbox)
	}
}

// fetchSubCount issues GET /debug/subs?channel=<topic> against the
// server and parses the "<n>\n" body. /debug/subs is loopback-only
// and returns the hub's SubscriberCount for any topic — channel ids
// or `user:<viewer>` strings alike.
func fetchSubCount(t *testing.T, httpURL, topic string) int {
	t.Helper()
	u := fmt.Sprintf("%s/debug/subs?channel=%s", httpURL, url.QueryEscape(topic))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("new GET /debug/subs: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /debug/subs: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /debug/subs %s: status %d", topic, resp.StatusCode)
	}
	var count int
	if _, err := fmt.Fscanf(resp.Body, "%d", &count); err != nil {
		t.Fatalf("scan /debug/subs body for %s: %v", topic, err)
	}
	return count
}

// waitForSubCount polls /debug/subs every 25ms until the topic's
// subscriber count equals want or timeout elapses. Returns true on
// success, false on timeout.
func waitForSubCount(httpURL, topic string, want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	u := fmt.Sprintf("%s/debug/subs?channel=%s", httpURL, url.QueryEscape(topic))
	client := &http.Client{Timeout: 1 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err == nil {
			var count int
			_, scanErr := fmt.Fscanf(resp.Body, "%d", &count)
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if scanErr == nil && count == want {
				return true
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

// lookupSeededGeneralChannelID lists channels with the given bearer
// token and returns the seeded "general" channel id. Phase 9 WS
// upgrades still require a real channel id under the production
// `cfg.ChannelLookup` wiring (the L15 default-channel fallback is the
// W sub-issue's responsibility).
func lookupSeededGeneralChannelID(t *testing.T, srv *testsupport.Server, bearer string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.HTTPURL+"/api/channels", nil)
	if err != nil {
		t.Fatalf("new GET /api/channels: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/channels: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/channels: status %d body %s", resp.StatusCode, raw)
	}
	var env struct {
		OK   bool             `json:"ok"`
		Data *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&env); err != nil {
		t.Fatalf("decode /api/channels: %v body %s", err, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/api/channels envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channels list: %v body %s", err, raw)
	}
	for _, ch := range data.Channels {
		if ch.Name == "general" {
			return ch.ID
		}
	}
	t.Fatalf("/api/channels: no seeded 'general' in %s", raw)
	return ""
}
