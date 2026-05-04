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
// and cleanup() dumps server.log to stderr. Asserts non-zero exit AND a
// recognizable `[smoke]` error marker (or the cleanup log header) on stderr.
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
	cmd.Env = append(os.Environ(), "CHAT_SERVER_PORT="+strconv.Itoa(port))
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

	// "Clear error message" check: cleanup() dumps `--- server.log ---` to
	// stderr on any non-zero exit, and the script's own failure paths print
	// `[smoke] ` lines. Either is sufficient evidence of a recognizable
	// error message reaching the operator.
	stderrStr := stderr.String()
	stdoutStr := stdout.String()
	combined := stderrStr + stdoutStr
	if !strings.Contains(stderrStr, "--- server.log ---") && !strings.Contains(combined, "[smoke] ") {
		t.Fatalf("scripts/smoke.sh exited non-zero (%d) but stderr lacks a recognizable error marker\n--- stdout ---\n%s\n--- stderr ---\n%s",
			exitErr.ExitCode(), stdoutStr, stderrStr)
	}
}
