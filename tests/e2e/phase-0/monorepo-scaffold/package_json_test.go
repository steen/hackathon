// Package monorepo_scaffold_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-monorepo-scaffold.md. The tests are
// static repo-shape checks — no binary boot required.
package monorepo_scaffold_e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestAC3_MonorepoScaffold_RootScriptsFanOut asserts AC-3 from
// specs/plans/phase-0/feature-monorepo-scaffold.md, verbatim:
//
//	Root package.json exposes dev, build, and test scripts that fan out
//	to the relevant apps/packages.
//
// The check parses <root>/package.json and verifies:
//  1. .scripts.dev, .scripts.build, .scripts.test all exist as
//     non-empty strings.
//  2. Each of those three commands fans out across workspaces — i.e.
//     the command body invokes pnpm with -r / --recursive, --filter, or
//     --workspace, which are the documented fan-out forms in the pnpm
//     CLI. A bare command body that does not delegate to any workspace
//     fails because it would not exercise apps/packages.
func TestAC3_MonorepoScaffold_RootScriptsFanOut(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "package.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if pkg.Scripts == nil {
		t.Fatalf("%s has no top-level `scripts` object", path)
	}

	// Any one of these forms counts as "fans out":
	//   pnpm -r ...                    (run across every workspace)
	//   pnpm --recursive ...           (long form of -r)
	//   pnpm --filter <pkg> ...        (run in one named workspace)
	//   pnpm --workspace ...           (workspace-scoped invocation)
	// A simple one-shot like `bash scripts/foo.sh` with no pnpm fan-out
	// would fail this — that is intentional, the AC requires fan-out.
	fanOut := regexp.MustCompile(`pnpm\s+(?:-r\b|--recursive\b|--filter\b|--workspace\b)`)

	for _, key := range []string{"dev", "build", "test"} {
		body, ok := pkg.Scripts[key]
		if !ok {
			t.Errorf("%s: scripts.%s is missing", path, key)
			continue
		}
		if body == "" {
			t.Errorf("%s: scripts.%s is empty", path, key)
			continue
		}
		if !fanOut.MatchString(body) {
			t.Errorf("%s: scripts.%s does not fan out across workspaces "+
				"(expected pnpm -r / --recursive / --filter / --workspace); got: %q",
				path, key, body)
		}
	}
}
