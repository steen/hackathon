package scaffold_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestAC1_RootGoModuleIsSingleAndNamedHackathon verifies the repo uses a
// single root go.mod with module name "hackathon" (no go.work, no per-app
// go.mod). This keeps the module path independent of the GitHub coordinate.
func TestAC1_RootGoModuleIsSingleAndNamedHackathon(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
		t.Fatalf("AC-1: go.work must NOT exist; the repo uses a single root go.mod")
	}

	for _, p := range []string{
		"apps/cli/go.mod",
		"apps/server/go.mod",
		"tests/scaffold/go.mod",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			t.Errorf("AC-1: per-app go.mod must NOT exist at %s under single-module layout", p)
		}
	}

	rootMod := filepath.Join(root, "go.mod")
	content, err := os.ReadFile(rootMod)
	if err != nil {
		t.Fatalf("AC-1: root go.mod missing: %v", err)
	}

	moduleLine := regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)
	m := moduleLine.FindStringSubmatch(string(content))
	if m == nil {
		t.Fatalf("AC-1: root go.mod missing `module <name>` directive; got:\n%s", content)
	}
	if got := m[1]; got != "hackathon" {
		t.Errorf("AC-1: root module name = %q, want %q (no GitHub coordinate)", got, "hackathon")
	}

	if !strings.Contains(string(content), "go ") {
		t.Errorf("AC-1: root go.mod missing `go <version>` directive; got:\n%s", content)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("AC-1: cannot determine caller file")
	}
	// this file lives at <root>/tests/scaffold/go_module_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
