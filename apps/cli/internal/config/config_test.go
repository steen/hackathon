package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &File{
		Server: "http://example.test",
		Token:  "tok-abc",
		User:   &User{ID: "u1", Username: "alice"},
	}
	if err := Save(dir, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server != in.Server || got.Token != in.Token {
		t.Errorf("round-trip = %+v, want %+v", got, in)
	}
	if got.User == nil || got.User.Username != "alice" {
		t.Errorf("user round-trip = %+v, want alice", got.User)
	}
}

func TestSaveWritesMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permissions not enforced on windows")
	}
	dir := t.TempDir()
	if err := Save(dir, &File{Token: "secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Token != "" || got.Server != "" || got.User != nil {
		t.Errorf("missing-file load = %+v, want empty", got)
	}
}

func TestClearRemovesFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &File{Token: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Errorf("file still exists after Clear: err=%v", err)
	}
	if err := Clear(dir); err != nil {
		t.Errorf("Clear of missing file = %v, want nil", err)
	}
}
