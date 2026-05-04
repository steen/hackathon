// AC-1: The SQLite database file is created with mode `0600` (owner
// read/write only).
//
// Source: specs/plans/phase-1/feature-file-perms-and-headers.md.
package file_perms_and_headers_e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAC1_SQLiteFileCreatedWith0600 — "The SQLite database file is
// created with mode `0600` (owner read/write only)." Verbatim from
// specs/plans/phase-1/feature-file-perms-and-headers.md.
//
// Two scenarios in subtests:
//
//  1. fresh boot — CHAT_DB_PATH points at a non-existent file; after
//     the port is listening (proving boot completed and the DB was
//     opened), os.Stat reports mode 0600.
//
//  2. pre-existing too-wide file — seed CHAT_DB_PATH with a 0644 file
//     before boot; after boot, the same file's mode is 0600. Proves
//     EnsureFile tightens existing files, not just freshly-created
//     ones.
func TestAC1_SQLiteFileCreatedWith0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0600 perm bits are unix-specific (perms_unix.go is the production path; perms_windows.go is a no-op)")
	}

	t.Run("fresh_boot_creates_file_at_0600", func(t *testing.T) {
		srv := startServer(t, startServerOpts{})

		info, err := os.Stat(srv.dbPath)
		if err != nil {
			t.Fatalf("stat %q: %v", srv.dbPath, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("sqlite file mode = %#o; want 0600", got)
		}
	})

	t.Run("pre_existing_wide_file_is_tightened_to_0600", func(t *testing.T) {
		// Seed a file at 0644 in a fresh tmpdir. The boot must chmod
		// it to 0600 (per perms_unix.go: belt-and-braces Chmod after
		// the umask-tightened Open).
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "chatd.sqlite")
		f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatalf("seed wide file: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close seeded file: %v", err)
		}
		// os.OpenFile honors the process umask, which on most CI hosts
		// is 0022 — that already produces 0644 from a 0644 request.
		// Force the mode explicitly so the test is independent of
		// umask: a more-restrictive umask (e.g. 0077) would otherwise
		// drop us to 0600 before the server even starts and the
		// assertion would pass for the wrong reason.
		if err := os.Chmod(dbPath, 0o644); err != nil {
			t.Fatalf("chmod seeded file 0644: %v", err)
		}

		srv := startServer(t, startServerOpts{dbPath: dbPath})

		info, err := os.Stat(srv.dbPath)
		if err != nil {
			t.Fatalf("stat %q: %v", srv.dbPath, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("pre-existing wide sqlite file mode after boot = %#o; want 0600", got)
		}
	})
}
