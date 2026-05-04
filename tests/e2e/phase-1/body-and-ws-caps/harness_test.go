// Package body_and_ws_caps_e2e_test holds black-box tests for the
// phase-1 body-and-ws-caps feature
// (specs/plans/phase-1/feature-body-and-ws-caps.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets, then drives the /ws and /api/* endpoints
// through real network clients. No internal-package imports from
// apps/** — the only coupling is the binary path passed to `go build`.
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them). The pattern mirrors
// tests/e2e/phase-1/auth-endpoints/harness_test.go on
// origin/test/phase-1-auth-endpoints.
package body_and_ws_caps_e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// runningServer carries the per-test handle the helpers need to talk
// to the spawned chat-server.
type runningServer struct {
	httpURL string
	wsURL   string
	port    int
	cancel  context.CancelFunc
	wait    chan struct{}
}

// randomSecret returns a hex string of byteLen random bytes (so the
// printed string is 2*byteLen chars). Per CLAUDE.md "No hardcoded
// secrets" — every JWT secret and invite code in this test tree is
// process-local.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// freePort asks the kernel for a free TCP port on loopback and returns
// it. There is a tiny race window between Close and the server's
// Listen; in practice this loopback path doesn't lose the port.
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

// repoRoot walks up from this file
// (.../tests/e2e/phase-1/body-and-ws-caps/harness_test.go) to the repo
// root (5 dirs up). Sanity-checked by stat-ing go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at %s: %v", root, err)
	}
	return root
}

// startServer builds apps/server, picks a free port, starts the binary
// with random secrets, and registers a Cleanup that stops it. Returns
// once the port is listening.
//
// AC-1 dials /ws without a ws-ticket, so this harness intentionally
// omits CHAT_DB_PATH — when the DB is unset, main.go skips the auth
// stack and Handler runs in its ts==nil branch (apps/server/internal/wsapi/handler.go),
// which accepts unauthenticated upgrades. That keeps this test a pure
// SEC-6 frame-size check, with no auth coupling.
func startServer(t *testing.T) *runningServer {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

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
		httpURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL:   fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:    port,
		cancel:  cancel,
		wait:    wait,
	}
}
