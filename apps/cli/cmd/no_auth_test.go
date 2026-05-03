package cmd

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAC_0_4_NoAuthSymbolsReferencedFromCLI(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	// Anchor "token" with surrounding context so this gate doesn't false-
	// positive on benign uses (a `go/token` import for AST work, a comment
	// about a parser token, etc.). Bare "auth" is still an import-only check
	// below; the literals are content checks.
	forbiddenLiterals := []string{"authorization", "bearer ", "auth token", "access token", "bearer token", "x-token"}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(strings.ToLower(ip), "auth") {
				t.Errorf("%s: import %q contains substring 'auth'", path, ip)
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(content))
		for _, lit := range forbiddenLiterals {
			if strings.Contains(lower, lit) {
				t.Errorf("%s: contains forbidden literal %q (case-insensitive)", path, lit)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
}
