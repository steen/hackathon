package smoke_test_e2e_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAC2_SmokeScriptExitsZeroOnSuccessNonZeroWithClearErrorOnFailure verifies AC-2:
// "The script exits 0 on success, non-zero with a clear error message on failure."
//
// The success leg is covered transitively by TestAC1 (exit 0 on a clean tree).
// This test exercises the failure leg: a forced port conflict makes the server
// fail to bind, downstream `chatd` calls return non-zero, `set -e` propagates,
// and cleanup() dumps server.log to stderr. Asserts non-zero exit AND two
// failure-only markers — the cleanup-branch `--- server.log ---` header on
// stderr, plus the `[smoke] register ` line that pins the failure mode to the
// chatd register HTTP call.
func TestAC2_SmokeScriptExitsZeroOnSuccessNonZeroWithClearErrorOnFailure(t *testing.T) {
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

	// Hold a port for the duration of the run so the server's bind fails.
	// The script's TCP-readiness probe will still connect (to this listener),
	// but the first real HTTP call from chatd will fail and `set -e` will
	// propagate the non-zero exit through the script.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not reserve port: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CHAT_LISTEN_ADDR=127.0.0.1:"+strconv.Itoa(port))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("scripts/smoke.sh did not finish within 120s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			stdout.String(), stderr.String())
	}
	if err == nil {
		t.Fatalf("scripts/smoke.sh exited 0 with port %d held; expected non-zero\n--- stdout ---\n%s\n--- stderr ---\n%s",
			port, stdout.String(), stderr.String())
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("scripts/smoke.sh failed to start (not an ExitError): %v", err)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatalf("scripts/smoke.sh ExitCode == 0 despite Run() returning error: %v", err)
	}

	// "Clear error message" check: assert two failure-only signals so a
	// regression in either the cleanup dump or the failure-mode trigger
	// point is caught. The previous looser check accepted any `[smoke] `
	// line, which also matches success-path logs like
	// `[smoke] building server + chatd...`.
	//
	// 1. `--- server.log ---` on stderr is emitted only inside cleanup()'s
	//    `if [[ $rc -ne 0 ]]` branch, so its presence proves the script
	//    took the failure-leg cleanup path.
	// 2. `[smoke] register ` on combined stdout+stderr identifies the
	//    specific failure mode this test forces: with our pre-bound TCP
	//    listener the script's TCP-readiness probe still connects, the
	//    server's bind silently fails, and the first failing command is
	//    `chatd register` (HTTP to a non-HTTP listener). The script logs
	//    `[smoke] register ${SMOKE_USER}` immediately before that call, so
	//    its presence locates the failure at the expected point. If the
	//    script started failing earlier (e.g. build break or port-open
	//    timeout), this assertion would catch that drift.
	stderrStr := stderr.String()
	stdoutStr := stdout.String()
	combined := stderrStr + stdoutStr
	if !strings.Contains(stderrStr, "--- server.log ---") {
		t.Fatalf("scripts/smoke.sh exited non-zero (%d) but stderr lacks the cleanup-only `--- server.log ---` header\n--- stdout ---\n%s\n--- stderr ---\n%s",
			exitErr.ExitCode(), stdoutStr, stderrStr)
	}
	if !strings.Contains(combined, "[smoke] register ") {
		t.Fatalf("scripts/smoke.sh exited non-zero (%d) but did not log `[smoke] register `; failure mode shifted away from chatd register HTTP failure\n--- stdout ---\n%s\n--- stderr ---\n%s",
			exitErr.ExitCode(), stdoutStr, stderrStr)
	}
}
