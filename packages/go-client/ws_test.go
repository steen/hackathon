package goclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	goclient "hackathon/packages/go-client"
)

// TestWatchEndToEnd stands up a tiny in-process server that mints a
// ticket, validates it on /ws, accepts the upgrade, and pushes one
// {type:"message", data:<Message>} frame. The test then drains Watch's
// channel and asserts the typed Message is decoded.
func TestWatchEndToEnd(t *testing.T) {
	const wantTicket = "abc123"
	const wantBody = "hello over ws"

	mux := http.NewServeMux()
	var ticketIssued sync.Once
	mux.HandleFunc("/api/auth/ws-ticket", func(w http.ResponseWriter, _ *http.Request) {
		ticketIssued.Do(func() {})
		_, _ = w.Write([]byte(envelopeJSON(
			`{"ticket":"` + wantTicket + `","expires_at":"2099-01-01T00:00:00Z"}`,
		)))
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ticket"); got != wantTicket {
			http.Error(w, "bad ticket", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("WS upgrade must not carry Authorization header")
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // httptest origin = ""
		})
		if err != nil {
			t.Errorf("ws accept: %v", err)
			return
		}
		defer func() { _ = conn.CloseNow() }()

		frame, _ := json.Marshal(map[string]interface{}{
			"type": "message",
			"data": map[string]interface{}{
				"id":             "01MSGAAAAAAAAAAAAAAAAAAAAA",
				"channel_id":     fixtureChannelID,
				"sender_user_id": "u1",
				"body":           wantBody,
				"created_at":     "2026-05-03T10:00:00Z",
			},
		})
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
			t.Errorf("ws write: %v", err)
		}
		// Hold the connection briefly so the client has time to read.
		select {
		case <-ctx.Done():
		case <-r.Context().Done():
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("user-tok"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := c.Watch(ctx, goclient.WatchOptions{ChannelID: fixtureChannelID})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("events channel closed before frame")
		}
		if ev.Type != "message" {
			t.Fatalf("Type = %q, want message", ev.Type)
		}
		if ev.Message == nil {
			t.Fatalf("Message nil; raw=%s", ev.Raw)
		}
		if ev.Message.Body != wantBody {
			t.Fatalf("Body = %q, want %q", ev.Message.Body, wantBody)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for frame")
	}
}

func TestWatchPropagatesTicketError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(envelopeError("unauthorized", "missing token")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if _, err := c.Watch(ctx, goclient.WatchOptions{}); err == nil {
		t.Fatalf("expected error when ticket mint fails")
	}
}

// TestWatchClosesOnReadIdleTimeout stands up a server that accepts the
// upgrade and then sits silent. With ReadIdleTimeout set tight, Watch
// must tear down and close its events channel inside that bound — the
// stale-connection blindness Phase 4 audit (#601) called out.
func TestWatchClosesOnReadIdleTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/ws-ticket", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(`{"ticket":"t","expires_at":"2099-01-01T00:00:00Z"}`)))
	})
	serverDone := make(chan struct{})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		// Hold the connection open without writing anything until the
		// client tears it down or the test ends.
		<-r.Context().Done()
		close(serverDone)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("u"))
	// Outer ctx is generous; the idle timeout is what should fire.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	idle := 200 * time.Millisecond
	events, err := c.Watch(ctx, goclient.WatchOptions{ReadIdleTimeout: idle})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	start := time.Now()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatalf("expected no frame; got one")
		}
		// Channel closed — that is the success path.
	case <-time.After(2 * time.Second):
		t.Fatalf("events channel still open after 2s with idle=%s", idle)
	}
	elapsed := time.Since(start)
	if elapsed < idle {
		t.Fatalf("Watch returned too early: %s < idle=%s", elapsed, idle)
	}
	// 1.5s gives the server-side r.Context() time to fire after the
	// client closes; without it the goroutine would otherwise be visible
	// as a leak in -race.
	select {
	case <-serverDone:
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestWatchClosesOnContextCancel verifies the issue's secondary concern:
// cancelling the caller-supplied ctx must drive the read goroutine to
// exit. The coder/websocket library implements ctx-cancel by calling
// c.close() via context.AfterFunc (conn.go), which unblocks Read.
func TestWatchClosesOnContextCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/ws-ticket", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(`{"ticket":"t","expires_at":"2099-01-01T00:00:00Z"}`)))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("u"))
	ctx, cancel := context.WithCancel(context.Background())
	// Disable the idle bound so only the ctx cancel can drive the exit.
	events, err := c.Watch(ctx, goclient.WatchOptions{ReadIdleTimeout: -1})
	if err != nil {
		cancel()
		t.Fatalf("Watch: %v", err)
	}

	// Give the read goroutine a moment to enter conn.Read.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatalf("expected no frame; got one")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("events channel still open 2s after ctx cancel")
	}
}

func TestWatchUnknownFrameSurfacesAsRaw(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/ws-ticket", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(`{"ticket":"t","expires_at":"2099-01-01T00:00:00Z"}`)))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte("not-json"))
		<-ctx.Done()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("u"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := c.Watch(ctx, goclient.WatchOptions{})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Type != "" {
			t.Fatalf("Type = %q, want empty for raw frame", ev.Type)
		}
		if string(ev.Raw) != "not-json" {
			t.Fatalf("Raw = %q", ev.Raw)
		}
	case <-ctx.Done():
		t.Fatalf("timed out")
	}
}
