package cli_full_commands_e2e_test

import (
	"os/exec"
	"strings"
	"testing"
)

// AC-9: All commands authenticate via the stored token and re-use the
// `packages/go-client` library.
//
// Two halves:
//   - Token-reuse: after register, subsequent commands do NOT re-prompt
//     for credentials. We assert this by running `chatd channels` with
//     stdin = "" — if the binary tried to prompt, readSecret would
//     return an empty value and the command would error with
//     "password is required" or similar. A clean exit with stdout
//     content proves the stored token is being used.
//   - Library re-use: `go list -deps ./apps/cli` lists
//     `hackathon/packages/go-client` as a transitive dependency. This
//     is a build-shape assertion; a CLI rewritten without go-client
//     would not list it.
func TestAC9_TokenReuse_AndGoClientDependency(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)

	// Register via chatd (flag path) so the config file gets written
	// exactly the way a flag-driven user would create it.
	chatdRegisterViaFlags(t, srv, xdg, username, password)

	// Now run a sequence of commands that each require auth, with no
	// stdin. If any of them tried to prompt, the empty stdin would
	// cause them to error.
	for _, args := range [][]string{
		{"--server", srv.url, "channels"},
		{"--server", srv.url, "whoami"},
	} {
		res := chatdRun(t, xdg, "", nil, args...)
		if res.exitCode != 0 {
			t.Errorf("AC-9 token reuse: %v exit=%d stderr=%q (re-prompted or token not reused?)",
				args, res.exitCode, res.stderr)
		}
	}

	// Library re-use: assert the binary actually depends on
	// packages/go-client.
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-deps", "./apps/cli")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("AC-9 deps: go list failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hackathon/packages/go-client") {
		t.Errorf("AC-9 deps: apps/cli does not depend on hackathon/packages/go-client; deps:\n%s", out)
	}
}
