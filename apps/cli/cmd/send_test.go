package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type recordedFrame struct {
	typ  websocket.MessageType
	data []byte
}

func TestAC_0_1_SendWritesSingleTextFrameToWebSocket(t *testing.T) {
	var (
		mu       sync.Mutex
		frames   []recordedFrame
		closeErr error
	)
	done := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer c.CloseNow()
		defer close(done)

		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				mu.Lock()
				closeErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			frames = append(frames, recordedFrame{typ: typ, data: data})
			mu.Unlock()
		}
	})
	s := httptest.NewServer(handler)
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"

	if err := Send(context.Background(), wsURL, []string{"hello", "world"}); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server read loop did not exit within 2s")
	}

	mu.Lock()
	defer mu.Unlock()

	if got, want := len(frames), 1; got != want {
		t.Fatalf("frames received = %d, want %d (frames=%v)", got, want, frames)
	}
	if frames[0].typ != websocket.MessageText {
		t.Errorf("frame type = %v, want MessageText", frames[0].typ)
	}
	if got, want := string(frames[0].data), "hello world"; got != want {
		t.Errorf("frame body = %q, want %q", got, want)
	}

	var ce websocket.CloseError
	if !errors.As(closeErr, &ce) {
		t.Fatalf("close error = %v (%T), want websocket.CloseError", closeErr, closeErr)
	}
	if ce.Code != websocket.StatusNormalClosure {
		t.Errorf("close status = %d, want %d (normal closure)", ce.Code, websocket.StatusNormalClosure)
	}
}

func TestAC_0_1_SendReturnsErrorWhenServerUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Send(ctx, "ws://127.0.0.1:1/ws", []string{"hello"})
	if err == nil {
		t.Fatal("Send() error = nil, want non-nil for unreachable server")
	}
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "dial") &&
		!strings.Contains(strings.ToLower(msg), "connect") &&
		!strings.Contains(strings.ToLower(msg), "refused") {
		t.Errorf("error %q does not mention dial/connect/refused — user can't diagnose it", msg)
	}
}
