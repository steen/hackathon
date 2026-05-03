package cmd_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/cli/cmd"
	"github.com/jumoel/hackathon/packages/go-shared/serverdefaults"
)

type countingWSServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	frames int
}

func newCountingWSServer(t *testing.T) *countingWSServer {
	t.Helper()
	r := &countingWSServer{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
			r.mu.Lock()
			r.frames++
			r.mu.Unlock()
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *countingWSServer) wsURL() string {
	return "ws" + strings.TrimPrefix(r.srv.URL, "http") + "/ws"
}

func (r *countingWSServer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames
}

func (r *countingWSServer) waitFor(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("frame count = %d, want %d within timeout", r.count(), want)
}

func runSend(flag, env string, args []string) error {
	url := cmd.ResolveURL(flag, env)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return cmd.Send(ctx, url, args)
}

func TestAC3_E2E_URLFlagAndEnvDriveDial(t *testing.T) {
	a := newCountingWSServer(t)
	b := newCountingWSServer(t)

	// Case 1: --url=A and CHAT_SERVER=B — flag wins, A receives, B doesn't.
	if err := runSend(a.wsURL(), b.wsURL(), []string{"case1"}); err != nil {
		t.Fatalf("case 1: Send returned %v, want nil", err)
	}
	a.waitFor(t, 1)
	if got := a.count(); got != 1 {
		t.Fatalf("case 1: A count = %d, want 1", got)
	}
	if got := b.count(); got != 0 {
		t.Fatalf("case 1: B count = %d, want 0", got)
	}

	// Case 2: no flag, CHAT_SERVER=B — env wins, B receives, A unchanged.
	if err := runSend("", b.wsURL(), []string{"case2"}); err != nil {
		t.Fatalf("case 2: Send returned %v, want nil", err)
	}
	b.waitFor(t, 1)
	if got := b.count(); got != 1 {
		t.Fatalf("case 2: B count = %d, want 1", got)
	}
	if got := a.count(); got != 1 {
		t.Fatalf("case 2: A count = %d, want 1 (unchanged from case 1)", got)
	}

	// Case 3: no flag, no env — dial targets the default URL (no listener),
	// Send returns a dial error and neither test server records a frame.
	addr := fmt.Sprintf("localhost:%d", serverdefaults.Port)
	if probe, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		probe.Close()
		t.Skipf("port %d already bound; skipping default-URL dial-failure case", serverdefaults.Port)
	}
	err := runSend("", "", []string{"case3"})
	if err == nil {
		t.Fatal("case 3: Send returned nil, want non-nil dial error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "dial") {
		t.Fatalf("case 3: error %q does not reference dial failure", err.Error())
	}
	if got := a.count(); got != 1 {
		t.Fatalf("case 3: A count = %d, want 1 (unchanged)", got)
	}
	if got := b.count(); got != 1 {
		t.Fatalf("case 3: B count = %d, want 1 (unchanged)", got)
	}
}
