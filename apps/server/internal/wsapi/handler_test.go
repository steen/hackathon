package wsapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
)

// Audit #78 (medium): an authenticated peer must NOT be able to
// rebroadcast a forged {type:"message",data:{sender_user_id:"<other>"}}
// frame to other subscribers. The phase-0 raw rebroadcast was removed
// because it bypassed persistence and let any peer impersonate any
// sender. The inbound frame is still read (so the conn drains and the
// size+rate limits still trip), but the bytes are dropped.
func TestHandlerDoesNotRebroadcastInboundFrames(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h, nil, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sender, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("sender dial: %v", err)
	}
	defer sender.CloseNow()

	receiver, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("receiver dial: %v", err)
	}
	defer receiver.CloseNow()

	if err := waitForSubscribers(h, "#general", 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	forged := []byte(`{"type":"message","data":{"id":"01HFAKEFAKEFAKEFAKEFAKEFAK",` +
		`"channel_id":"#general","sender_user_id":"01HVICTIMVICTIMVICTIMVICTIM",` +
		`"body":"impersonated text","created_at":"2026-05-03T00:00:00Z"}}`)
	if err := sender.Write(ctx, websocket.MessageText, forged); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Independently confirm the broadcast path still works for the
	// supported producer (hub.Broadcast invoked server-side, the way
	// the REST POST handler does it). Use a sentinel marker so it can
	// not be confused with the forged frame.
	const sentinel = "server-emitted-sentinel"
	// Give the server a brief moment to read the forged frame so any
	// (mistaken) rebroadcast would land before our sentinel.
	time.Sleep(50 * time.Millisecond)
	h.Broadcast("#general", []byte(sentinel))

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, data, err := receiver.Read(readCtx)
	if err != nil {
		t.Fatalf("receiver read: %v", err)
	}
	if string(data) != sentinel {
		t.Fatalf("first frame received was %q; want %q (raw rebroadcast leaked)",
			data, sentinel)
	}
}

func TestHandlerUnsubscribesOnDisconnect(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h, nil, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := waitForSubscribers(h, "#general", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	_ = c.Close(websocket.StatusNormalClosure, "")

	if err := waitForSubscribers(h, "#general", 0, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// SEC-12: a ticket may only be redeemed once. The first /ws upgrade
// using the ticket succeeds; a second upgrade with the same ticket
// must be rejected before the WebSocket handshake completes.
func TestHandlerTicketSingleUse(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	tok, _ := ts.Issue("user-1")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + tok

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer c.CloseNow()

	if err := waitForSubscribers(h, "#general", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("second dial: want error, got nil")
	}
	if resp == nil {
		t.Fatalf("second dial: want HTTP response, got nil err=%v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second dial status: got %d want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// Missing ticket → 401 before upgrade. Same envelope as a bad ticket
// so probing cannot distinguish "no ticket" from "wrong ticket".
func TestHandlerMissingTicketRejected(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("dial: want error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestHandlerInvalidTicketRejected(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=deadbeef"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("dial: want error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

// Cross-origin upgrade from https://evil.example must be rejected by
// coder/websocket's same-origin check (HTTP 403). We bypass the
// websocket.Dial helper here because it does not let us forge an
// arbitrary Origin against an httptest server's host.
func TestHandlerRejectsCrossOriginUpgrade(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h, nil, Config{}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/ws", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// Same-origin upgrade (Origin matches Host) succeeds — this guards
// against accidentally over-restricting the same-origin policy.
func TestHandlerAcceptsSameOriginUpgrade(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h, nil, Config{}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	wsURL := "ws://" + host + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://" + host}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func waitForSubscribers(h *hub.Hub, channel string, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		got := h.SubscriberCount(channel)
		if got == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waiting for %d subscribers on %s: got %d", want, channel, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gap-D: WS upgrade for an unknown channel is rejected with HTTP 404
// BEFORE the WebSocket handshake. The legacy #general sentinel keeps
// working without a DB lookup; any other channel id is checked via
// cfg.ChannelLookup.
func TestHandlerRejectsUnknownChannel(t *testing.T) {
	h := hub.New()
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws?channel=missing-channel-id")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "channel not found") {
		t.Fatalf("body: got %q want substring 'channel not found'", body)
	}
}

// gap-D: legacy #general bypasses the lookup so phase-0 boot paths and
// pre-DB tests keep working.
func TestHandlerAcceptsLegacyDefaultChannelWithoutLookup(t *testing.T) {
	h := hub.New()
	calls := 0
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			calls++
			return false, nil // would normally reject — but #general skips the check
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	if calls != 0 {
		t.Fatalf("ChannelLookup invoked %d time(s) for default channel; want 0", calls)
	}
}

// gap-D: a known channel passes the lookup and the upgrade succeeds.
func TestHandlerAcceptsKnownChannel(t *testing.T) {
	h := hub.New()
	const known = "01HABCDEFGHJKMNPQRSTVWXYZA"
	cfg := Config{
		ChannelLookup: func(_ context.Context, id string) (bool, error) {
			return id == known, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?channel=" + known
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if err := waitForSubscribers(h, known, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// gap-D: a ChannelLookup error becomes HTTP 500 (no upgrade).
func TestHandlerChannelLookupErrorReturns500(t *testing.T) {
	h := hub.New()
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			return false, errors.New("synthetic db failure")
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws?channel=anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", resp.StatusCode)
	}
}

// gap-D F5: after a successful ticket redemption, the per-conn state
// carries the redeemed userID. Observed via the test-only accessor on
// connSubscriber and hub.SnapshotSubscribers.
func TestHandlerBindsUserIDFromTicket(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	const owner = "user-7"
	tok, _ := ts.Issue(owner)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + tok

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if err := waitForSubscribers(h, defaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	subs := h.SnapshotSubscribers(defaultChannel)
	if len(subs) != 1 {
		t.Fatalf("subs: got %d want 1", len(subs))
	}
	cs, ok := subs[0].(*connSubscriber)
	if !ok {
		t.Fatalf("subscriber type: got %T want *connSubscriber", subs[0])
	}
	if got := cs.userIDForTesting(); got != owner {
		t.Fatalf("userID: got %q want %q", got, owner)
	}
	if got := cs.channelForTesting(); got != defaultChannel {
		t.Fatalf("channel: got %q want %q", got, defaultChannel)
	}
}
