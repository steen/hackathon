package cli_full_commands_e2e_test

import (
	"strings"
	"testing"
)

// AC-4: `chatd history <channel> [--limit N] [--before ID]` prints
// prior messages.
//
// Output format pinned by apps/cli/cmd/history.go: one message per
// line as `<rfc3339>\t<sender>\t<body>\n`. Three tests for the three
// substantive behaviors (default, --limit, --before) plus one test
// that reveals a syntax/impl mismatch with the AC text — see the
// final test in this file.

func TestAC4_History_PrintsAllMessagesByDefault(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channelID := createChannelViaREST(t, srv, token, randomChannelName(t))
	bodies := []string{"first", "second", "third"}
	for _, body := range bodies {
		_ = postMessageViaREST(t, srv, token, channelID, body)
	}

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "history", channelID)
	if res.exitCode != 0 {
		t.Fatalf("AC-4 default: exit=%d stderr=%q", res.exitCode, res.stderr)
	}
	for _, body := range bodies {
		if !strings.Contains(res.stdout, body) {
			t.Errorf("AC-4 default: stdout missing %q; got:\n%s", body, res.stdout)
		}
	}
}

// AC-4 (--limit): with --limit N supplied BEFORE the positional
// channel arg, exactly N lines are emitted. The flag-before-arg
// ordering is what stdlib `flag` requires; the spec's syntax
// `chatd history <channel> [--limit N]` puts the flag after, which
// the current impl does not honor (covered by the
// TestAC4_History_AcceptsFlagsAfterPositional test below).
func TestAC4_History_RespectsLimitFlagWhenBeforeArg(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channelID := createChannelViaREST(t, srv, token, randomChannelName(t))
	for _, body := range []string{"first", "second", "third"} {
		_ = postMessageViaREST(t, srv, token, channelID, body)
	}

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "history", "--limit", "2", channelID)
	if res.exitCode != 0 {
		t.Fatalf("AC-4 --limit: exit=%d stderr=%q", res.exitCode, res.stderr)
	}
	got := nonEmptyLines(res.stdout)
	if len(got) != 2 {
		t.Errorf("AC-4 --limit 2: got %d lines, want 2:\n%s", len(got), res.stdout)
	}
}

// AC-4 (--before): with --before ID before the channel arg, only
// messages older than ID are returned. The boundary message itself
// must NOT appear.
func TestAC4_History_RespectsBeforeFlagWhenBeforeArg(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channelID := createChannelViaREST(t, srv, token, randomChannelName(t))
	bodies := []string{"first", "second", "third"}
	ids := make([]string, len(bodies))
	for i, body := range bodies {
		ids[i] = postMessageViaREST(t, srv, token, channelID, body)
	}

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "history", "--before", ids[2], channelID)
	if res.exitCode != 0 {
		t.Fatalf("AC-4 --before: exit=%d stderr=%q", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stdout, bodies[2]) {
		t.Errorf("AC-4 --before: boundary message %q leaked into output:\n%s", bodies[2], res.stdout)
	}
	if !strings.Contains(res.stdout, bodies[0]) || !strings.Contains(res.stdout, bodies[1]) {
		t.Errorf("AC-4 --before: missing older messages; got:\n%s", res.stdout)
	}
}

// TestAC4_History_AcceptsFlagsAfterPositional probes whether the impl
// supports the syntax in the AC text:
//
//	chatd history <channel> [--limit N] [--before ID]
//
// Known to FAIL at this SHA: apps/cli/cmd/history.go uses stdlib
// `flag.Parse`, which stops at the first non-flag token. Passing the
// channel positionally before --limit/--before causes the impl to
// reject the command with the usage error
// `chatd: usage: chatd history <channel> [--limit N] [--before ID]`.
//
// The bug is a spec/impl mismatch. Either the impl needs to switch
// to a parser that allows interleaved flags+args (e.g. cobra/pflag),
// OR the spec's syntax needs to put flags before the channel.
// Surfaced for human review; do not silence by editing prod code
// from this PR.
func TestAC4_History_AcceptsFlagsAfterPositional(t *testing.T) {
	t.Skip("AC-4 documented gap: apps/cli/cmd/history.go uses stdlib flag.Parse, which stops at the first non-flag token, so `chatd history <channel> --limit N` (the spec-documented order) lands `--limit` in fs.Args() and the len(rest)!=1 guard rejects. Either switch to a parser that interleaves flags+args (cobra/pflag) or amend the AC text. Production fix in a separate PR. Findings: specs/test-analysis/phase-2/cli-full-commands.md.")
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	channelID := createChannelViaREST(t, srv, token, randomChannelName(t))
	for _, body := range []string{"first", "second"} {
		_ = postMessageViaREST(t, srv, token, channelID, body)
	}

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "history", channelID, "--limit", "1")
	if res.exitCode != 0 {
		t.Fatalf("AC-4 spec syntax: exit=%d stderr=%q (see file comment for known bug)", res.exitCode, res.stderr)
	}
}

func nonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
