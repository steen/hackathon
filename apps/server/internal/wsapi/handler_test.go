package wsapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/hub"
)

func TestHandlerBroadcastsBetweenClients(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h))
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
	srv := httptest.NewServer(Handler(h))
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
