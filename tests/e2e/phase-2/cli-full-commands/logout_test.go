package cli_full_commands_e2e_test

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"testing"
)

// AC-8: `chatd logout` clears the stored token from the config file
// AND calls server POST /api/auth/logout to invalidate the token
// server-side. Exits 0 on success.
//
// The server-side half is the load-bearing one — without it, a stolen
// token outlives the user's logout. We capture the token from the
// config file pre-logout and assert GET /api/auth/me with that token
// returns 401 post-logout.
func TestAC8_Logout_ClearsConfigAndInvalidatesServerSide(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)

	// Login via chatd (flag path) so the config file matches a real
	// flag-driven flow. The prompt-path bug surfaces in AC-2's own
	// test, not here.
	chatdLoginViaFlags(t, srv, xdg, username, password)
	pre, err := readConfigFile(t, xdg)
	if err != nil {
		t.Fatalf("setup: read config: %v", err)
	}
	capturedToken := pre.Token
	if capturedToken == "" {
		t.Fatalf("setup: empty token in config")
	}
	// Sanity: the captured token is currently valid.
	if got := meStatus(t, srv, capturedToken); got != http.StatusOK {
		t.Fatalf("setup: pre-logout /me with captured token = %d; want 200", got)
	}

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "logout")
	if res.err != nil {
		t.Fatalf("chatd logout exec error: %v", res.err)
	}
	if res.exitCode != 0 {
		t.Fatalf("AC-8: logout exit=%d\nstdout=%q\nstderr=%q", res.exitCode, res.stdout, res.stderr)
	}

	// Local clear: the config file is removed (or has no token). The
	// impl in apps/cli/internal/config/config.go::Clear removes the
	// file outright; assert that and fall back to "token cleared" if
	// the impl ever changes to a soft clear.
	cf, err := readConfigFile(t, xdg)
	switch {
	case err == nil:
		if cf.Token != "" {
			t.Errorf("AC-8: token still set after logout; cf=%+v", cf)
		}
	case errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err):
		// Common case: file removed entirely.
	default:
		t.Errorf("AC-8: unexpected error reading config after logout: %v", err)
	}

	// Server-side invalidation — the captured token must no longer be
	// honored. This is the half that proves chatd actually called
	// POST /api/auth/logout, not just removed the local file.
	if got := meStatus(t, srv, capturedToken); got == http.StatusOK {
		t.Errorf("AC-8: post-logout /me with captured token = 200; expected 401 (server did not invalidate)")
	}
}
