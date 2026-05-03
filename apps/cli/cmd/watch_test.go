package cmd

import (
	"bytes"
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

func TestAC_0_2_WatchPrintsEachFrameOnItsOwnLine(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer c.CloseNow()

		ctx := r.Context()
		for _, msg := range []string{"a", "b", "c"} {
			if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
				t.Errorf("server write: %v", err)
				return
			}
		}
		c.Close(websocket.StatusNormalClosure, "")
	})
	s := httptest.NewServer(handler)
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Watch(ctx, wsURL, &out); err != nil {
		t.Fatalf("Watch() error = %v, want nil", err)
	}

	got := out.String()
	want := "a\nb\nc\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestAC_0_2_WatchExitsCleanlyOnContextCancel(t *testing.T) {
	serverDone := make(chan struct{})
	var (
		mu             sync.Mutex
		serverCloseErr error
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			close(serverDone)
			return
		}
		defer c.CloseNow()
		defer close(serverDone)

		// Send a ready frame so the test can deterministically know the
		// client's Dial has returned and the read loop is running.
		if err := c.Write(r.Context(), websocket.MessageText, []byte("ready")); err != nil {
			t.Errorf("server write: %v", err)
			return
		}

		// Block on Read until client initiates close. Read returns a
		// CloseError when the client sends a close frame.
		_, _, err = c.Read(r.Context())
		mu.Lock()
		serverCloseErr = err
		mu.Unlock()
	})
	s := httptest.NewServer(handler)
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())

	out := &safeBuffer{}
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- Watch(ctx, wsURL, out)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "ready\n") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "ready\n") {
		cancel()
		t.Fatal("client did not receive ready frame within 2s")
	}

	cancel()

	select {
	case err := <-watchErr:
		if err != nil {
			t.Errorf("Watch() error = %v, want nil (clean exit on context cancel)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return within 2s of context cancel")
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe close within 2s")
	}

	mu.Lock()
	defer mu.Unlock()
	if serverCloseErr == nil {
		t.Fatal("server saw no error on Read; client did not initiate close handshake")
	}
	var ce websocket.CloseError
	if !errors.As(serverCloseErr, &ce) {
		t.Fatalf("server close error = %v (%T), want websocket.CloseError (client should send close frame)", serverCloseErr, serverCloseErr)
	}
}
