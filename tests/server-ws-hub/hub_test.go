package server_ws_hub_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// randomSecret returns a hex string sized for the SEC-1 minimum so the test
// harness boots the server without committing a fixed fake-secret literal.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

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
		// PR #28: SEC-1 needs a strong, non-denylisted secret. Generated
		// per-test from crypto/rand so no fake-secret literal is committed
		// to git; lives only in this test process's env.
		"CHAT_JWT_SECRET="+randomSecret(t, 32),
		"CHAT_INVITE_CODE="+randomSecret(t, 8),
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

// Audit #78 (medium) removed the phase-0 raw rebroadcast — inbound WS
// frames are now silently dropped because they let any peer forge
// {type,data} envelopes with arbitrary sender_user_id. The two ACs
// below were originally written against that contract; they are now
// flipped to assert the new (post-removal) behavior:
//
//   - AC2 (hardcoded #general): both clients still join the same default
//     channel — verified via /debug/subs?channel=#general showing 2.
//   - AC3 (broadcast reaches all subscribers): a server-side broadcast
//     onto #general (the supported producer path is REST, but the hub
//     primitive is the same) reaches every WS subscriber. Inbound frames
//     written by a client must NOT reach others.
//
// The original "client A writes, client B reads" assertion is now a
// negative assertion: client B must NOT receive client A's bytes.

func TestAC2_ServerWsHub_HardcodedGeneralChannel(t *testing.T) {
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

	// Wait for both subscribers to register on the shared default
	// channel via the unauthenticated /debug/subs probe.
	debugURL := fmt.Sprintf("http://127.0.0.1:%d/debug/subs?channel=%%23general", srv.port)
	deadline := time.Now().Add(2 * time.Second)
	for {
		count, err := fetchSubscriberCount(debugURL)
		if err != nil {
			t.Fatalf("fetch /debug/subs: %v", err)
		}
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("debug/subs#general = %d after 2s; want 2 (server did not place both on shared channel)", count)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Audit #78 regression: inbound WS frames are dropped, not rebroadcast.
// A peer who writes a frame must not see it echoed to other subscribers.
func TestAC3_ServerWsHub_InboundFramesDropped(t *testing.T) {
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

	if err := conns[0].Write(ctx, websocket.MessageText, []byte("forged-ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Each receiver should NOT observe the forged frame within a
	// short window. We use a per-read deadline shorter than ctx so a
	// timeout is the success path.
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, c := range conns {
		if i == 0 {
			continue // skip the sender (its own frame would not echo regardless)
		}
		wg.Add(1)
		go func(i int, c *websocket.Conn) {
			defer wg.Done()
			readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer readCancel()
			_, data, err := c.Read(readCtx)
			if err == nil {
				errs <- fmt.Errorf("conn[%d] received %q; want timeout (raw rebroadcast leaked)", i, data)
				return
			}
			// Any non-timeout error — close, etc. — is also a failure.
			if readCtx.Err() == nil {
				errs <- fmt.Errorf("conn[%d] read errored without timeout: %w", i, err)
			}
		}(i, c)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// fetchSubscriberCount queries the /debug/subs endpoint and parses
// "<n>\n" into an int.
func fetchSubscriberCount(url string) (int, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper, URL is built from the test-managed loopback port
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var count int
	if _, err := fmt.Fscanf(resp.Body, "%d", &count); err != nil {
		return 0, err
	}
	return count, nil
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
