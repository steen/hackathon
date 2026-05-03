package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/cli/cmd"
)

func newWriterWSServer(t *testing.T, frames []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for _, m := range frames {
			if err := c.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
				return
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSilentWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURLOf(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func TestAC2_WatchExitsCleanlyOnContextCancel(t *testing.T) {
	srv := newSilentWSServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Watch(ctx, wsURLOf(srv), &bytes.Buffer{})
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Watch did not return within 1s after context cancel")
	}
}

func TestAC2_WatchPrintsEachFrameOnItsOwnLine(t *testing.T) {
	srv := newWriterWSServer(t, []string{"alpha", "beta", "gamma"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := cmd.Watch(ctx, wsURLOf(srv), &buf)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Watch returned %v, want nil or context.Canceled", err)
	}

	if got, want := buf.String(), "alpha\nbeta\ngamma\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
