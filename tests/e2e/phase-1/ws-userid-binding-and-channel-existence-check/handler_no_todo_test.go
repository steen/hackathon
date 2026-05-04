package ws_userid_binding_and_channel_existence_check_e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAC4_TODOAtHandlerLine148IsRemoved asserts the AC-4 statement verbatim:
//
//	"The TODO at apps/server/internal/wsapi/handler.go:148 (`_ = userID`) is removed."
//
// Source-code hygiene claim — proved by reading the file and asserting the
// `_ = userID` discard substring is gone, plus that no TODO comment in the
// file still mentions discarding userID. Line numbers may have shifted since
// the spec was written; the durable check is the substring match.
func TestAC4_TODOAtHandlerLine148IsRemoved(t *testing.T) {
	root := repoRootForAC4(t)
	handlerPath := filepath.Join(root, "apps", "server", "internal", "wsapi", "handler.go")

	content, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatalf("AC-4: read %s: %v", handlerPath, err)
	}

	src := string(content)

	if strings.Contains(src, "_ = userID") {
		t.Errorf("AC-4: %s still contains `_ = userID` discard; the TODO must be removed", handlerPath)
	}

	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "todo") && strings.Contains(lower, "userid") {
			t.Errorf("AC-4: %s still has a TODO comment referencing userID: %q", handlerPath, trimmed)
		}
	}
}

// repoRootForAC4 walks up from this file
// (<root>/tests/e2e/phase-1/ws-userid-binding-and-channel-existence-check/handler_no_todo_test.go)
// to the repo root (5 Dir() calls). Sanity-checked by stat-ing go.mod.
func repoRootForAC4(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("AC-4: runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("AC-4: expected go.mod at %s: %v", root, err)
	}
	return root
}
