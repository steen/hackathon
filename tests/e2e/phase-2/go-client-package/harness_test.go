// Package goclientpackage_e2e_test holds black-box E2E tests for the
// phase-2 go-client-package feature
// (specs/plans/phase-2/10-feature-go-client-package.md).
//
// Each test boots the production apps/server binary on a free loopback
// port with random secrets and a per-test SQLite DB, then drives the
// hackathon/packages/go-client surface against it. No internal-package
// imports from apps/** — the only coupling is the go build path passed
// to `go build` and the JSON shapes the server speaks on the wire.
//
// Helpers mirror tests/e2e/phase-2/cli-full-commands/harness_test.go and
// tests/e2e/phase-1/auth-endpoints/harness_test.go. They are not lifted
// to a shared package yet (CLAUDE.md: no shared abstractions until 3+
// features need them).
package goclientpackage_e2e_test

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
	"sync"
	"testing"
	"time"
)

// runningServer is the handle startServer returns to a test. URL is the
// http:// base; inviteCode is the per-test invite secret callers pass to
// goclient.Register; jwtSecret is exposed for tests that mint their own
// JWTs (none in AC-1, but kept for parity with the phase-1/2 harnesses).
type runningServer struct {
	url        string
	port       int
	inviteCode string
	jwtSecret  string
	dbPath     string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// startServer builds apps/server (cached at package level) and launches
// it on a fresh port with fresh secrets and a fresh sqlite db.
// Registers a Cleanup hook that cancels the context and waits for the
// process to exit.
func startServer(t *testing.T) *runningServer {
	t.Helper()

	bin := serverBinary(t)
	port := freePort(t)
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
	invite := randomSecret(t, 8)
	jwt := randomSecret(t, 32)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+jwt,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
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
		url:        fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
		inviteCode: invite,
		jwtSecret:  jwt,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
}

// Build apps/server once per test binary so each per-test startServer
// call is just a subprocess launch, not a fresh compile.
var (
	serverBuildOnce  sync.Once
	serverBuildPath  string
	serverBuildErr   error
	binBuildBaseDir  string
	binBuildBaseOnce sync.Once
)

func binBuildBase(t *testing.T) string {
	t.Helper()
	binBuildBaseOnce.Do(func() {
		// Use os.MkdirTemp — package-scoped, not bound to a single t.
		dir, err := os.MkdirTemp("", "go-client-package-e2e-bin-*")
		if err != nil {
			t.Fatalf("mkdir bin base: %v", err)
		}
		binBuildBaseDir = dir
	})
	return binBuildBaseDir
}

func serverBinary(t *testing.T) string {
	t.Helper()
	serverBuildOnce.Do(func() {
		root := repoRoot(t)
		out := filepath.Join(binBuildBase(t), "chat-server")
		build := exec.Command("go", "build", "-o", out, "./apps/server")
		build.Dir = root
		if combined, err := build.CombinedOutput(); err != nil {
			serverBuildErr = fmt.Errorf("go build ./apps/server: %w\n%s", err, combined)
			return
		}
		serverBuildPath = out
	})
	if serverBuildErr != nil {
		t.Fatalf("%v", serverBuildErr)
	}
	return serverBuildPath
}

// randomSecret returns a hex string sized for the SEC-1 minimum.
// Mirrors tests/e2e/phase-1/auth-endpoints/harness_test.go::randomSecret.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
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

// repoRoot walks up from this file until it finds go.mod. Lets every
// test resolve `go build` paths regardless of where `go test` is
// invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", file)
		}
		dir = parent
	}
}

// randomUsername generates a 12-char ASCII username matching the
// server's `^[A-Za-z0-9_-]{3,32}$` regex. Per-test so concurrent runs
// in `go test -count=N` do not collide on the unique-username
// constraint.
func randomUsername(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "u" + string(b[:11])
}

// randomChannelName returns a name matching the server's
// `^[a-z0-9][a-z0-9-]{0,39}$` regex.
func randomChannelName(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "c" + string(b[:9])
}
