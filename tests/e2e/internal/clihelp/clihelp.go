// Package clihelp holds shared subprocess plumbing for phase-2+ E2E
// tests that exec the production `chatd` CLI against a running
// `apps/server` instance.
//
// Two distinct call sites grew copies of these helpers before the
// extraction: tests/e2e/phase-2/cli-full-commands/ (one-shot
// chatdRun-per-AC) and tests/e2e/phase-2/presence/ (long-running
// `chatd watch` subprocess for AC-4). The duplication is what
// motivated #380.
//
// The package deliberately takes primitive parameters (httpURL,
// xdg, username, password) rather than each test package's local
// `runningServer` struct — the two existing test packages declare
// runningServer with non-overlapping fields (`url` vs `httpURL`,
// presence/cli-full-commands), so a typed parameter would force
// one of them to rename its harness.
package clihelp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatdBuildOnce/chatdBuildPath/chatdBuildErr cache one `go build
// ./apps/cli` per test process across both call sites — building the
// CLI is the slow step (~1-3s on a warm cache, more on cold) and the
// pre-extraction copies each kept their own cache, so a process that
// ran both presence and cli-full-commands in one `go test ./...`
// compiled the binary twice. Caching in this package means one build
// for both.
var (
	chatdBuildOnce sync.Once
	chatdBuildPath string
	chatdBuildErr  error
)

// BuildChatd builds apps/cli once per test process and returns the
// absolute path to the resulting binary. The build directory is
// created with os.MkdirTemp under os.TempDir() and intentionally NOT
// tied to any single test's t.TempDir — the cache is process-scoped.
// The OS reaps the dir at next reboot.
func BuildChatd(t *testing.T) string {
	t.Helper()
	chatdBuildOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			chatdBuildErr = err
			return
		}
		dir, err := os.MkdirTemp("", "clihelp-chatd-*")
		if err != nil {
			chatdBuildErr = fmt.Errorf("mkdir clihelp-chatd build dir: %w", err)
			return
		}
		out := filepath.Join(dir, "chatd")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// G204: `out` is os.MkdirTemp output, no caller-controlled
		// data flows into the argv. Test-only helper.
		build := exec.CommandContext(ctx, "go", "build", "-o", out, "./apps/cli") //nolint:gosec // see comment
		build.Dir = root
		if combined, err := build.CombinedOutput(); err != nil {
			chatdBuildErr = fmt.Errorf("go build ./apps/cli: %w\n%s", err, combined)
			return
		}
		chatdBuildPath = out
	})
	if chatdBuildErr != nil {
		t.Fatalf("%v", chatdBuildErr)
	}
	return chatdBuildPath
}

// repoRoot walks up from this source file's directory until it finds
// go.mod. Used so callers don't have to thread the repo root through
// every helper. Returns an error rather than calling t.Fatal so
// BuildChatd's cache stores it via the once-Do error path.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", file)
		}
		dir = parent
	}
}

// chatdEnv returns the env every chatd invocation needs: an isolated
// XDG_CONFIG_HOME + HOME so the CLI's per-user config writes land in
// a per-test directory, and CHATD_CONFIG_DIR cleared so XDG resolution
// is genuinely tested.
func chatdEnv(xdg string, extra ...string) []string {
	env := append(os.Environ(),
		"XDG_CONFIG_HOME="+xdg,
		"HOME="+xdg,
		"CHATD_CONFIG_DIR=",
	)
	return append(env, extra...)
}

