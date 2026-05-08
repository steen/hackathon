// AC-3: "`db.EnsureFile(path)` is invoked from `apps/server/main.go` at
// startup before any code opens the SQLite file. The path comes from the
// same env var (`CHAT_DB_PATH`, PRD §9 default `./chat.db`) that the
// future SQLite open will read."
//
// Source: specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md.
package security_headers_e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestAC3_EnsureFileInvokedBeforeSQLiteOpen — verbatim AC-3.
//
// EnsureFile is an internal helper; black-box tests cannot observe the
// call directly. The visible signals that EnsureFile ran from main and
// ran before the SQLite open are:
//
//  1. After main boots against a fresh CHAT_DB_PATH, the file exists at
//     mode 0600 (proves EnsureFile created/tightened it).
//  2. With a pre-existing CHAT_DB_PATH file at mode 0644, after main
//     boots the same file is at mode 0600 (proves EnsureFile is invoked
//     even when the file pre-exists, not only on first creation —
//     a no-op startup that skipped EnsureFile and went straight to
//     sql.Open would leave the 0644 mode untouched).
//  3. A register round-trip succeeds against the same CHAT_DB_PATH —
//     proves the SQLite open + migrate completed against the file whose
//     mode was set by EnsureFile, i.e. EnsureFile preceded the SQL open
//     (had the order been reversed, the driver would have created the
//     file at the umask-default mode and (1) would fail).
//
// The startServer harness already requires the port to be listening
// before returning, which only happens after openAndMigrate succeeds in
// run() — so reaching the assertion body proves the boot sequence
// completed past the EnsureFile + sql.Open + migrate steps.
func TestAC3_EnsureFileInvokedBeforeSQLiteOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0600 perm bits are unix-specific (perms_windows.go is a no-op stub)")
	}

	t.Run("fresh_boot_creates_chat_db_path_at_0600", func(t *testing.T) {
		srv := startServer(t)

		info, err := os.Stat(srv.dbPath)
		if err != nil {
			t.Fatalf("stat CHAT_DB_PATH %q: %v", srv.dbPath, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("CHAT_DB_PATH mode after fresh boot = %#o; want 0600 (EnsureFile must run from main before SQLite open)", got)
		}
	})

	t.Run("pre_existing_0644_file_is_tightened_to_0600", func(t *testing.T) {
		// Seed a 0644 file at a known path, boot a server pointed at
		// that path, and verify the file's mode is 0600 after the
		// port begins listening. startServer in harness_test.go owns
		// CHAT_DB_PATH (it does not accept an injected path); to keep
		// the footprint to a single new file, this sub-test runs its
		// own boot using only the helpers that are exported via
		// harness_test.go.
		root := repoRoot(t)
		tmpDir := t.TempDir()
		binPath := filepath.Join(tmpDir, "chat-server")

		build := exec.Command("go", "build", "-o", binPath, "./apps/server")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
		}

		dbPath := filepath.Join(tmpDir, "chatd.sqlite")
		f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatalf("seed wide file: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close seeded file: %v", err)
		}
		// Force 0644 explicitly — a more-restrictive process umask
		// (e.g. 0077) on the test host would otherwise drop the seed
		// file to 0600 before boot, and the assertion would pass for
		// the wrong reason.
		if err := os.Chmod(dbPath, 0o644); err != nil {
			t.Fatalf("chmod seeded file 0644: %v", err)
		}

		port := freePort(t)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, binPath)
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
			"CHAT_JWT_SECRET="+randomSecret(t, 32),
			"CHAT_INVITE_CODE="+randomSecret(t, 8),
			"CHAT_DB_PATH="+dbPath,
		)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start server: %v", err)
		}
		wait := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(wait)
		}()
		t.Cleanup(func() {
			cancel()
			<-wait
		})

		if err := waitForPort(port, 10*time.Second); err != nil {
			t.Fatalf("server did not listen on :%d: %v", port, err)
		}

		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("stat seeded db %q: %v", dbPath, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("pre-existing 0644 CHAT_DB_PATH mode after boot = %#o; want 0600 (EnsureFile must tighten existing files, not just freshly-created ones)", got)
		}
	})

	t.Run("sqlite_open_succeeded_against_same_path", func(t *testing.T) {
		// register round-trip exercises the users table, which only
		// exists if openAndMigrate ran. The harness blocks until the
		// port listens, which only happens after openAndMigrate
		// returns nil. Together these prove EnsureFile (called inside
		// db.Open per apps/server/internal/db/open.go:29) ran before
		// the SQL driver opened the file — otherwise main would have
		// returned the EnsureFile error and never reached
		// srv.ListenAndServe.
		srv := startServer(t)

		username := "ac3user"
		password := randomSecret(t, 12)
		_ = register(t, srv, username, password)

		// Re-stat CHAT_DB_PATH after the register round-trip: the
		// driver has now opened, written, and (via WAL) flushed. The
		// mode must still be 0600 — proves the umask tightened by
		// EnsureFile was the one in effect when the SQL driver first
		// touched the file. A widened mode here would indicate the
		// driver opened the file before EnsureFile ran.
		info, err := os.Stat(srv.dbPath)
		if err != nil {
			t.Fatalf("stat CHAT_DB_PATH %q after register: %v", srv.dbPath, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("CHAT_DB_PATH mode after register = %#o; want 0600 (driver opened the file before EnsureFile ran)", got)
		}
	})
}
