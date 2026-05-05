// Issue #714: server logs a soft warning when the SQLite parent
// directory has loose modes (anything other than 0700-style owner-only).
//
// PRD §9 "Persistence hygiene" recommends parent dir 0700 as a
// belt-and-braces companion to the 0600 file mode. The warning is
// informational — the server keeps running.
//
// Source: specs/plans/phase-1/feature-file-perms-and-headers.md (PRD §9
// reference) and the issue body.
package file_perms_and_headers_e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSQLiteParentDirLooseModeWarning seeds the parent directory at 0755
// before boot, then asserts that the server's stdout/stderr carries the
// "WARN: SQLite parent directory" line. The complementary subtest pre-
// creates the directory at 0700 and asserts no such line appears, so the
// test fails on a regression that warns unconditionally.
func TestSQLiteParentDirLooseModeWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes not meaningful on windows")
	}

	t.Run("loose_0755_parent_logs_warning", func(t *testing.T) {
		out := bootCapturingOutput(t, 0o755)
		if !strings.Contains(out, "WARN: SQLite parent directory") {
			t.Fatalf("expected WARN line about loose parent dir mode; got:\n%s", out)
		}
		if !strings.Contains(out, "recommended 0700") {
			t.Fatalf("WARN line should include the 'recommended 0700' guidance; got:\n%s", out)
		}
	})

	t.Run("tight_0700_parent_no_warning", func(t *testing.T) {
		out := bootCapturingOutput(t, 0o700)
		if strings.Contains(out, "WARN: SQLite parent directory") {
			t.Fatalf("did not expect WARN line for 0700 parent dir; got:\n%s", out)
		}
	})
}

// bootCapturingOutput builds the server, pre-creates the parent
// directory at parentMode, boots the binary with CHAT_DB_PATH inside it,
// waits for the listening port (so we know boot completed and the
// parent-dir check ran), then stops the binary and returns the combined
// stdout+stderr.
func bootCapturingOutput(t *testing.T, parentMode os.FileMode) string {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	// Pre-create parent dir with the test's chosen mode. os.Mkdir
	// honors the umask, so chmod afterwards to pin the exact bits.
	parent := filepath.Join(tmpDir, "data")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.Chmod(parent, parentMode); err != nil {
		t.Fatalf("chmod parent %#o: %v", parentMode, err)
	}
	dbPath := filepath.Join(parent, "chatd.sqlite")

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+randomSecret(t, 32),
		"CHAT_INVITE_CODE="+randomSecret(t, 8),
		"CHAT_DB_PATH="+dbPath,
	)
	var (
		buf bytes.Buffer
		mu  sync.Mutex
	)
	cmd.Stdout = &lockedWriter{w: &buf, mu: &mu}
	cmd.Stderr = &lockedWriter{w: &buf, mu: &mu}

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
		t.Fatalf("server did not listen on :%d in time: %v\noutput:\n%s", port, err, snapshot(&mu, &buf))
	}

	// Give the server a beat to flush startup log lines past the
	// "listening" message before we tear it down. The WARN is logged
	// during db.Open which precedes ListenAndServe, so by the time
	// the port accepts a connection the line is already in the
	// buffered writer; the small sleep just covers OS-level pipe
	// buffering on slow CI hosts.
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-wait

	return snapshot(&mu, &buf)
}

// lockedWriter serializes concurrent writes from the spawned process's
// stdout and stderr goroutines into a single buffer. The exec package
// will write from two goroutines without internal synchronization when
// Stdout and Stderr point at the same underlying buffer, so the lock
// here prevents a -race trip.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func snapshot(mu *sync.Mutex, buf *bytes.Buffer) string {
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}
