package cmd_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cliSourceRoot resolves the path to apps/cli relative to this test file's
// working directory (which is the package directory at test time).
func cliSourceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, ".."))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	return root
}

func TestUS8_CLISourceContainsNoAuthOrTokenSymbols(t *testing.T) {
	root := cliSourceRoot(t)

	bannedIdents := []string{
		"Login",
		"Logout",
		"Register",
		"Token",
		"Bearer",
		"JWT",
		"bcrypt",
		"password",
	}
	identRes := make([]*regexp.Regexp, len(bannedIdents))
	for i, ident := range bannedIdents {
		identRes[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(ident) + `\b`)
	}

	bannedImports := []string{
		"apps/server/internal/auth",
		"packages/go-client",
	}

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
		// The static-absence check itself names the banned identifiers — skip it
		// so the test does not flag its own source.
		if filepath.Base(path) == "no_auth_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, re := range identRes {
			if re.Match(data) {
				t.Errorf("%s: banned identifier %q found", path, bannedIdents[i])
			}
		}
		for _, imp := range bannedImports {
			if strings.Contains(string(data), imp) {
				t.Errorf("%s: banned import path %q found", path, imp)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
}
