package cli_dms_e2e_test

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/clihelp"
)

// watchProc is a long-running chatd subprocess wrapper. The shape
// mirrors clihelp.WatchProc but accepts an arbitrary args slice so this
// test package can drive `chatd dm watch [<peer>]` without extending
// the shared helper (which is currently `chatd watch <channel>`-only).
type watchProc struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	stdoutR io.ReadCloser
	stderrR io.ReadCloser

	mu       sync.Mutex
	stdout   strings.Builder
	stderr   strings.Builder
	waitOnce sync.Once
	done     chan struct{}
}

// startWatchProc spawns `chatd --server <httpURL> <args...>` with the
// given XDG dir and returns a handle. Caller must call stop() (defer).
func startWatchProc(t *testing.T, httpURL, xdg string, extraArgs []string) *watchProc {
	t.Helper()
	bin := clihelp.BuildChatd(t)

	args := append([]string{"--server", httpURL}, extraArgs...)
	ctx, cancel := context.WithCancel(context.Background())
	// G204: bin is the per-process build-cache path, args are
	// test-fixture strings from this package. Loopback only.
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // see comment
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdg,
		"HOME="+xdg,
		"CHATD_CONFIG_DIR=",
	)

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("startWatchProc stdout pipe: %v", err)
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("startWatchProc stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("startWatchProc: %v", err)
	}

	w := &watchProc{
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
		close(w.done)
	}()
	return w
}

func (w *watchProc) scan(r io.Reader, into *strings.Builder) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		w.mu.Lock()
		into.WriteString(scanner.Text())
		into.WriteByte('\n')
		w.mu.Unlock()
	}
}

// stdoutSnapshot returns a copy of stdout-so-far. Safe to call
// concurrently with the scanner.
func (w *watchProc) stdoutSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stdout.String()
}

func (w *watchProc) stderrSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderr.String()
}

// waitConnected returns once the watcher appears connected — defined
// as either any stdout output OR a 500ms quiet stretch with no
// "watch:" stderr error. Fails the test on timeout.
func (w *watchProc) waitConnected(t *testing.T, timeout time.Duration) {
	t.Helper()
	settleAt := time.Now().Add(500 * time.Millisecond)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		hasStdout := w.stdout.Len() > 0
		hasErr := strings.Contains(w.stderr.String(), "dm watch:") ||
			strings.Contains(w.stderr.String(), "watch:")
		w.mu.Unlock()
		if hasStdout {
			return
		}
		if hasErr {
			t.Fatalf("watcher saw error before connect: stderr=%q", w.stderrSnapshot())
		}
		if time.Now().After(settleAt) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("watcher did not connect in %s; stderr=%q", timeout, w.stderrSnapshot())
}

// waitForLine polls stdout for any line containing needle. Returns true
// on hit, false on timeout.
func (w *watchProc) waitForLine(needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		hit := strings.Contains(w.stdout.String(), needle)
		w.mu.Unlock()
		if hit {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (w *watchProc) stop() {
	w.waitOnce.Do(func() {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(2 * time.Second):
		}
	})
}
