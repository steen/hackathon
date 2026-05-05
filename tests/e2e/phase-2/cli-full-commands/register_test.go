package cli_full_commands_e2e_test

import (
	"net/http"
	"strings"
	"testing"

	"hackathon/tests/e2e/internal/clihelp"
)

// AC-1: `chatd register <username>` prompts for password and invite
// code, calls POST /api/auth/register, stores the returned token. Exits
// 0 on success.
//
// The AC has three sub-claims; this file splits them so a regression
// in one (e.g. the prompt path) does not silence the other two.

// TestAC1_Register_PersistsTokenViaFlags drives register through the
// --password / --invite-code flag path so it isolates the
// "calls /api/auth/register and stores the token" half of AC-1 from
// the prompt path. Expected: passes against current production code.
func TestAC1_Register_PersistsTokenViaFlags(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)

	res := chatdRun(t, xdg, "", nil,
		"--server", srv.url, "register",
		"--password", password,
		"--invite-code", srv.inviteCode,
		username,
	)
	if res.exitCode != 0 {
		t.Fatalf("AC-1 flags: exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Registered as "+username) {
		t.Errorf("AC-1: stdout missing confirmation; got %q", res.stdout)
	}

	cf, err := readConfigFile(t, xdg)
	if err != nil {
		t.Fatalf("AC-1: config file unreadable after register: %v", err)
	}
	if cf.Token == "" {
		t.Errorf("AC-1: config token is empty after register; cf=%+v", cf)
	}
	if cf.User == nil || cf.User.Username != username {
		t.Errorf("AC-1: cached user mismatch; got %+v want username=%s", cf.User, username)
	}

	// Belt-and-suspenders: prove the persisted token is one the
	// server actually issued, by hitting /api/auth/me with it.
	if got := meStatus(t, srv, cf.Token); got != http.StatusOK {
		t.Errorf("AC-1: GET /api/auth/me with persisted token = %d; want 200", got)
	}
}

// TestAC1_Register_PromptsForPasswordAndInviteCode drives register
// through the prompt path with stdin pre-stuffed for password and
// invite code.
func TestAC1_Register_PromptsForPasswordAndInviteCode(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	stdin := password + "\n" + srv.inviteCode + "\n"

	res := chatdRun(t, xdg, stdin, nil, "--server", srv.url, "register", username)
	if res.exitCode != 0 {
		t.Fatalf("AC-1 prompts: exit=%d stderr=%q", res.exitCode, res.stderr)
	}
	if _, err := readConfigFile(t, xdg); err != nil {
		t.Errorf("AC-1 prompts: config file unreadable after prompted register: %v", err)
	}
}

// AC-1 (negative): wrong invite code is rejected and no config is
// written. Drives the flag path to keep this test independent of the
// prompt-path code.
func TestAC1_Register_WrongInviteCodeRejected(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)

	res := chatdRun(t, xdg, "", nil,
		"--server", srv.url, "register",
		"--password", password,
		"--invite-code", "wrong-code-xxxxx",
		username,
	)
	if res.exitCode == 0 {
		t.Fatalf("AC-1: register with bad invite code exited 0; stdout=%q stderr=%q", res.stdout, res.stderr)
	}
	if _, err := readConfigFile(t, xdg); err == nil {
		t.Errorf("AC-1: config file written despite failed register")
	}
}
