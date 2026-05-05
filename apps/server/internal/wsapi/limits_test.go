package wsapi_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wsapi"
	"hackathon/apps/server/wsproto"
)

func dialServer(ctx context.Context, t *testing.T) (*websocket.Conn, *hub.Hub, func()) {
	t.Helper()
	h := hub.New()
	srv := httptest.NewServer(wsapi.Handler(h, nil, wsapi.Config{}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	// Lift the client read limit so an oversize server-side close frame's
	// reason text never trips a client-side limit before we observe the code.
	conn.SetReadLimit(-1)
	return conn, h, func() {
		_ = conn.CloseNow()
		srv.Close()
	}
}

// TestWSRejectsFrameOver64KiBClose1009 — covers PRD §11 SEC-6.
// Asserts the server closes with WebSocket close code 1009
// (StatusMessageTooBig) when an inbound frame exceeds the read limit.
func TestWSRejectsFrameOver64KiBClose1009(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, cleanup := dialServer(ctx, t)
	defer cleanup()

	oversize := bytes.Repeat([]byte("x"), int(wsapi.ReadLimitBytes)+1)
	if err := conn.Write(ctx, websocket.MessageBinary, oversize); err != nil {
		t.Fatalf("write oversize: %v", err)
	}

	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatalf("expected close error after oversize frame")
	}
	got := websocket.CloseStatus(err)
	if got != websocket.StatusMessageTooBig {
		t.Fatalf("close code: got %d want %d (StatusMessageTooBig); err=%v",
			got, websocket.StatusMessageTooBig, err)
	}
	if int(websocket.StatusMessageTooBig) != 1009 {
		t.Fatalf("library StatusMessageTooBig is not 1009: %d", websocket.StatusMessageTooBig)
	}
}

// TestWSRejectsMessageBodyOver4KiB — covers PRD §11 SEC-8 (WS path).
// A body between 4 KiB and 64 KiB passes the read limit but exceeds
// the per-message body cap, so the server closes with 1009.
func TestWSRejectsMessageBodyOver4KiB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, cleanup := dialServer(ctx, t)
	defer cleanup()

	body := bytes.Repeat([]byte("x"), wsapi.MessageBodyLimit+1)
	if err := conn.Write(ctx, websocket.MessageBinary, body); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatalf("expected close error after over-4KiB body")
	}
	got := websocket.CloseStatus(err)
	if got != websocket.StatusMessageTooBig {
		t.Fatalf("close code: got %d want %d (StatusMessageTooBig); err=%v",
			got, websocket.StatusMessageTooBig, err)
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *websocket.CloseError, got %T: %v", err, err)
	}
	if ce.Reason != wsproto.MessageBodyLimitCloseReason {
		t.Fatalf("close reason: got %q want %q", ce.Reason, wsproto.MessageBodyLimitCloseReason)
	}
}

// TestWSSendRateLimitClosesPolicyViolation — covers PRD §9 send rate
// limit. After exhausting burst+steady-state tokens, the server closes
// with StatusPolicyViolation (1008).
func TestWSSendRateLimitClosesPolicyViolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, cleanup := dialServer(ctx, t)
	defer cleanup()

	// Burst is 30; flooding well past it within a few ms drains the
	// bucket faster than refill (10/s) can replenish.
	for i := 0; i < wsapi.SendRateBurst+20; i++ {
		if err := conn.Write(ctx, websocket.MessageText, []byte("x")); err != nil {
			break
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("close code: got %d want %d (StatusPolicyViolation); err=%v",
					websocket.CloseStatus(err), websocket.StatusPolicyViolation, err)
			}
			if int(websocket.StatusPolicyViolation) != 1008 {
				t.Fatalf("library StatusPolicyViolation is not 1008: %d", websocket.StatusPolicyViolation)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never closed after burst flood")
		}
	}
}
