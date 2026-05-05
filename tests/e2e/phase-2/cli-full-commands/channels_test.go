package cli_full_commands_e2e_test

import (
	"strings"
	"testing"

	"hackathon/tests/e2e/internal/clihelp"
)

// AC-3: `chatd channels` lists channels.
//
// Format pinned by apps/cli/cmd/channels.go: `<id>\t<name>\n` per
// channel. This test creates two channels via REST, then asserts both
// names appear in chatd channels output.
func TestAC3_Channels_ListsChannels(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)

	// Persist the token via chatd login (flag path — see
	// harness_test.go) so the CLI command picks it up the same way a
	// flag-driven user would.
	clihelp.LoginViaFlags(t, srv.url, xdg, username, password)

	a := clihelp.RandomChannelName(t)
	b := clihelp.RandomChannelName(t)
	_ = createChannelViaREST(t, srv, token, a)
	_ = createChannelViaREST(t, srv, token, b)

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "channels")
	if res.exitCode != 0 {
		t.Fatalf("AC-3: chatd channels exit=%d\nstdout=%q\nstderr=%q", res.exitCode, res.stdout, res.stderr)
	}
	for _, name := range []string{a, b} {
		if !strings.Contains(res.stdout, name) {
			t.Errorf("AC-3: stdout missing channel %q; got:\n%s", name, res.stdout)
		}
	}
}
