package cli_full_commands_e2e_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathon/tests/e2e/internal/clihelp"
)

// AC-2: `chatd login` prompts for username and password, stores token
// in $XDG_CONFIG_HOME/chatd/config.json (or platform equivalent).
//
// Two sub-claims; split so a regression in one does not silence the
// other.

// TestAC2_Login_PersistsTokenViaFlagsUnderXDG drives login via
// --username / --password flags and asserts the path + perm contract.
// Expected to pass against current production code.
func TestAC2_Login_PersistsTokenViaFlagsUnderXDG(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)

	res := chatdRun(t, xdg, "", nil,
		"--server", srv.url, "login",
		"--username", username,
		"--password", password,
	)
	if res.exitCode != 0 {
		t.Fatalf("AC-2 flags: exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Logged in as "+username) {
		t.Errorf("AC-2: stdout missing confirmation; got %q", res.stdout)
	}

	// AC-2 specifies the path. Inspect the actual file path on disk.
	wantPath := filepath.Join(xdg, "chatd", "config.json")
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("AC-2: expected config at %s: %v", wantPath, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		// Documented in apps/cli/internal/config/config.go's package
		// comment as a SEC contract; flag if it regresses.
		t.Errorf("AC-2: config file perm = %o; want 0o600", got)
	}

	cf, err := readConfigFile(t, xdg)
	if err != nil {
		t.Fatalf("AC-2: config unreadable: %v", err)
	}
	if cf.Token == "" {
		t.Errorf("AC-2: token field empty after login; cf=%+v", cf)
	}
	if cf.User == nil || cf.User.Username != username {
		t.Errorf("AC-2: cached user mismatch; got %+v want username=%s", cf.User, username)
	}
	if got := meStatus(t, srv, cf.Token); got != http.StatusOK {
		t.Errorf("AC-2: GET /api/auth/me with persisted token = %d; want 200", got)
	}
}

// TestAC2_Login_PromptsForUsernameAndPassword drives login through the
// prompt path with stdin pre-stuffed for username and password.
func TestAC2_Login_PromptsForUsernameAndPassword(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)

	stdin := username + "\n" + password + "\n"
	res := chatdRun(t, xdg, stdin, nil, "--server", srv.url, "login")
	if res.exitCode != 0 {
		t.Fatalf("AC-2 prompts: exit=%d stderr=%q", res.exitCode, res.stderr)
	}
}

// AC-2 (negative): login with the wrong password does not persist a
// token. Driven via flags to keep this assertion independent of the
// prompt-path code.
func TestAC2_Login_WrongPasswordDoesNotPersist(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)

	res := chatdRun(t, xdg, "", nil,
		"--server", srv.url, "login",
		"--username", username,
		"--password", "wrong-password-32-bytes-xxxxxxxx",
	)
	if res.exitCode == 0 {
		t.Fatalf("AC-2: login with wrong password exited 0; stdout=%q stderr=%q", res.stdout, res.stderr)
	}
	if _, err := readConfigFile(t, xdg); err == nil {
		t.Errorf("AC-2: config file written despite failed login")
	}
}
