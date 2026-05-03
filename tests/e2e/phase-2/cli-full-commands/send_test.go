package cli_full_commands_e2e_test

import (
	"testing"
)

// AC-6: `chatd send <channel> <message>` posts a message; supports
// stdin input when <message> is "-".
//
// Two sub-tests share boot/login setup but exercise the two input
// paths separately so a single flake (e.g. stdin handling regression)
// shows up as one failure, not two.
func TestAC6_Send_PostsInlineMessage(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channel := randomChannelName(t)
	channelID := createChannelViaREST(t, srv, token, channel)

	body := "hello-from-inline-arg"
	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "send", channelID, body)
	if res.exitCode != 0 {
		t.Fatalf("AC-6 inline: exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
	}

	// Confirm via REST that the message landed.
	got := listMessagesViaREST(t, srv, token, channelID)
	if !contains(got, body) {
		t.Errorf("AC-6 inline: server did not record %q; got=%v", body, got)
	}
}

// AC-6 (stdin half): when message arg is "-", body comes from stdin.
func TestAC6_Send_PostsMessageFromStdinWhenDash(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channel := randomChannelName(t)
	channelID := createChannelViaREST(t, srv, token, channel)

	body := "from-stdin-pipe"
	res := chatdRun(t, xdg, body+"\n", nil, "--server", srv.url, "send", channelID, "-")
	if res.exitCode != 0 {
		t.Fatalf("AC-6 stdin: exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
	}

	got := listMessagesViaREST(t, srv, token, channelID)
	if !contains(got, body) {
		t.Errorf("AC-6 stdin: server did not record %q; got=%v", body, got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
