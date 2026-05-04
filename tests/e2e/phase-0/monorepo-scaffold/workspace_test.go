// Package monorepo_scaffold_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-monorepo-scaffold.md. The tests are
// static repo-shape checks — no binary boot required.
package monorepo_scaffold_e2e_test

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC2_MonorepoScaffold_PnpmWorkspaceDeclaration asserts AC-2 from
// specs/plans/phase-0/feature-monorepo-scaffold.md, verbatim:
//
//	pnpm-workspace.yaml declares apps/* and packages/* workspaces.
//
// The check parses the YAML's top-level `packages:` list (line-based
// scan; the file is a flat list of quoted strings) and verifies both
// `apps/*` and `packages/*` are present as entries — or are covered by
// a broader glob entry such as `*` / `**` / `apps/**` / `packages/**`.
func TestAC2_MonorepoScaffold_PnpmWorkspaceDeclaration(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "pnpm-workspace.yaml")

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	entries, err := parsePnpmWorkspacePackages(f)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s has no top-level `packages:` list entries", path)
	}

	for _, want := range []string{"apps/*", "packages/*"} {
		if !covers(entries, want) {
			t.Errorf("%s `packages:` list does not cover %q (entries: %v)", path, want, entries)
		}
	}
}

// parsePnpmWorkspacePackages extracts the list-string entries that sit
// directly under the top-level `packages:` key of a pnpm-workspace.yaml.
// It is intentionally narrow: it walks line by line, enters list mode
// when it sees `packages:` at column 0, and exits when a non-indented
// non-blank line appears. Any non-list content under `packages:` (e.g.
// a mapping) returns an error so the test surfaces unexpected shape
// instead of silently passing.
func parsePnpmWorkspacePackages(r io.Reader) ([]string, error) {
	s := bufio.NewScanner(r)
	var (
		entries []string
		inList  bool
	)
	for s.Scan() {
		raw := s.Text()
		// Strip trailing inline comments — pnpm-workspace.yaml may have
		// a trailing `#` comment on a list entry; YAML allows it.
		// Limitation: this is a naive substring search and does not
		// recognize `#` inside quoted entries (e.g. `- "apps/#weird"`
		// would be truncated at the `#`). pnpm workspace globs do not
		// realistically contain `#`, so this is accepted; a quote-aware
		// scan or a real YAML parser would be more cost than the bug
		// warrants.
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = raw[:i]
		}
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}

		// A non-indented line ends the `packages:` list.
		if inList && !startsWithSpace(raw) {
			inList = false
		}

		if !inList {
			if trim == "packages:" {
				inList = true
			}
			continue
		}

		// In-list line. It must be `- "<entry>"` / `- '<entry>'` /
		// `- <entry>`.
		if !strings.HasPrefix(trim, "-") {
			// A mapping under `packages:` is not the shape we expect.
			return nil, &parseError{line: raw}
		}
		val := strings.TrimSpace(strings.TrimPrefix(trim, "-"))
		val = strings.Trim(val, `"'`)
		if val == "" {
			continue
		}
		entries = append(entries, val)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type parseError struct{ line string }

func (e *parseError) Error() string {
	return "unexpected non-list content under `packages:` — " + e.line
}

func startsWithSpace(s string) bool {
	if s == "" {
		return false
	}
	return s[0] == ' ' || s[0] == '\t'
}

// covers reports whether any entry in the list satisfies want. An
// exact string match counts. A broader glob counts when want's
// directory prefix is a prefix of the entry and the entry's tail is a
// glob (`*` or `**`). Examples that cover `apps/*`: `apps/*`,
// `apps/**`, `*`, `**`.
func covers(entries []string, want string) bool {
	wantDir, _ := splitGlob(want)
	for _, e := range entries {
		if e == want {
			return true
		}
		eDir, eTail := splitGlob(e)
		if eTail == "" {
			continue
		}
		if eDir == "" || strings.HasPrefix(wantDir, eDir) {
			return true
		}
	}
	return false
}

// splitGlob splits an entry into a directory prefix and a glob tail
// (`*` or `**`). For `apps/*` it returns (`apps/`, `*`). For an entry
// with no glob it returns (entry, "").
func splitGlob(entry string) (string, string) {
	for _, tail := range []string{"/**", "/*", "**", "*"} {
		if strings.HasSuffix(entry, tail) {
			prefix := strings.TrimSuffix(entry, tail)
			prefix = strings.TrimSuffix(prefix, "/")
			if prefix != "" {
				prefix += "/"
			}
			return prefix, tail
		}
	}
	return entry, ""
}
