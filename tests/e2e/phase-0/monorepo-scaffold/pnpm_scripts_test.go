// Package monorepo_scaffold_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-monorepo-scaffold.md.
package monorepo_scaffold_e2e_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAC4_AC5_MonorepoScaffold_PnpmInstallAndScripts asserts AC-4 and AC-5
// from specs/plans/phase-0/feature-monorepo-scaffold.md, verbatim:
//
//	AC-4: Running `pnpm install` from a clean clone succeeds without errors.
//	AC-5: Running each top-level script (`dev`, `build`, `test`) completes
//	      without configuration errors (script bodies may be stubs at this
//	      stage).
//
// Strategy: `git clone --local` the current repo into t.TempDir() so we run
// against committed content only — no working-tree leakage from the live
// checkout (uncommitted edits, build artifacts, node_modules). Then drive
// pnpm in that clone:
//
//  1. `pnpm install --frozen-lockfile` — exit 0 (AC-4).
//  2. `pnpm run build` — exit 0 (AC-5 build).
//  3. `pnpm run dev` — long-running watcher; SIGTERM after a short wait,
//     accept exit 0 or signal-terminated (AC-5 dev). Vite-style watchers
//     that emitted a config error would exit non-zero before the SIGTERM
//     window, which is the negative signal the AC excludes.
//  4. `pnpm run test` — exit 0 (AC-5 test). The root `test` script invokes
//     scripts/smoke.sh, which builds and runs the Go server; if `go` is
//     not on PATH this subtest is skipped rather than failed.
//
// Skips: `git`, `pnpm` not on PATH skips the whole test; `go` not on PATH
// skips only the `pnpm run test` subtest.
func TestAC4_AC5_MonorepoScaffold_PnpmInstallAndScripts(t *testing.T) {
	clone := setupCleanClone(t)

	t.Run("ac4_pnpm_install_frozen_lockfile", func(t *testing.T) {
		runPnpm(t, clone, 5*time.Minute, "install", "--frozen-lockfile")
	})

	t.Run("ac5_build", func(t *testing.T) {
		runPnpm(t, clone, 5*time.Minute, "run", "build")
	})

	t.Run("ac5_dev_starts_then_sigterm", func(t *testing.T) {
		runLongRunningPnpm(t, clone, "run", "dev")
	})

	t.Run("ac5_test", func(t *testing.T) {
		// Root `test` script chains scripts/smoke.sh, which compiles the
		// Go server. Without `go` it would fail for a reason unrelated
		// to AC-5.
		if _, err := exec.LookPath("go"); err != nil {
			t.Skipf("go not on PATH (required by scripts/smoke.sh): %v", err)
		}
		runPnpm(t, clone, 5*time.Minute, "run", "test")
	})
}

// setupCleanClone produces a fresh `git clone --local` of the live repo
// inside t.TempDir(). It skips the entire test when `git` or `pnpm` are
// missing so the test is non-fatal on bare runners.
func setupCleanClone(t *testing.T) string {
	t.Helper()

	for _, tool := range []string{"git", "pnpm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %q not on PATH: %v", tool, err)
		}
	}

	root := repoRoot(t)
	dst := filepath.Join(t.TempDir(), "clone")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", "--local", root, dst)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git clone --local %s %s: %v\n--- stderr ---\n%s", root, dst, err, stderr.String())
	}
	return dst
}

// runPnpm runs `pnpm <args...>` in dir with the supplied timeout and
// fails the test on non-zero exit. The subprocess is given its own
// process group so any orphaned children pnpm forks (build watchers,
// test runners) can be reaped via the group on test failure.
func runPnpm(t *testing.T, dir string, timeout time.Duration, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pnpm", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pnpm %s: %v", strings.Join(args, " "), err)
	}
	pgid := cmd.Process.Pid

	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatalf("pnpm %s exceeded %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			strings.Join(args, " "), timeout, stdout.String(), stderr.String())
	}
	if runErr != nil {
		t.Fatalf("pnpm %s exited non-zero: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			strings.Join(args, " "), runErr, stdout.String(), stderr.String())
	}
}

// runLongRunningPnpm starts `pnpm <args...>` in dir, gives it a short
// budget to emit any config-error exit, then sends SIGTERM to its
// process group and accepts either a clean exit or signal-terminated.
// The AC requires "without configuration errors": a watcher that spat a
// missing-script or unresolvable-config error would exit non-zero
// before the SIGTERM window.
func runLongRunningPnpm(t *testing.T, dir string, args ...string) {
	t.Helper()

	const startupBudget = 5 * time.Second
	const shutdownBudget = 15 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), startupBudget+shutdownBudget+10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pnpm", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pnpm %s: %v", strings.Join(args, " "), err)
	}
	pgid := cmd.Process.Pid

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case runErr := <-exited:
		// The watcher exited on its own before the startup budget. For
		// a long-running script that is the config-error signature the
		// AC excludes — non-zero exit means a configuration error,
		// zero exit before SIGTERM means the script body is a no-op
		// stub which the spec explicitly allows.
		if runErr != nil {
			t.Fatalf("pnpm %s exited before SIGTERM with error: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				strings.Join(args, " "), runErr, stdout.String(), stderr.String())
		}
		return
	case <-time.After(startupBudget):
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatalf("SIGTERM pgid %d: %v", pgid, err)
	}

	select {
	case runErr := <-exited:
		if runErr == nil {
			return
		}
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("pnpm %s post-SIGTERM Wait error: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				strings.Join(args, " "), runErr, stdout.String(), stderr.String())
		}
		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			t.Fatalf("pnpm %s: unexpected WaitStatus type %T", strings.Join(args, " "), exitErr.Sys())
		}
		// Accept signal-terminated (the SIGTERM we sent or SIGKILL via
		// the cascade) or any non-zero exit code attributable to the
		// signal — pnpm forwards child exit codes, so 143 (128+SIGTERM)
		// or 130 (128+SIGINT) are both legitimate "shut down on
		// signal" outcomes.
		if ws.Signaled() {
			return
		}
		code := ws.ExitStatus()
		if code == 143 || code == 130 || code == 137 {
			return
		}
		t.Fatalf("pnpm %s exited %d after SIGTERM (want 0 or signal): %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			strings.Join(args, " "), code, runErr, stdout.String(), stderr.String())
	case <-time.After(shutdownBudget):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-exited
		t.Fatalf("pnpm %s did not exit within %s of SIGTERM (pgid %d)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			strings.Join(args, " "), shutdownBudget, pgid, stdout.String(), stderr.String())
	}
}
