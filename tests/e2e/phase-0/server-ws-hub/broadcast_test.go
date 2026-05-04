package server_ws_hub_e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-3: Every received message is broadcast to all subscribers of the
// message's channel.
//
// Audit #78 reframed this AC: inbound WS frames are silently dropped
// to prevent peer impersonation. The producer path is
// POST /api/channels/{id}/messages — this test exercises that path
// and asserts both WS subscribers observe the message frame.
func TestAC3_ServerWsHub_BroadcastReachesAllSubscribers(t *testing.T) {
	srv := startServerWithDB(t)

	username := randomUsername(t)
	password := randomSecret(t, 16)
	token, _ := registerViaREST(t, srv, username, password)
	chID := createChannelViaREST(t, srv, token, randomChannelName(t))

	// Each subscriber needs its own one-shot ws-ticket per SEC-12.
	ticketA := mintWSTicket(t, srv, token)
	ticketB := mintWSTicket(t, srv, token)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	urlA := fmt.Sprintf("%s?ticket=%s&channel=%s", srv.wsURL, ticketA, chID)
	urlB := fmt.Sprintf("%s?ticket=%s&channel=%s", srv.wsURL, ticketB, chID)

	connA, _, err := websocket.Dial(ctx, urlA, nil)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer connA.CloseNow()
	connB, _, err := websocket.Dial(ctx, urlB, nil)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer connB.CloseNow()

	// Wait for both subscribers to register with the hub before the
	// publisher fires, otherwise the broadcast races the subscribe.
	// Authenticated dials also emit a presence_join broadcast; the
	// reader loop below skips those by filtering on frame.Type.
	waitForSubscriberCount(t, srv, chID, 2, 2*time.Second)

	const messageBody = "broadcast-positive"
	postMessageViaREST(t, srv, token, chID, messageBody)

	// Read frames on each subscriber until a {type:"message"} envelope
	// arrives, ignoring earlier frames (presence_join from authenticated
	// dials). One shared timeout context drives all reads — coder/websocket
	// closes the connection if a per-read context is cancelled, so the
	// reader must NOT use WithTimeout per iteration on a conn it wants
	// to keep using.
	type readResult struct {
		from string
		body string
		err  error
	}
	results := make(chan readResult, 2)
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	var wg sync.WaitGroup
	for _, sub := range []struct {
		name string
		conn *websocket.Conn
	}{{"A", connA}, {"B", connB}} {
		wg.Add(1)
		go func(name string, c *websocket.Conn) {
			defer wg.Done()
			for {
				_, data, err := c.Read(readCtx)
				if err != nil {
					results <- readResult{from: name, err: err}
					return
				}
				var frame struct {
					Type string `json:"type"`
					Data struct {
						Body string `json:"body"`
					} `json:"data"`
				}
				if jerr := json.Unmarshal(data, &frame); jerr != nil {
					results <- readResult{from: name, err: fmt.Errorf("unmarshal: %w body=%s", jerr, data)}
					return
				}
				if frame.Type == "message" {
					results <- readResult{from: name, body: frame.Data.Body}
					return
				}
				// Skip non-message frames (e.g. presence_join).
			}
		}(sub.name, sub.conn)
	}
	wg.Wait()
	close(results)

	got := map[string]string{}
	for r := range results {
		if r.err != nil {
			t.Fatalf("subscriber %s: %v", r.from, r.err)
		}
		got[r.from] = r.body
	}
	if got["A"] != messageBody || got["B"] != messageBody {
		t.Fatalf("broadcast bodies: A=%q B=%q want both %q", got["A"], got["B"], messageBody)
	}
}

// AC-3 (negative twin): an inbound WS frame written by client A must
// NOT echo to client B. Mirrors audit #78's contract — client-emitted
// frames are silently dropped server-side rather than rebroadcast.
func TestAC3_ServerWsHub_InboundFramesNotEchoed(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, srv.wsURL, nil)
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	waitForSubscriberCount(t, srv, "#general", 2, 2*time.Second)

	if err := a.Write(ctx, websocket.MessageText, []byte("forged-by-A")); err != nil {
		t.Fatalf("write from A: %v", err)
	}

	// B should NOT receive A's bytes within a short window. A read
	// timeout is the success path; any frame received (or non-timeout
	// error) is a leak.
	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readCancel()
	_, data, err := b.Read(readCtx)
	if err == nil {
		t.Fatalf("client B received %q from client A; want no echo (raw rebroadcast leaked)", data)
	}
	if readCtx.Err() == nil {
		t.Fatalf("client B read errored without timeout: %v", err)
	}
}
