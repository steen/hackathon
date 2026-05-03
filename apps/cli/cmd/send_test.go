package cmd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/cli/cmd"
)

type recordedFrame struct {
	typ     websocket.MessageType
	payload []byte
}

type fakeWSServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	frames []recordedFrame
	done   chan struct{}
}

func newFakeWSServer(t *testing.T) *fakeWSServer {
	t.Helper()
	f := &fakeWSServer{done: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				close(f.done)
				return
			}
			f.mu.Lock()
			f.frames = append(f.frames, recordedFrame{typ: typ, payload: data})
			f.mu.Unlock()
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWSServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws"
}

func (f *fakeWSServer) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(2 * time.Second):
		t.Fatal("fake server did not observe a client disconnect within 2s")
	}
}

func (f *fakeWSServer) recordedFrames() []recordedFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedFrame, len(f.frames))
	copy(out, f.frames)
	return out
}

func TestUS8_SendReturnsErrorWhenServerUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := cmd.Send(ctx, "ws://127.0.0.1:0/ws", []string{"hello"})
	if err == nil {
		t.Fatal("Send against unreachable URL returned nil error, want non-nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "dial") {
		t.Fatalf("error %q does not mention dial failure", err.Error())
	}
}

func TestUS8_SendWritesJoinedArgsAsSingleTextFrame(t *testing.T) {
	fake := newFakeWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := cmd.Send(ctx, fake.wsURL(), []string{"hello", "world"}); err != nil {
		t.Fatalf("Send returned error %v, want nil", err)
	}
	fake.waitClosed(t)

	frames := fake.recordedFrames()
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want exactly 1", len(frames))
	}
	if frames[0].typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want MessageText", frames[0].typ)
	}
	if got, want := string(frames[0].payload), "hello world"; got != want {
		t.Fatalf("frame payload = %q, want %q", got, want)
	}
}
