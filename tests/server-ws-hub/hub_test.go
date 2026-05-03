package server_ws_hub_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// System-level tests for specs/plans/phase-0/feature-server-ws-hub.md.
// Builds and launches apps/server on a chosen port, then dials it. Deeper
// unit-level coverage of the hub and wsapi handler lives next to the impl
// in apps/server/internal/{hub,wsapi}/*_test.go. The tests in this file
// only assert behavior visible from outside the binary.

type runningServer struct {
	url    string
	port   int
	cancel context.CancelFunc
	wait   chan struct{}
}

func startServer(t *testing.T) *runningServer {
	t.Helper()

	root := repoRoot(t)
	binPath := filepath.Join(t.TempDir(), "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET=test-secret-32-bytes-min-aaaaaaaa",
		"CHAT_INVITE_CODE=test-invite",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	wait := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(wait)
	}()

	if err := waitForPort(port, 10*time.Second); err != nil {
		cancel()
		<-wait
		t.Fatalf("server did not listen on :%d in time: %v", port, err)
	}

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &runningServer{
		url:    fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:   port,
		cancel: cancel,
		wait:   wait,
	}
}

func TestAC1_ServerWsHub_WsEndpointAccepts101Upgrade(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %v, want 101", resp)
	}
}

func TestAC2_ServerWsHub_HardcodedGeneralChannel(t *testing.T) {
	// Two independent clients must both observe a frame written by either.
	// If the handler subscribed each conn to a per-conn ephemeral channel,
	// b would never see a's write.
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	// Give the server a brief moment to register both subscribers before
	// broadcast (read loops are goroutines started after Subscribe but the
	// dial returning is not a synchronization point on the server side).
	time.Sleep(50 * time.Millisecond)

	if err := a.Write(ctx, websocket.MessageText, []byte("from-a")); err != nil {
		t.Fatalf("a write: %v", err)
	}
	_, data, err := b.Read(ctx)
	if err != nil {
		t.Fatalf("b read: %v", err)
	}
	if string(data) != "from-a" {
		t.Fatalf("b received %q, want %q (server did not place both clients on a shared channel)", data, "from-a")
	}
}

func TestAC3_ServerWsHub_BroadcastReachesAllSubscribers(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const n = 3
	conns := make([]*websocket.Conn, n)
	for i := 0; i < n; i++ {
		c, _, err := websocket.Dial(ctx, srv.url, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		t.Cleanup(func() { c.CloseNow() })
		conns[i] = c
	}

	time.Sleep(50 * time.Millisecond)

	if err := conns[0].Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, c := range conns {
		wg.Add(1)
		go func(i int, c *websocket.Conn) {
			defer wg.Done()
			_, data, err := c.Read(ctx)
			if err != nil {
				errs <- fmt.Errorf("conn[%d] read: %w", i, err)
				return
			}
			if string(data) != "ping" {
				errs <- fmt.Errorf("conn[%d] received %q, want %q", i, data, "ping")
			}
		}(i, c)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestAC4_ServerWsHub_ServerListensOnConfiguredPort(t *testing.T) {
	// startServer launches the binary with CHAT_SERVER_PORT set and then
	// waits for the chosen port to be listening. If the env var were
	// ignored or the default port hardcoded, waitForPort would time out
	// and startServer would already have failed. The dial here is a
	// belt-and-suspenders check.
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial port %d: %v", srv.port, err)
	}
	defer c.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status on configured port %d = %d, want 101", srv.port, resp.StatusCode)
	}
}

func TestAC5_ServerWsHub_NoAuthorizationHeaderRequiredOnUpgrade(t *testing.T) {
	srv := startServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// websocket.Dial with nil opts sends no Authorization header. A
	// successful upgrade therefore proves the server doesn't require one.
	c, resp, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial without Authorization header: %v", err)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %v, want 101 (server should not require auth in phase 0)", resp)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../tests/server-ws-hub/hub_test.go ; up three to repo root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at %s: %v", root, err)
	}
	return root
}
