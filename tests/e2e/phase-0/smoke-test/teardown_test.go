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
	"syscall"
	"testing"
	"time"
)

// TestAC3_SmokeScriptTearsDownAllSpawnedProcessesOnCompletion verifies AC-3:
// "The script tears down all spawned processes on completion (success or failure)."
//
// Strategy: launch scripts/smoke.sh in its own POSIX process group (Setpgid)
// so every child it forks (server binary, two `chatd watch` processes, the
// `chatd send` invocation, transient curl/python helpers, the `pick_free_port`
// subshell, etc.) inherits the same pgid. After Wait() returns, poll
// `pgrep -g <pgid>` until either no processes remain in the group or a
// short deadline elapses. Any remaining process is a teardown failure.
//
// The script registers `trap cleanup EXIT INT TERM HUP` and cleanup() does a
// bounded TERM-then-KILL on the watcher and server PIDs it tracks. AC-3 says
// "all spawned processes" — process-group membership is the portable proxy
// for "spawned by this script" without parsing /proc (which doesn't exist on
// macOS) or pid-walking ps output.
//
// Both legs of the AC are exercised:
//   - success leg (clean run, exit 0) — also covered by TestAC1, here
//     re-asserted with the post-exit pgid sweep.
//   - failure leg (forced port conflict, non-zero exit) — also exercised by
//     TestAC2 for exit-code semantics; here we additionally demand the
//     trap fires and cleans up the server it spawned before failing.
func TestAC3_SmokeScriptTearsDownAllSpawnedProcessesOnCompletion(t *testing.T) {
	root := repoRoot(t)

	for _, tool := range []string{"go", "openssl", "python3", "curl", "bash", "pgrep"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %q not on PATH: %v", tool, err)
		}
	}

	script := filepath.Join(root, "scripts", "smoke.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("scripts/smoke.sh not found at %s: %v", script, err)
	}

	t.Run("success_leg_no_leftover_processes", func(t *testing.T) {
		runAndAssertNoLeftovers(t, root, script, nil, true)
	})

	t.Run("failure_leg_no_leftover_processes", func(t *testing.T) {
		// Hold a port so the server's bind fails. `set -e` propagates the
		// non-zero exit through the script; the EXIT trap must still tear
		// down anything the script managed to spawn before that point.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("could not reserve port: %v", err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port

		env := []string{"CHAT_LISTEN_ADDR=127.0.0.1:" + strconv.Itoa(port)}
		runAndAssertNoLeftovers(t, root, script, env, false)
	})
}

// runAndAssertNoLeftovers runs scripts/smoke.sh in its own process group with
// the supplied extra env vars, waits for it to exit, then asserts no
// processes remain in that process group. expectSuccess=true demands exit 0
// (the happy path); expectSuccess=false demands non-zero (the forced
// failure leg). Either way the post-exit pgid sweep is the AC-3 assertion.
func runAndAssertNoLeftovers(t *testing.T, root, script string, extraEnv []string, expectSuccess bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), extraEnv...)
	// New process group so every fork the script makes inherits this pgid.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start scripts/smoke.sh: %v", err)
	}
	pgid := cmd.Process.Pid // Setpgid:true with no Pgid means pgid == pid.

	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		// Best-effort: nuke the whole group so this test doesn't leak procs
		// for the next test in the file.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatalf("scripts/smoke.sh did not finish within 120s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			stdout.String(), stderr.String())
	}

	if expectSuccess {
		if runErr != nil {
			t.Fatalf("scripts/smoke.sh exited non-zero on success leg: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				runErr, stdout.String(), stderr.String())
		}
	} else {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("scripts/smoke.sh failure leg expected non-zero exit, got: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				runErr, stdout.String(), stderr.String())
		}
		if exitErr.ExitCode() == 0 {
			t.Fatalf("scripts/smoke.sh failure leg ExitCode == 0 despite Run() error\n--- stdout ---\n%s\n--- stderr ---\n%s",
				stdout.String(), stderr.String())
		}
	}

	// Poll for up to 10s for `pgrep -g <pgid>` to return empty. Cleanup()
	// in the script does a bounded TERM-then-KILL with up to ~5s per pid,
	// and double-fork helpers (curl/python in subshells) may need an
	// extra reaping tick. 10s is comfortably above that envelope without
	// dragging the test on red.
	deadline := time.Now().Add(10 * time.Second)
	var leftover []string
	for time.Now().Before(deadline) {
		leftover = pgrepGroup(t, pgid)
		if len(leftover) == 0 {
			return // AC-3 satisfied.
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Best-effort cleanup so subsequent tests don't inherit zombies.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	t.Fatalf("scripts/smoke.sh left %d process(es) alive in pgid %d after exit: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
		len(leftover), pgid, leftover, stdout.String(), stderr.String())
}

// pgrepGroup returns the PIDs (as strings, since their identity is what we
// care about, not arithmetic) of every process currently in process group
// pgid. An empty slice means the group is empty.
func pgrepGroup(t *testing.T, pgid int) []string {
	t.Helper()
	out, err := exec.Command("pgrep", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		// pgrep exits 1 when no matches found — that's the AC-3 success
		// signal, not a test error. Any other ExitError or non-ExitError
		// is a real failure.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("pgrep -g %d failed: %v", pgid, err)
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	return pids
}
