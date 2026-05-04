// Package smoke_test_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-smoke-test.md. The tests invoke
// scripts/smoke.sh as a subprocess and assert behavior only via
// the script's exit code and stdout/stderr.
//
// AC-5 ("This test stays green for the rest of the project") is a
// meta-property: it is satisfied transitively by TestAC1 below
// running in CI on every commit. No separate AC-5 test exists.
package smoke_test_e2e_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAC1_SmokeScriptBootsServerStartsTwoWatchersSendsMessageBothReceive verifies AC-1:
// "scripts/smoke.sh boots the server, starts two chatd watch processes, sends a message
// via chatd send, and asserts both watchers received it."
//
// Black-box: invoke scripts/smoke.sh from the repo root and assert exit 0 plus the
// success line "[smoke] OK: both watchers received <msg>" on stdout. The script itself
// boots the server, starts two watchers, sends one message, and verifies both watchers
// observed it; a clean exit is the assertion.
func TestAC1_SmokeScriptBootsServerStartsTwoWatchersSendsMessageBothReceive(t *testing.T) {
	root := repoRoot(t)

	for _, tool := range []string{"go", "openssl", "python3", "curl", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %q not on PATH: %v", tool, err)
		}
	}

	script := filepath.Join(root, "scripts", "smoke.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("scripts/smoke.sh not found at %s: %v", script, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("scripts/smoke.sh did not finish within 120s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			stdout.String(), stderr.String())
	}
	if err != nil {
		t.Fatalf("scripts/smoke.sh exited non-zero: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			err, stdout.String(), stderr.String())
	}

	const successPrefix = "[smoke] OK: both watchers received"
	if !strings.Contains(stdout.String(), successPrefix) {
		t.Fatalf("scripts/smoke.sh stdout did not contain %q\n--- stdout ---\n%s\n--- stderr ---\n%s",
			successPrefix, stdout.String(), stderr.String())
	}
}

// repoRoot walks up from this file until it finds go.mod.
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
