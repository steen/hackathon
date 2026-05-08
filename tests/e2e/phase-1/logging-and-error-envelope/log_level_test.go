package logging_and_error_envelope_e2e_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCHATLogLevel_WarnSuppressesInfo covers issue #715: setting
// CHAT_LOG_LEVEL=warn must filter out info-level bootstrap lines that
// the default level (info) would emit. The "config check ok" lines and
// the "chat server listening" line are emitted by main.go via slog.Info
// after the leveled handler is installed, so a warn handler must drop
// them while still letting the listener come up.
//
// The test does NOT exercise the access-log middleware — that path uses
// stdlib log and is intentionally out of scope for the bootstrap-only
// CHAT_LOG_LEVEL wiring (see the issue body's "Out of scope" section).
func TestCHATLogLevel_WarnSuppressesInfo(t *testing.T) {
	srv := startServerWithLogLevel(t, "warn")

	// Wait for the listener so the goroutine that would have logged
	// "chat server listening" had a chance to run. waitForPort already
	// confirmed connectivity; sleep briefly to flush any post-listen
	// writes the slog handler still has buffered.
	time.Sleep(150 * time.Millisecond)

	logs := srv.logs.String()

	// At info level these phrases would appear; at warn they must not.
	forbidden := []string{
		"config check ok",
		"chat server listening",
	}
	for _, needle := range forbidden {
		if strings.Contains(logs, needle) {
			t.Errorf("CHAT_LOG_LEVEL=warn leaked info-level line containing %q\nfull log:\n%s", needle, logs)
		}
	}
}

// TestCHATLogLevel_InfoEmitsBootstrap is the positive control: with the
// default level, the same lines DO appear. Without this, the warn test
// could pass for the wrong reason (e.g. the server crashed before
// logging anything).
func TestCHATLogLevel_InfoEmitsBootstrap(t *testing.T) {
	srv := startServerWithLogLevel(t, "info")

	awaitLogLine(t, srv, []string{"chat server listening"}, 5*time.Second)
	awaitLogLine(t, srv, []string{"config check ok"}, 5*time.Second)
}

// startServerWithLogLevel mirrors startServer but lets the caller set
// CHAT_LOG_LEVEL. Kept local to this file — the harness's startServer
// has no opt for arbitrary env additions and broadening it for one
// caller would expand the public surface unnecessarily.
func startServerWithLogLevel(t *testing.T, level string) *runningServer {
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

	logs := &syncBuf{}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
		"CHAT_LOG_LEVEL="+level,
	)
	cmd.Stdout = io.MultiWriter(os.Stderr, logs)
	cmd.Stderr = io.MultiWriter(os.Stderr, logs)
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
		logs:       logs,
		cancel:     cancel,
		wait:       wait,
	}
}
