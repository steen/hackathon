package testsupport

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// StartOptions configures StartServer. Every field is optional; the
// zero value boots a default chat-server (random secrets, fresh
// tempdir DB, no extra env, stderr passed through to os.Stderr).
type StartOptions struct {
	// BuildTags, if non-empty, is forwarded to `go build -tags=<csv>`.
	// Used by tests that need a build-tag-gated route (e.g. the
	// panicprobe wiring under apps/server/internal/wiring/panicprobe.go).
	BuildTags string

	// DBPath, if non-empty, is passed via CHAT_DB_PATH instead of the
	// default <tmpDir>/chatd.sqlite. Used by tests that pre-create the
	// file with non-default permissions (file-perms feature).
	DBPath string

	// JWTSecret / InviteCode override the random per-call defaults. A
	// caller that needs to mint its own JWTs out-of-band sets these to
	// known values and reads them back from Server.
	JWTSecret  string
	InviteCode string

	// ExtraEnv is appended to the spawned process environment after
	// CHAT_* defaults. Later entries shadow earlier ones, so callers
	// that want a different CHAT_* value than the defaults can
	// override here (e.g. CHAT_REGISTER_BURST for tests that register
	// many users).
	ExtraEnv []string

	// LogWriter, if non-nil, receives a tee of the server's stderr in
	// addition to os.Stderr. Tests that assert on access-log output
	// pass a thread-safe buffer.
	LogWriter io.Writer

	// BinaryPath, if non-empty, skips the per-call `go build` and
	// execs the named binary directly. Used by packages that cache
	// the build across tests (channels-and-messages keeps a sync.Once
	// build to amortize the cost).
	BinaryPath string

	// StartTimeout overrides the default 10s wait for the port to
	// listen. Set this only if the default is too tight for a
	// CI-emulated slow box.
	StartTimeout time.Duration
}

// Server is the handle returned by StartServer. The fields are public
// so tests can read them; the cleanup is registered on the *testing.T
// passed to StartServer, so callers don't need to call Close.
type Server struct {
	HTTPURL    string // e.g. http://127.0.0.1:54321
	WSURL      string // e.g. ws://127.0.0.1:54321/ws
	Port       int
	DBPath     string
	JWTSecret  string
	InviteCode string

	// Cancel stops the spawned server early; t.Cleanup also calls it.
	// Tests that need to assert post-shutdown behavior (e.g. file
	// modes left on disk) call Cancel + <-Wait themselves.
	Cancel context.CancelFunc
	Wait   chan struct{}
}

// StartServer builds (or re-uses) apps/server, picks a free port,
// starts the binary with the merged environment, and registers a
// t.Cleanup that stops it. Returns once the port is listening or
// fails the test on timeout.
func StartServer(t *testing.T, opts StartOptions) *Server {
	t.Helper()

	tmpDir := t.TempDir()

	binPath := opts.BinaryPath
	if binPath == "" {
		root := RepoRoot(t)
		binPath = filepath.Join(tmpDir, "chat-server")
		args := []string{"build"}
		if opts.BuildTags != "" {
			args = append(args, "-tags="+opts.BuildTags)
		}
		args = append(args, "-o", binPath, "./apps/server")
		// noctx: synchronous one-shot build; no outer context to thread.
		build := exec.Command("go", args...) //nolint:noctx // see comment
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build ./apps/server (tags=%q) failed: %v\n%s", opts.BuildTags, err, out)
		}
	}

	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(tmpDir, "chatd.sqlite")
	}

	jwtSecret := opts.JWTSecret
	if jwtSecret == "" {
		jwtSecret = RandomSecret(t, 32)
	}
	invite := opts.InviteCode
	if invite == "" {
		invite = RandomSecret(t, 8)
	}

	port := FreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	// G204: binPath is either an opts.BinaryPath supplied by a test
	// harness or the per-test t.TempDir() build output. In both cases
	// no untrusted input flows into argv. Test-only helper.
	cmd := exec.CommandContext(ctx, binPath) //nolint:gosec // see comment
	env := append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
	)
	env = append(env, opts.ExtraEnv...)
	cmd.Env = env

	if opts.LogWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stderr, opts.LogWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, opts.LogWriter)
	} else {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	wait := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(wait)
	}()

	timeout := opts.StartTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if err := WaitForPort(port, timeout); err != nil {
		cancel()
		<-wait
		t.Fatalf("server did not listen on :%d in time: %v", port, err)
	}

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &Server{
		HTTPURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		WSURL:      fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		Port:       port,
		DBPath:     dbPath,
		JWTSecret:  jwtSecret,
		InviteCode: invite,
		Cancel:     cancel,
		Wait:       wait,
	}
}
