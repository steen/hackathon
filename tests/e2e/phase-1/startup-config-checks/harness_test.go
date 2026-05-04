// Package startup_config_checks_e2e_test holds black-box tests for the
// phase-1 startup config check feature
// (specs/plans/phase-1/feature-startup-config-checks.md).
//
// Unlike sibling phase-1 e2e packages, this feature primarily tests the
// *failure* boot path: the binary must exit non-zero with a clear error
// when env config is invalid. The harness therefore exposes
// tryStartServer, which captures the binary's exit code and combined
// output rather than waiting for a "Started" log line on stdout.
//
// Helpers live in this file rather than a shared package: there is no
// third call site yet (CLAUDE.md: no shared abstractions until 3+
// features need them) and the auth-endpoints harness is structured
// around the success path.
package startup_config_checks_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

// builtBinary is the path to a `go build`'d apps/server binary, built
// once per test process. Multiple tests in this package share the
// build to keep `go test ./...` fast.
var (
	builtBinaryOnce sync.Once
	builtBinaryPath string
	builtBinaryErr  error
)

// repoRoot walks up from this file (.../tests/e2e/phase-1/
// startup-config-checks/harness_test.go) to the repo root (5 dirs up).
// Sanity-checked by stat-ing go.mod.
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

// buildServerBinary builds apps/server once per process and returns
// the on-disk path. The binary lives in os.TempDir() (not t.TempDir)
// so it survives across the whole package's tests.
func buildServerBinary(t *testing.T) string {
	t.Helper()
	builtBinaryOnce.Do(func() {
		root := repoRoot(t)
		dir, err := os.MkdirTemp("", "startup-config-checks-bin-")
		if err != nil {
			builtBinaryErr = fmt.Errorf("mkdir tempdir: %w", err)
			return
		}
		bin := filepath.Join(dir, "chat-server")
		cmd := exec.Command("go", "build", "-o", bin, "./apps/server")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			builtBinaryErr = fmt.Errorf("go build ./apps/server: %w\n%s", err, out)
			return
		}
		builtBinaryPath = bin
	})
	if builtBinaryErr != nil {
		t.Fatalf("build server: %v", builtBinaryErr)
	}
	return builtBinaryPath
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

// envSlice converts a map to the KEY=VALUE slice exec.Cmd.Env wants.
// The returned slice does NOT inherit the parent process env — that
// matters for the AC-4 missing-invite-code case where an inherited
// CHAT_INVITE_CODE would mask the condition under test.
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// tryStartServer runs the built binary synchronously with the supplied
// env and a hard 10-second wall clock. Returns the exit code and the
// combined stdout+stderr captured during the run.
//
// A misconfigured server is expected to exit before binding any port,
// so this helper does NOT wait for a listening port.
func tryStartServer(t *testing.T, env map[string]string) (exitCode int, output string) {
	t.Helper()
	bin := buildServerBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = envSlice(env)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.String()
	if err == nil {
		return 0, out
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), out
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("tryStartServer: server did not exit within 10s; output:\n%s", out)
	}
	t.Fatalf("tryStartServer: unexpected error %v; output:\n%s", err, out)
	return -1, out
}

// successStartServer launches the binary in the background with the
// supplied env, waits for its TCP port to be listening, and registers a
// Cleanup that stops it. Use this for positive-path checks that a valid
// env actually boots. Caller must set CHAT_SERVER_PORT in env to match
// `port`.
func successStartServer(t *testing.T, env map[string]string, port int) {
	t.Helper()
	bin := buildServerBinary(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = envSlice(env)
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
}
