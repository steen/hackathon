package scaffold_e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAC1_GoWorkSyncSucceeds_E2E(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("AC-1: go.work not found at repo root %s: %v", root, err)
	}

	cmd := exec.Command("go", "work", "sync")
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("AC-1: `go work sync` failed: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	stderrText := stderr.String()
	for _, fragment := range []string{"no such module", "directory not found"} {
		if strings.Contains(stderrText, fragment) {
			t.Errorf("AC-1: stderr from `go work sync` contains forbidden fragment %q.\nstderr:\n%s",
				fragment, stderrText)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}
