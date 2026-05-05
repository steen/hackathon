package db

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// SEC-14: SQLite database file is created with mode 0600.

func TestEnsureFile_CreatesWith0600_SEC14(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not meaningful on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")

	if err := EnsureFile(path); err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != FileMode {
		t.Fatalf("mode: got %o, want %o", got, FileMode)
	}
}

func TestEnsureFile_TightensExistingWiderMode_SEC14(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not meaningful on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")

	if err := os.WriteFile(path, []byte("seed"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := EnsureFile(path); err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != FileMode {
		t.Fatalf("mode: got %o, want %o", got, FileMode)
	}
}

// captureLog redirects the default logger's output to a buffer for the
// duration of fn, then restores it. The default logger is what
// WarnDirMode writes to via log.Printf.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})
	fn()
	return buf.String()
}

// PRD §9: SQLite parent directory should be 0700; warn on wider modes.
func TestWarnDirMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes not meaningful on windows")
	}

	cases := []struct {
		name   string
		mode   os.FileMode
		expect bool
	}{
		{"recommended_0700_no_warn", 0o700, false},
		{"owner_only_with_exec_off_no_warn", 0o600, false},
		{"group_read_warns_0750", 0o750, true},
		{"world_read_warns_0755", 0o755, true},
		{"group_write_warns_0770", 0o770, true},
		{"other_exec_warns_0701", 0o701, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "parent")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.Chmod(dir, tc.mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			out := captureLog(t, func() { WarnDirMode(dir) })
			has := strings.Contains(out, "WARN: SQLite parent directory")
			if has != tc.expect {
				t.Fatalf("mode %#o: warned=%v want=%v; output=%q", tc.mode, has, tc.expect, out)
			}
			if tc.expect && !strings.Contains(out, "recommended 0700") {
				t.Fatalf("warning is missing the 'recommended 0700' guidance; output=%q", out)
			}
		})
	}
}

func TestWarnDirMode_MissingDir_NoWarn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes not meaningful on windows")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	out := captureLog(t, func() { WarnDirMode(missing) })
	if out != "" {
		t.Fatalf("missing dir produced output: %q", out)
	}
}

func TestWarnDirMode_NotADir_NoWarn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes not meaningful on windows")
	}
	path := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	out := captureLog(t, func() { WarnDirMode(path) })
	if strings.Contains(out, "WARN: SQLite parent directory") {
		t.Fatalf("regular file should not produce dir-mode warning; output=%q", out)
	}
}

func TestEnsureFile_IsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not meaningful on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")

	for i := 0; i < 3; i++ {
		if err := EnsureFile(path); err != nil {
			t.Fatalf("EnsureFile #%d: %v", i, err)
		}
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != FileMode {
		t.Fatalf("mode: got %o, want %o", got, FileMode)
	}
}