// LoginViaFlags drives `chatd login` using --username / --password
// flags so the test never hits the readSecret prompt path. This is
// the setup-helper variant — it fails the test on non-zero exit.
// AC tests that need to assert login behavior itself should invoke
// chatd directly rather than through this helper.
func LoginViaFlags(t *testing.T, httpURL, xdg, username, password string) {
	t.Helper()
	bin := BuildChatd(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// G204: `bin` is the per-process build-cache path; `httpURL` and
	// credentials are test fixtures (see RandomUsername/RandomPassword
	// + the per-test loopback server). Test-only helper.
	cmd := exec.CommandContext(ctx, bin, //nolint:gosec // see comment
		"--server", httpURL,
		"login",
		"--username", username,
		"--password", password,
	)
	cmd.Env = chatdEnv(xdg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("chatd login: exit=%d stderr=%q", exitErr.ExitCode(), stderr.String())
		}
		t.Fatalf("chatd login: %v stderr=%q", err, stderr.String())
	}
}

// WatchProc is a long-running `chatd watch` subprocess wrapper. The
// pre-extraction copy lived in presence/clients_surface_test.go; the
// shape (mutex-guarded buffers, channel-based exit detection) is
// preserved verbatim so the AC-4 timing characteristics don't drift.
type WatchProc struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	stdoutR io.ReadCloser
	stderrR io.ReadCloser

	mu       sync.Mutex
	stdout   strings.Builder
	stderr   strings.Builder
	waitOnce sync.Once
	done     chan struct{}
	exited   bool
}

// StartWatch spawns `chatd --server <httpURL> watch <channelArg>`
// with the given XDG dir and returns a handle. The caller must invoke
// Stop() on the handle (via defer) to release the subprocess.
func StartWatch(t *testing.T, httpURL, xdg, channelArg string) *WatchProc {
	t.Helper()
	bin := BuildChatd(t)

	ctx, cancel := context.WithCancel(context.Background())
	// G204: `bin` is the per-process build-cache path; `httpURL` and
	// `channelArg` are test fixtures from a loopback server. Test-only.
	cmd := exec.CommandContext(ctx, bin, "--server", httpURL, "watch", channelArg) //nolint:gosec // see comment
	cmd.Env = chatdEnv(xdg)

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("StartWatch stdout pipe: %v", err)
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("StartWatch stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("StartWatch: %v", err)
	}

	w := &WatchProc{
		cmd:     cmd,
		cancel:  cancel,
		stdoutR: stdoutR,
		stderrR: stderrR,
		done:    make(chan struct{}),
	}

	go w.scan(stdoutR, &w.stdout)
	go w.scan(stderrR, &w.stderr)
	go func() {
		_ = cmd.Wait()
		w.mu.Lock()
		w.exited = true
		w.mu.Unlock()
		close(w.done)
	}()
	return w
}

func (w *WatchProc) scan(r io.Reader, into *strings.Builder) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		w.mu.Lock()
		into.WriteString(scanner.Text())
		into.WriteByte('\n')
		w.mu.Unlock()
	}
}

// StdoutSnapshot returns a copy of the watcher's stdout-so-far. Safe
// to call concurrently with the scanner.
func (w *WatchProc) StdoutSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stdout.String()
}

// StderrSnapshot returns a copy of the watcher's stderr-so-far.
func (w *WatchProc) StderrSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderr.String()
}

// HasExited reports whether the subprocess has reaped via cmd.Wait.
func (w *WatchProc) HasExited() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exited
}

// Stop cancels the subprocess context and waits up to 2s for the
// process to exit. Idempotent — safe to call from a defer even if a
// test path already stopped it explicitly.
func (w *WatchProc) Stop() {
	w.waitOnce.Do(func() {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(2 * time.Second):
		}
	})
}

// RandomUsername returns a 12-char ASCII username matching the
// server's `^[A-Za-z0-9_-]{3,32}$` regex. Per-test so concurrent runs
// in `go test -count=N` do not collide on the unique-username
// constraint.
func RandomUsername(t *testing.T) string {
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

// RandomPassword returns a 32-char hex password — comfortably above
// the server's 10-char minimum, generated per-test from crypto/rand.
func RandomPassword(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return fmt.Sprintf("%x", b)
}

// RandomChannelName returns a name matching the server's
// `^[a-z0-9][a-z0-9-]{0,39}$` regex.
func RandomChannelName(t *testing.T) string {
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
