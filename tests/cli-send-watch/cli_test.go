package cli_send_watch_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/cli/cmd"
)

// System-level anchors for the AC IDs in
// specs/plans/phase-0/feature-cli-send-watch.md. Deeper coverage lives in
// apps/cli/cmd/*_test.go (which records protocol-level details). These tests
// exercise only the system-visible behavior of cmd.Send / cmd.Watch /
// cmd.ResolveURL via their public API.

type recordingHandler struct {
	mu      sync.Mutex
	headers []http.Header
	frames  [][]byte
	emit    [][]byte // optional frames to send after accept
}

func (r *recordingHandler) Handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		hdr := req.Header.Clone()
		r.headers = append(r.headers, hdr)
		r.mu.Unlock()

		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer c.CloseNow()

		ctx := req.Context()
		for _, m := range r.emit {
			if err := c.Write(ctx, websocket.MessageText, m); err != nil {
				return
			}
		}

		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			r.mu.Lock()
			r.frames = append(r.frames, append([]byte(nil), data...))
			r.mu.Unlock()
		}
	}
}

func newFakeWS(t *testing.T, h *recordingHandler) string {
	t.Helper()
	srv := httptest.NewServer(h.Handler(t))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func TestAC1_CliSendWatch_SendWritesPayloadAsTextFrameAndExitsZero(t *testing.T) {
	rec := &recordingHandler{}
	url := newFakeWS(t, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cmd.Send(ctx, url, []string{"hello", "world"}); err != nil {
		t.Fatalf("Send returned error %v, want nil (AC requires exit-0 on success)", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := len(rec.frames)
		rec.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.frames) != 1 {
		t.Fatalf("server received %d frames, want exactly 1", len(rec.frames))
	}
	if got := string(rec.frames[0]); got != "hello world" {
		t.Errorf("frame body = %q, want %q", got, "hello world")
	}
}

// syncBuf is a goroutine-safe wrapper around bytes.Buffer. cmd.Watch writes
// from its read goroutine while the test polls String() — bare bytes.Buffer
// is not safe for concurrent use, so unprotected sharing trips the race
// detector even when the visible behavior is correct.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestAC2_CliSendWatch_WatchPrintsEveryReceivedFrameOnePerLine(t *testing.T) {
	rec := &recordingHandler{
		emit: [][]byte{[]byte("one"), []byte("two"), []byte("three")},
	}
	url := newFakeWS(t, rec)

	var out syncBuf
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Watch returns when the server closes the connection (which the fake
	// won't do on its own) or when ctx is cancelled. Run it in a goroutine
	// and cancel after the expected output appears.
	done := make(chan error, 1)
	go func() { done <- cmd.Watch(ctx, url, &out) }()

	deadline := time.Now().Add(2 * time.Second)
	want := "one\ntwo\nthree\n"
	for time.Now().Before(deadline) {
		if out.String() == want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return within 2s of ctx cancel")
	}

	if got := out.String(); got != want {
		t.Errorf("Watch output = %q, want %q (one frame per line, in order)", got, want)
	}
}

func TestAC3_CliSendWatch_UrlPrecedenceFlagOverEnvOverDefault(t *testing.T) {
	t.Setenv("CHAT_SERVER", "ws://env.example/ws")

	if got := cmd.ResolveURL("ws://flag.example/ws"); got != "ws://flag.example/ws" {
		t.Errorf("flag set: ResolveURL = %q, want flag value (flag must beat env)", got)
	}

	if got := cmd.ResolveURL(""); got != "ws://env.example/ws" {
		t.Errorf("flag unset, env set: ResolveURL = %q, want env value", got)
	}

	t.Setenv("CHAT_SERVER", "")
	if got := cmd.ResolveURL(""); !strings.HasPrefix(got, "ws://localhost:") || !strings.HasSuffix(got, "/ws") {
		t.Errorf("flag and env unset: ResolveURL = %q, want ws://localhost:<port>/ws", got)
	}
}

func TestAC4_CliSendWatch_NoAuthorizationHeaderOnUpgrade(t *testing.T) {
	rec := &recordingHandler{}
	url := newFakeWS(t, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cmd.Send(ctx, url, []string{"hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.headers) == 0 {
		t.Fatal("server saw no upgrade request")
	}
	for i, h := range rec.headers {
		if v := h.Get("Authorization"); v != "" {
			t.Errorf("upgrade %d sent Authorization header %q; phase-0 forbids auth", i, v)
		}
	}
}
