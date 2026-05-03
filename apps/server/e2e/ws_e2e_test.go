// Package e2e exercises the chat WebSocket server end-to-end. The handler
// chain (config -> hub -> WSHandler) is the real one; only the network
// boundary is wrapped in httptest, except for AC-4 which spawns the actual
// `apps/server` binary so the configurable-port behaviour can be verified
// against a real listening process.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/server/internal/config"
	"github.com/jumoel/hackathon/apps/server/internal/hub"
	"github.com/jumoel/hackathon/apps/server/internal/server"
)

// newE2EServer wires the real WSHandler against a real Hub on an httptest
// server. E2E here means "no mocks of the hub or upgrader".
func newE2EServer(t *testing.T) (string, *hub.Hub) {
	t.Helper()
	h := hub.New()
	mux := http.NewServeMux()
	mux.Handle("/ws", server.NewWSHandler(h))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL, h
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws"
}

func dialWS(t *testing.T, ctx context.Context, addr string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, addr, nil)
	if err != nil {
		t.Fatalf("Dial(%s): %v", addr, err)
	}
	return c
}

func waitForCount(t *testing.T, h *hub.Hub, channel string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := h.SubscriberCount(channel); got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("SubscriberCount(%s) = %d after 2s, want %d", channel, h.SubscriberCount(channel), want)
}

func expectMessage(t *testing.T, ctx context.Context, c *websocket.Conn, want []byte) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, got, err := c.Read(readCtx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

func expectNoMessage(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, msg, err := c.Read(ctx)
	if err == nil {
		t.Fatalf("expected no further message; got %q", msg)
	}
}

func TestE2E_AC1_ServerExposesWSEndpoint(t *testing.T) {
	base, _ := newE2EServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, wsURL(base), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer c.CloseNow()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
}

func TestE2E_AC2_DisconnectRemovesSubscriberFromGeneral(t *testing.T) {
	base, h := newE2EServer(t)

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a := dialWS(t, ctx, wsURL(base))
	b := dialWS(t, ctx, wsURL(base))

	waitForCount(t, h, "#general", 2)

	// Drain A's writer once we close it: keep reading until the conn errors,
	// otherwise the server-side writer can deadlock trying to flush before
	// the close frame is processed.
	if err := a.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("close A: %v", err)
	}

	waitForCount(t, h, "#general", 1)

	// Have B broadcast — A must not be in the delivery set anymore.
	if err := b.Write(ctx, websocket.MessageText, []byte("after-A-closed")); err != nil {
		t.Fatalf("B write: %v", err)
	}
	expectMessage(t, ctx, b, []byte("after-A-closed"))

	// Close B and let the hub drain.
	if err := b.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("close B: %v", err)
	}
	waitForCount(t, h, "#general", 0)

	// Goroutine-leak check (goleak-equivalent): the per-connection reader and
	// writer for both connections should have exited; total goroutines should
	// settle back to baseline. Allow a small margin for httptest's connection
	// pool to release.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baselineGoroutines+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutine leak suspected: baseline=%d, current=%d", baselineGoroutines, runtime.NumGoroutine())
}

func TestE2E_AC3_MessageFromOneClientReachesAllOthers(t *testing.T) {
	base, h := newE2EServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c1 := dialWS(t, ctx, wsURL(base))
	defer c1.CloseNow()
	c2 := dialWS(t, ctx, wsURL(base))
	defer c2.CloseNow()
	c3 := dialWS(t, ctx, wsURL(base))
	defer c3.CloseNow()

	waitForCount(t, h, "#general", 3)

	payload := []byte("hello channel")
	if err := c1.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("c1 write: %v", err)
	}

	// All three clients receive concurrently; reading sequentially can stall
	// behind a slow read on another client, so fan out.
	var wg sync.WaitGroup
	for name, c := range map[string]*websocket.Conn{"c1": c1, "c2": c2, "c3": c3} {
		wg.Add(1)
		go func(name string, c *websocket.Conn) {
			defer wg.Done()
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_, got, err := c.Read(readCtx)
			if err != nil {
				t.Errorf("%s read: %v", name, err)
				return
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("%s payload = %q, want %q", name, got, payload)
			}
		}(name, c)
	}
	wg.Wait()

	// "Exactly once": a second read on c2/c3 must time out.
	expectNoMessage(t, c2)
	expectNoMessage(t, c3)
}

func TestE2E_AC5_UnauthenticatedClientCanSendAndReceive(t *testing.T) {
	base, h := newE2EServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, wsURL(base), nil)
	if err != nil {
		t.Fatalf("a dial: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, wsURL(base), nil)
	if err != nil {
		t.Fatalf("b dial: %v", err)
	}
	defer b.CloseNow()

	waitForCount(t, h, "#general", 2)

	if err := a.Write(ctx, websocket.MessageText, []byte("anon")); err != nil {
		t.Fatalf("a write: %v", err)
	}
	expectMessage(t, ctx, b, []byte("anon"))
}

func TestE2E_AC4_ServerListensOnConfiguredPort(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not on PATH")
	}

	// Pick a free port for the server to listen on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeport: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close freeport listener: %v", err)
	}

	// Skip the negative half if the default port is occupied externally —
	// otherwise the assertion "default port not bound by this server" would be
	// a false positive.
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", config.DefaultPort), 200*time.Millisecond); err == nil {
		conn.Close()
		t.Skipf("default port %d is occupied externally; cannot validate negative assertion", config.DefaultPort)
	}

	binary := buildServerBinary(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", config.EnvPort, port))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait for the server to start listening on the configured port.
	if !waitForListen(fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second) {
		t.Fatalf("server not listening on configured port %d after 5s\nserver output:\n%s", port, output.String())
	}

	// Positive: WS dial on the configured port succeeds.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", port), nil)
	if err != nil {
		t.Fatalf("Dial configured port %d: %v", port, err)
	}
	defer c.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	// Negative: TCP dial to the default port must fail (this server instance
	// is bound only to the configured port).
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", config.DefaultPort), 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("server unexpectedly accepting connections on default port %d", config.DefaultPort)
	}
}

// buildServerBinary compiles ./apps/server into a per-test temp directory and
// returns the binary path. The workspace root is found by walking up from this
// test file's location until a go.work is seen.
func buildServerBinary(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not locate go.work above test file")
		}
		root = parent
	}

	bin := filepath.Join(t.TempDir(), "server")
	cmd := exec.Command("go", "build", "-o", bin, "./apps/server")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server: %v\n%s", err, out)
	}
	return bin
}

func waitForListen(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			conn.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
