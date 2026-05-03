package wsapi

import (
	"context"
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

func TestHandlerBroadcastsBetweenClients(t *testing.T) {
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

	if err := sender.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, data, err := receiver.Read(ctx)
	if err != nil {
		t.Fatalf("receiver read: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("got %q want %q", data, "hi")
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
