package cli_full_commands_e2e_test

import (
	"strings"
	"testing"
)

// AC-7: `chatd whoami` prints the current authenticated username (or
// exits non-zero with a clear message if not logged in).
func TestAC7_Whoami_PrintsUsernameWhenLoggedIn(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)

	// Log in via chatd (flag path — see harness_test.go) so the
	// config file is created exactly the way a flag-driven user
	// would create it.
	chatdLoginViaFlags(t, srv, xdg, username, password)

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "whoami")
	if res.exitCode != 0 {
		t.Fatalf("AC-7: whoami exit=%d\nstdout=%q\nstderr=%q", res.exitCode, res.stdout, res.stderr)
	}
	got := strings.TrimSpace(res.stdout)
	if got != username {
		t.Errorf("AC-7: whoami stdout = %q; want %q", got, username)
	}
}

// AC-7 (negative): with no config file, whoami exits non-zero and the
// user gets a "not logged in" message they can act on (the spec calls
// this "a clear message").
func TestAC7_Whoami_ExitsNonZeroWhenLoggedOut(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir() // empty — no chatd/config.json under it

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "whoami")
	if res.exitCode == 0 {
		t.Fatalf("AC-7: whoami when logged out exited 0; stdout=%q stderr=%q", res.stdout, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stderr), "not logged in") {
		t.Errorf("AC-7: stderr missing 'not logged in'; got %q", res.stderr)
	}
}
