package testsupport

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RepoRoot walks parent directories from this source file until it
// finds a go.mod, and returns that directory. Implemented as a walk
// rather than a fixed-count Dir^N because the walk survives any
// future move of either testsupport or the calling test package.
func RepoRoot(t *testing.T) string {
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
