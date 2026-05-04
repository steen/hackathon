package auth_endpoints_e2e_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// AC-7: scripts/smoke.sh continues to exit 0 after this feature lands.
//
// smoke.sh manages its own ports + secrets; we just exec it from the
// repo root and demand exit 0. Combined output is logged on failure so
// CI gives us the bash trace.
func TestAC7_SmokeScriptExitsZero(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "smoke.sh")

	// Generous ceiling — smoke.sh builds binaries, runs CLI roundtrips,
	// and tears down. 90s is comfortably above the typical 30-40s seen
	// in CI without dragging the test loop on red.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Run(); err != nil {
		t.Fatalf("scripts/smoke.sh: %v\noutput:\n%s", err, combined.String())
	}
}
