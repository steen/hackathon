package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/server/internal/hub"
	"github.com/jumoel/hackathon/apps/server/internal/server"
)

func newTestServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.New()
	mux := http.NewServeMux()
	mux.Handle("/ws", server.NewWSHandler(h))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, h
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestAC1_WSHandler_UpgradesHTTPToWebSocket(t *testing.T) {
	ts, _ := newTestServer(t)

	t.Run("valid upgrade returns 101 with Sec-WebSocket-Accept", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		c, resp, err := websocket.Dial(ctx, wsURL(ts.URL)+"/ws", nil)
		if err != nil {
			t.Fatalf("Dial returned error: %v", err)
		}
		defer c.CloseNow()

		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want 101", resp.StatusCode)
		}
		if got := resp.Header.Get("Sec-WebSocket-Accept"); got == "" {
			t.Fatal("missing Sec-WebSocket-Accept header in upgrade response")
		}
	})

	t.Run("non-websocket GET returns 4xx without panic", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ws")
		if err != nil {
			t.Fatalf("GET /ws without upgrade headers errored: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Fatalf("status = %d, want 4xx", resp.StatusCode)
		}
	})
}

func waitForCount(t *testing.T, h *hub.Hub, channel string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := h.SubscriberCount(channel); got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("SubscriberCount(%s) = %d after 2s, want %d", channel, h.SubscriberCount(channel), want)
}

func TestAC2_WSHandler_RegistersConnectionToGeneralChannel(t *testing.T) {
	ts, h := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	waitForCount(t, h, "#general", 1)

	// Closing the connection must trigger the deferred unsubscribe.
	if err := c.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waitForCount(t, h, "#general", 0)
}

func TestAC5_WSHandler_AcceptsConnectionWithoutCredentials(t *testing.T) {
	ts, h := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Build a request with no Authorization header, no cookies, no auth query
	// parameters. coder/websocket.Dial accepts an HTTPHeader option; leaving
	// it nil already sends no credentials, which is what we are asserting.
	c, resp, err := websocket.Dial(ctx, wsURL(ts.URL)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if err != nil {
		t.Fatalf("Dial without credentials returned error: %v", err)
	}
	defer c.CloseNow()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if got := resp.Request.Header.Get("Authorization"); got != "" {
		t.Fatalf("test sent Authorization header %q; expected none", got)
	}
	if cookies := resp.Request.Cookies(); len(cookies) != 0 {
		t.Fatalf("test sent cookies %v; expected none", cookies)
	}

	waitForCount(t, h, "#general", 1)
}
