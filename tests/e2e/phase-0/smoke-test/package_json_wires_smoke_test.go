package smoke_test_e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC4_SmokeScriptIsWiredIntoRootPackageJsonTestScript verifies AC-4:
// "The script is referenced by the root package.json test script (or an
// equivalent task) so it runs as part of the standard test workflow."
//
// Static check: decode <repoRoot>/package.json and assert that
// .scripts.test references scripts/smoke.sh. The current value is
// "bash scripts/smoke.sh && pnpm -r --if-present run test"; any future
// rewording is fine as long as the smoke script remains in the test
// pipeline.
func TestAC4_SmokeScriptIsWiredIntoRootPackageJsonTestScript(t *testing.T) {
	root := repoRoot(t)
	pkgPath := filepath.Join(root, "package.json")

	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read %s: %v", pkgPath, err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("decode %s: %v", pkgPath, err)
	}

	testScript, ok := pkg.Scripts["test"]
	if !ok {
		t.Fatalf("%s has no .scripts.test entry; AC-4 requires the smoke script to run as part of the standard test workflow\n--- raw ---\n%s",
			pkgPath, raw)
	}

	const needle = "scripts/smoke.sh"
	if !strings.Contains(testScript, needle) {
		t.Fatalf("%s .scripts.test = %q does not reference %q\nAC-4: scripts/smoke.sh must be wired into the root test script.",
			pkgPath, testScript, needle)
	}
}
