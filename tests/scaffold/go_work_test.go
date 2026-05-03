package scaffold_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAC1_GoWorkDeclaresExpectedModules(t *testing.T) {
	root := repoRoot(t)
	goWorkPath := filepath.Join(root, "go.work")

	content, err := os.ReadFile(goWorkPath)
	if err != nil {
		t.Fatalf("AC-1: %s does not exist or cannot be read: %v", goWorkPath, err)
	}

	text := string(content)

	goDirective := regexp.MustCompile(`(?m)^go\s+\d+\.\d+(\.\d+)?\s*$`)
	if !goDirective.MatchString(text) {
		t.Errorf("AC-1: go.work missing `go <version>` directive; got:\n%s", text)
	}

	uses := parseUseEntries(text)
	for _, want := range []string{"./apps/server", "./apps/cli"} {
		if !contains(uses, want) {
			t.Errorf("AC-1: go.work `use` block missing entry %q; got entries: %v", want, uses)
		}
	}
}

func parseUseEntries(text string) []string {
	var entries []string

	blockRe := regexp.MustCompile(`(?s)use\s*\(\s*(.*?)\s*\)`)
	for _, m := range blockRe.FindAllStringSubmatch(text, -1) {
		for _, line := range strings.Split(m[1], "\n") {
			line = strings.TrimSpace(stripLineComment(line))
			if line != "" {
				entries = append(entries, line)
			}
		}
	}

	singleRe := regexp.MustCompile(`(?m)^use[ \t]+([^\s(][^\n]*?)\s*(?://.*)?$`)
	for _, m := range singleRe.FindAllStringSubmatch(text, -1) {
		entries = append(entries, strings.TrimSpace(m[1]))
	}
	return entries
}

func stripLineComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
