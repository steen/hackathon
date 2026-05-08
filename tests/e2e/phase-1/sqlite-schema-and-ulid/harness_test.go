// Package sqlite_schema_and_ulid_e2e_test holds black-box tests for the
// phase-1 sqlite-schema-and-ulid feature
// (specs/plans/phase-1/feature-sqlite-schema-and-ulid.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB, then opens that
// SQLite file read-only via the modernc.org/sqlite driver to inspect
// the schema the server applied at startup. No internal-package
// imports from apps/** — coupling is limited to the binary path passed
// to `go build` and the on-disk DB file.
//
// Helpers live in this file rather than a shared package because there
// is no third call site yet (CLAUDE.md: no shared abstractions until
// 3+ features need them). Pattern copied from
// tests/e2e/phase-1/auth-endpoints/harness_test.go.
package sqlite_schema_and_ulid_e2e_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type runningServer struct {
	httpURL    string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

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
// (.../tests/e2e/phase-1/sqlite-schema-and-ulid/harness_test.go) to the
// repo root: five Dir() calls, sanity-checked by stat-ing go.mod.
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
	jwtSecret := randomSecret(t, 32)
	invite := randomSecret(t, 8)
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
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
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
		dbPath:     dbPath,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

// openDBReadOnly opens the running server's SQLite file in read-only
// mode so the test can SELECT without contending with the server's
// writes. Caller need not Close — t.Cleanup handles it.
func openDBReadOnly(t *testing.T, srv *runningServer) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: srv.dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
