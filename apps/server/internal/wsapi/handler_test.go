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
// frame to other subscribers. An earlier raw-rebroadcast contract was
// removed because it bypassed persistence and let any peer impersonate
// any sender. The inbound frame is still read (so the conn drains and
// the size+rate limits still trip), but the bytes are dropped.
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

	if err := waitForSubscribers(h, testDefaultChannel, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	forged := []byte(`{"type":"message","data":{"id":"01HFAKEFAKEFAKEFAKEFAKEFAK",` +
		`"channel_id":"` + testDefaultChannel + `","sender_user_id":"01HVICTIMVICTIMVICTIMVICTIM",` +
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
	// 50ms is the floor: it lets the server's frame-read loop consume
	// the forged frame before the sentinel arrives, so a regression
	// that rebroadcasts raw client frames cannot hide behind ordering
	// luck. Shorter waits race the sentinel and risk false greens.
	time.Sleep(50 * time.Millisecond)
	h.Broadcast(testDefaultChannel, []byte(sentinel))

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

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	_ = c.Close(websocket.StatusNormalClosure, "")

	if err := waitForSubscribers(h, testDefaultChannel, 0, 2*time.Second); err != nil {
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

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
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
		// Poll: hub subscriber registration races the WS handshake, and
		// there is no synchronous signal exposed for "subscribed" — keep
		// the wait short so flaky CI still reaches the deadline branch.
		time.Sleep(10 * time.Millisecond)
	}
}

// gap-D: WS upgrade for an unknown channel is rejected with HTTP 404
// BEFORE the WebSocket handshake.
func TestHandlerRejectsUnknownChannel(t *testing.T) {
	h := hub.New()
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	// Use a structurally-valid (26-char base32) channel id so the
	// upgrade reaches ChannelLookup; a malformed id is rejected by the
	// shared normalizer earlier with the same 404, but this test
	// targets the lookup-driven branch.
	resp, err := http.Get(srv.URL + "/ws?channel=01HMISSINGMISSINGMISSINGMI")
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

// With cfg.ChannelLookup wired (production path), an upgrade missing
// ?channel= must reject with HTTP 400 BEFORE the WebSocket handshake.
func TestHandlerRejectsMissingChannelWhenLookupWired(t *testing.T) {
	h := hub.New()
	calls := 0
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			calls++
			return true, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	if calls != 0 {
		t.Fatalf("ChannelLookup invoked %d time(s) before channel-required check; want 0", calls)
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

	resp, err := http.Get(srv.URL + "/ws?channel=01HANYTHINGANYTHINGANYTHIN")
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

// Audit #78 (low): a 404 on an unknown ?channel= must NOT redeem the
// ws-ticket. We assert this by issuing one ticket, sending it to /ws
// with an unknown channel, observing the 404, and then redeeming the
// same ticket directly against the TicketStore — which must still
// succeed because the handler did not consume it. Also covers SEC-12
// indistinguishability: the rejection body for unknown channel is the
// same shape as for missing/invalid ticket only at the upgrade level;
// here we only assert non-consumption, which is the new invariant.
func TestHandlerUnknownChannelDoesNotConsumeTicket(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}
	srv := httptest.NewServer(Handler(h, ts, cfg))
	defer srv.Close()

	const owner = "user-probe"
	tok, _ := ts.Issue(owner)

	resp, err := http.Get(srv.URL + "/ws?channel=missing-channel-id&ticket=" + tok)
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

	uid, ok := ts.Redeem(tok)
	if !ok {
		t.Fatal("ticket was consumed by the unknown-channel rejection; want still-redeemable")
	}
	if uid != owner {
		t.Fatalf("redeemed user_id: got %q want %q", uid, owner)
	}
}

// Audit #78 (low): with the channel-check-first ordering, an unknown
// channel + missing ticket must return 404 (channel arm), NOT 401
// (ticket arm). Locks in the ordering as a contract — a future swap
// back to ticket-first would silently change the response and pass
// the rest of the suite.
func TestHandlerUnknownChannelWithoutTicketReturns404(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}
	srv := httptest.NewServer(Handler(h, ts, cfg))
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
		t.Fatalf("status: got %d want 404 (channel arm runs before ticket arm)", resp.StatusCode)
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

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	subs := h.SnapshotSubscribers(testDefaultChannel)
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
	// Decision log §10 / L15: an authenticated connection binds two
	// topics — the channel topic first, `user:<viewer>` second.
	wantTopics := []string{testDefaultChannel, "user:" + owner}
	gotTopics := cs.channelsForTesting()
	if len(gotTopics) != len(wantTopics) {
		t.Fatalf("channels: got %v want %v", gotTopics, wantTopics)
	}
	for i := range wantTopics {
		if gotTopics[i] != wantTopics[i] {
			t.Fatalf("channels[%d]: got %q want %q (full got=%v want=%v)",
				i, gotTopics[i], wantTopics[i], gotTopics, wantTopics)
		}
	}
}

// Decision log §10 / L15: every authenticated WS connection auto-
// subscribes to the inbox topic `user:<viewer>` alongside its channel
// topic. Asserts the hub registers the connection on BOTH topics and
// that a Broadcast on `user:<viewer>` reaches the connection.
func TestHandlerSubscribesToUserInboxTopic(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	const owner = "user-multi-topic"
	tok, _ := ts.Issue(owner)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + tok

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForSubscribers(h, "user:"+owner, 1, 2*time.Second); err != nil {
		t.Fatalf("user-inbox topic subscriber: %v", err)
	}

	// Drain the self-emitted presence-join frame the handler broadcasts
	// for the first connection of an authenticated user — it arrives via
	// BroadcastAll on the channel topic, not on the user-inbox topic, but
	// it sits at the head of the read queue and would otherwise be the
	// first frame this test reads.
	drainCtx, drainCancel := context.WithTimeout(ctx, 2*time.Second)
	defer drainCancel()
	if _, _, err := c.Read(drainCtx); err != nil {
		t.Fatalf("drain presence-join frame: %v", err)
	}

	// A frame published to the inbox topic must reach the connection.
	const payload = "user-inbox-payload"
	h.Broadcast("user:"+owner, []byte(payload))
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, data, err := c.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != payload {
		t.Fatalf("frame: got %q want %q", data, payload)
	}
}

// Decision log §10: a ticket-less connection has no viewer id, so the
// connection must NOT register a `user:` topic with an empty id (which
// would let a future broadcast leak to every unauthenticated client).
// The single-topic legacy shape is preserved for the no-TicketStore path.
func TestHandlerNoUserTopicWhenUnauthenticated(t *testing.T) {
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
	defer c.CloseNow()

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := h.SubscriberCount("user:"); got != 0 {
		t.Fatalf("topic 'user:' must have no subscribers when unauthenticated; got %d", got)
	}

	subs := h.SnapshotSubscribers(testDefaultChannel)
	if len(subs) != 1 {
		t.Fatalf("subs: got %d want 1", len(subs))
	}
	cs, ok := subs[0].(*connSubscriber)
	if !ok {
		t.Fatalf("subscriber type: got %T want *connSubscriber", subs[0])
	}
	if got := cs.channelsForTesting(); len(got) != 1 || got[0] != testDefaultChannel {
		t.Fatalf("channels: got %v want [%q]", got, testDefaultChannel)
	}
}

// AC: close path unsubscribes from ALL bound topics (multi-topic
// teardown). Asserts both the channel topic and `user:<viewer>` drop
// to zero subscribers after the connection closes.
func TestHandlerUnsubscribesAllTopicsOnDisconnect(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	const owner = "user-teardown"
	tok, _ := ts.Issue(owner)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + tok

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := waitForSubscribers(h, testDefaultChannel, 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForSubscribers(h, "user:"+owner, 1, 2*time.Second); err != nil {
		t.Fatalf("user-inbox topic subscriber: %v", err)
	}

	_ = c.Close(websocket.StatusNormalClosure, "")

	if err := waitForSubscribers(h, testDefaultChannel, 0, 2*time.Second); err != nil {
		t.Fatalf("channel topic teardown: %v", err)
	}
	if err := waitForSubscribers(h, "user:"+owner, 0, 2*time.Second); err != nil {
		t.Fatalf("user-inbox topic teardown: %v", err)
	}
}

// Audit #78 (info): the WS handler must upper-fold a non-sentinel
// ?channel= value through the same normalizer the REST surface uses.
// A lower-case ULID resolves the same channel as the canonical
// upper-case form, and is passed to ChannelLookup and the hub in
// upper-case so subscribers on /api/channels/{id}/messages and /ws
// land on the same channel string.
func TestHandlerLowercaseChannelIDFoldsToUpper(t *testing.T) {
	h := hub.New()
	const canonical = "01HABCDEFGHJKMNPQRSTVWXYZA"
	lower := strings.ToLower(canonical)

	var seen []string
	cfg := Config{
		ChannelLookup: func(_ context.Context, id string) (bool, error) {
			seen = append(seen, id)
			return id == canonical, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?channel=" + lower
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if err := waitForSubscribers(h, canonical, 1, 2*time.Second); err != nil {
		t.Fatalf("subscriber on canonical channel: %v", err)
	}
	if got := h.SubscriberCount(lower); got != 0 {
		t.Fatalf("lower-case channel must have no subscribers, got %d", got)
	}
	if len(seen) != 1 || seen[0] != canonical {
		t.Fatalf("ChannelLookup ids: got %v want [%q]", seen, canonical)
	}
}

// Audit #78 (info): a malformed channel id (not 26 chars / outside
// the 0-9A-Z alphabet) is rejected with HTTP 404 BEFORE ChannelLookup
// is invoked.
func TestHandlerMalformedChannelIDRejected(t *testing.T) {
	h := hub.New()
	called := false
	cfg := Config{
		ChannelLookup: func(_ context.Context, _ string) (bool, error) {
			called = true
			return true, nil
		},
	}
	srv := httptest.NewServer(Handler(h, nil, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws?channel=not-a-ulid!")
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
	if called {
		t.Fatal("ChannelLookup invoked for malformed id; should be rejected by normalizer first")
	}
}
