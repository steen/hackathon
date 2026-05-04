// Package file_perms_and_headers_e2e_test holds black-box tests for the
// phase-1 file-perms-and-headers feature
// (specs/plans/phase-1/feature-file-perms-and-headers.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB path, then asserts
// behavior visible from outside the binary (file modes on disk, response
// headers). No internal-package imports from apps/** — the only coupling
// is the binary path passed to `go build`.
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them).
package file_perms_and_headers_e2e_test

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
	httpURL string // e.g. http://127.0.0.1:54321
	port    int
	dbPath  string
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
// (.../tests/e2e/phase-1/file-perms-and-headers/harness_test.go) to the
// repo root (5 Dir() calls). Sanity-checked by stat-ing go.mod.
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

// startServerOpts lets a test override the default DB path (used by
// AC-1 to seed a too-wide pre-existing file before boot).
type startServerOpts struct {
	// dbPath, if non-empty, is passed via CHAT_DB_PATH instead of the
	// default <tmpDir>/chatd.sqlite. The caller is responsible for
	// pre-creating it (or any parent dir) if that's part of the test.
	dbPath string

	// buildTags, if non-empty, is passed to `go build` as `-tags=<csv>`.
	// AC-3's 500 sub-case uses "panicprobe" so the binary registers
	// /debug/panic from apps/server/internal/wiring/panicprobe.go.
	buildTags string
}

// startServer builds apps/server, picks a free port, starts the binary
// with random secrets and a tempdir DB, and registers a Cleanup that
// stops it. Returns once the port is listening.
func startServer(t *testing.T, opts startServerOpts) *runningServer {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	buildArgs := []string{"build"}
	if opts.buildTags != "" {
		buildArgs = append(buildArgs, "-tags="+opts.buildTags)
	}
	buildArgs = append(buildArgs, "-o", binPath, "./apps/server")
	build := exec.Command("go", buildArgs...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	dbPath := opts.dbPath
	if dbPath == "" {
		dbPath = filepath.Join(tmpDir, "chatd.sqlite")
	}

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+randomSecret(t, 32),
		"CHAT_INVITE_CODE="+randomSecret(t, 8),
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
		httpURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		port:    port,
		dbPath:  dbPath,
		cancel:  cancel,
		wait:    wait,
	}
}
