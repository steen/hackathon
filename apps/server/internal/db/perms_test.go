package db

import (
	"os"
	"path/filepath"
	"runtime"
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
