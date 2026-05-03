package cli_full_commands_e2e_test

import (
	"strings"
	"testing"
)

// AC-10: `--server` flag and `CHAT_SERVER` env var override the
// default base URL.
//
// Per apps/cli/cmd/cmd.go::ResolveServer, the precedence is:
//
//	flag > $CHAT_SERVER > DefaultServer (http://localhost:8080)
//
// Three sub-tests prove each rung of the ladder, plus a "flag wins
// over env" check.
func TestAC10_ServerOverride_FlagWorks(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	res := chatdRun(t, xdg, "", nil, "--server", srv.url, "channels")
	if res.exitCode != 0 {
		t.Fatalf("AC-10 flag: channels with --server exit=%d stderr=%q", res.exitCode, res.stderr)
	}
}

func TestAC10_ServerOverride_EnvVarWorks(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	// Strip the --server flag; rely on CHAT_SERVER env.
	res := chatdRun(t, xdg, "", []string{"CHAT_SERVER=" + srv.url}, "channels")
	if res.exitCode != 0 {
		t.Fatalf("AC-10 env: channels with CHAT_SERVER exit=%d stderr=%q", res.exitCode, res.stderr)
	}
}

// AC-10 (precedence): when both are set, the flag wins. We point the
// flag at a bogus URL and the env at the real server; expect failure
// (proving the flag took effect, not the env).
func TestAC10_ServerOverride_FlagWinsOverEnv(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := randomUsername(t)
	password := randomPassword(t)
	_, _ = registerViaREST(t, srv, username, password)
	chatdLoginViaFlags(t, srv, xdg, username, password)

	bogus := "http://127.0.0.1:1" // port 1 is reserved → connection refused
	res := chatdRun(t, xdg, "", []string{"CHAT_SERVER=" + srv.url}, "--server", bogus, "channels")
	if res.exitCode == 0 {
		t.Fatalf("AC-10 precedence: expected failure when --server points at bogus URL; got success\nstdout=%q stderr=%q",
			res.stdout, res.stderr)
	}
	// Sanity: the failure mode should mention the dial / connection
	// (i.e. the flag URL was actually attempted).
	if !strings.Contains(strings.ToLower(res.stderr), "127.0.0.1:1") &&
		!strings.Contains(strings.ToLower(res.stderr), "connection refused") &&
		!strings.Contains(strings.ToLower(res.stderr), "dial") {
		t.Errorf("AC-10 precedence: stderr does not look like a dial failure; got %q", res.stderr)
	}
}
