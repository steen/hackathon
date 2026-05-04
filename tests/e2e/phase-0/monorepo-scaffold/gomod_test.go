// Package monorepo_scaffold_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-monorepo-scaffold.md. The tests are
// static repo-shape checks — no binary boot required.
package monorepo_scaffold_e2e_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestAC1_MonorepoScaffold_SingleRootGoModHackathonImports asserts AC-1
// from specs/plans/phase-0/feature-monorepo-scaffold.md, verbatim:
//
//	A single root go.mod declares module name hackathon; all Go code under
//	apps/ and packages/ lives in this one module and imports use the form
//	hackathon/<path>.
//
// Three sub-conditions are checked black-box against the on-disk tree:
//  1. Exactly one go.mod sits at the repo root and its module directive
//     is exactly "hackathon" (per CLAUDE.md the name is intentionally
//     unrelated to the repo's hosting URL).
//  2. No nested go.mod exists anywhere under apps/ or packages/.
//  3. Every internal import in *.go files under apps/ and packages/ that
//     looks project-internal uses the hackathon/ prefix; none uses a
//     github.com/... coordinate for internal code.
func TestAC1_MonorepoScaffold_SingleRootGoModHackathonImports(t *testing.T) {
	root := repoRoot(t)

	t.Run("root_go_mod_declares_module_hackathon", func(t *testing.T) {
		modPath := filepath.Join(root, "go.mod")
		f, err := os.Open(modPath)
		if err != nil {
			t.Fatalf("open %s: %v", modPath, err)
		}
		defer f.Close()

		var got string
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			if strings.HasPrefix(line, "module ") {
				got = strings.TrimSpace(strings.TrimPrefix(line, "module"))
				break
			}
		}
		if err := s.Err(); err != nil {
			t.Fatalf("scan %s: %v", modPath, err)
		}
		if got != "hackathon" {
			t.Fatalf("root go.mod module directive = %q, want %q", got, "hackathon")
		}
	})

	t.Run("no_nested_go_mod_under_apps_or_packages", func(t *testing.T) {
		for _, sub := range []string{"apps", "packages"} {
			dir := filepath.Join(root, sub)
			info, err := os.Stat(dir)
			if err != nil {
				if os.IsNotExist(err) {
					// Subtree not present yet — vacuously satisfied.
					continue
				}
				t.Fatalf("stat %s: %v", dir, err)
			}
			if !info.IsDir() {
				t.Fatalf("%s exists but is not a directory", dir)
			}

			err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					name := d.Name()
					// Skip vendored / generated trees that may legitimately ship
					// their own go.mod and are not "Go code under apps/ and
					// packages/" in the AC's sense.
					if name == "node_modules" || name == "vendor" || name == ".git" || name == "dist" {
						return fs.SkipDir
					}
					return nil
				}
				if d.Name() == "go.mod" {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("nested go.mod found at %s; AC-1 forbids per-app/per-package modules", rel)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", dir, err)
			}
		}
	})

	t.Run("internal_imports_use_hackathon_prefix", func(t *testing.T) {
		// Match a single import line, e.g.:
		//   import "hackathon/apps/server/internal/hub"
		//   	"hackathon/apps/server/internal/hub"
		// Captures the import path inside the quotes.
		importRE := regexp.MustCompile(`^\s*(?:import\s+)?"([^"]+)"\s*$`)

		for _, sub := range []string{"apps", "packages"} {
			dir := filepath.Join(root, sub)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				continue
			}

			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					name := d.Name()
					if name == "node_modules" || name == "vendor" || name == ".git" || name == "dist" {
						return fs.SkipDir
					}
					return nil
				}
				if !strings.HasSuffix(d.Name(), ".go") {
					return nil
				}

				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()

				rel, _ := filepath.Rel(root, path)
				inBlock := false
				s := bufio.NewScanner(f)
				s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
				for s.Scan() {
					line := s.Text()
					trim := strings.TrimSpace(line)
					switch {
					case !inBlock && strings.HasPrefix(trim, "import ("):
						inBlock = true
						continue
					case inBlock && trim == ")":
						inBlock = false
						continue
					case !inBlock && !strings.HasPrefix(trim, "import "):
						continue
					}

					m := importRE.FindStringSubmatch(line)
					if m == nil {
						continue
					}
					imp := m[1]

					// Forbid github.com/<user>/Hackathon (any case) — this is
					// the exact mistake CLAUDE.md warns against; it would tie
					// the module to the repo's hosting URL.
					lower := strings.ToLower(imp)
					if strings.HasPrefix(lower, "github.com/") && strings.Contains(lower, "/hackathon") {
						t.Errorf("%s imports %q; internal code must use hackathon/<path>, not a github.com coordinate", rel, imp)
						continue
					}

					// Imports rooted at one of the in-repo top-level dirs
					// without the hackathon/ prefix would break the single-
					// module assumption.
					for _, internalRoot := range []string{"apps/", "packages/"} {
						if strings.HasPrefix(imp, internalRoot) {
							t.Errorf("%s imports %q; internal code must use hackathon/%s prefix", rel, imp, internalRoot)
						}
					}
				}
				return s.Err()
			})
			if err != nil {
				t.Fatalf("walk %s: %v", dir, err)
			}
		}
	})
}

// repoRoot walks up from this file until it finds go.mod. Mirrors the
// helper in tests/server-ws-hub/hub_test.go and tests/e2e/phase-0/
// server-ws-hub/harness_test.go (intentionally copied, not imported —
// each E2E test package keeps its helpers local).
func repoRoot(t *testing.T) string {
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
