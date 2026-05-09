// Package cli_dms_e2e_test exercises the Phase 9 CLI surface end-to-end:
// `chatd dm list/send/history/read/watch` and `chatd channels read`.
// Decision-log anchors: §3 listing hides empty conversations, §4/§8 the
// {type:"dm"} envelope is fanned to both viewers' user:<viewer> topics,
// §11 dm_reads is recipient-driven, L5 advance-only mark-read, L25 CLI
// command shape.
//
// Black-box harness: boots the production chat-server binary via
// testsupport.StartServer and execs the production chatd binary via
// clihelp.BuildChatd, asserting on stdout/stderr/exit-code.
package cli_dms_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/clihelp"
	"hackathon/tests/e2e/internal/testsupport"
)

const (
	// chatd CLI bodies cap at 4096 (decision-log L16) — every test
	// uses ASCII bodies well under that.
	smallBody = "hello-from-alice"
)

// chatdRun execs the production chatd binary with the given args, an
// isolated XDG_CONFIG_HOME, and the supplied stdin/extra-env. Returns
// stdout, stderr, exit code, and any process-launch error. Mirrors the
// shape used by tests/e2e/phase-2/cli-full-commands so a reader who
// already knows that harness gets no surprises here.
func chatdRun(t *testing.T, xdgDir, stdin string, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	bin := clihelp.BuildChatd(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// G204: bin is the per-process build-cache path, args are test
	// fixtures from this package. Loopback-only.
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // see comment
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir,
		"HOME="+xdgDir,
		"CHATD_CONFIG_DIR=",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec chatd %v: %v", args, err)
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// loginViaFlags persists a JWT under xdgDir using the chatd login flag
// path so subsequent invocations pick up the bearer the same way a
// human user would.
func loginViaFlags(t *testing.T, srv *testsupport.Server, xdg, username, password string) {
	t.Helper()
	clihelp.LoginViaFlags(t, srv.HTTPURL, xdg, username, password)
}

// registerAndLogin creates a fresh user via REST and stores a token
// under xdg. Returns (userID, token) for harness paths that bypass the
// CLI entirely.
func registerAndLogin(t *testing.T, srv *testsupport.Server) (xdg, username, password, userID, token string) {
	t.Helper()
	xdg = t.TempDir()
	username = clihelp.RandomUsername(t)
	password = clihelp.RandomPassword(t)
	userID, token = testsupport.Register(t, srv.HTTPURL, srv.InviteCode, username, password)
	loginViaFlags(t, srv, xdg, username, password)
	return xdg, username, password, userID, token
}

// postDMViaREST exercises the server's DM-send endpoint directly so the
// CLI test doesn't have to drive a second chatd process to seed
// conversation state. Returns the message id.
func postDMViaREST(t *testing.T, srv *testsupport.Server, token, conversationID, body string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		fmt.Sprintf("/api/dms/%s/messages", conversationID), token,
		map[string]string{"body": body})
	if status != http.StatusCreated {
		t.Fatalf("post dm message: status %d body=%s", status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode dm message: %v body=%s", err, raw)
	}
	return data.ID
}

// createDMViaREST find-or-creates the conversation between caller and
// peer. Returns the conversation id.
func createDMViaREST(t *testing.T, srv *testsupport.Server, token, peerID string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", token,
		map[string]string{"peer_user_id": peerID})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("post /api/dms: status %d body=%s", status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/dms data: %v body=%s", err, raw)
	}
	return data.ID
}

// dmListUnread returns the unread_count on the conversation between
// `me` and the peer encoded in `peerSubstr` — caller picks a peer
// substring unique per-test (typically the username) to disambiguate
// rows in the response.
func dmListUnread(t *testing.T, srv *testsupport.Server, token, peerSubstr string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.HTTPURL+"/api/dms", nil)
	if err != nil {
		t.Fatalf("build /api/dms req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed below
	if err != nil {
		t.Fatalf("get /api/dms: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get /api/dms: status %d body=%s", resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Conversations []struct {
				Peer struct {
					Username string `json:"username"`
				} `json:"peer"`
				UnreadCount int `json:"unread_count"`
			} `json:"conversations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode /api/dms: %v body=%s", err, raw)
	}
	for _, c := range env.Data.Conversations {
		if strings.Contains(c.Peer.Username, peerSubstr) {
			return c.UnreadCount
		}
	}
	t.Fatalf("conversation with peer matching %q not found in: %s", peerSubstr, raw)
	return -1
}

// channelReadCursor reads the viewer's stored last_read_message_id for
// channelID by listing channels and matching by id. Returns the empty
// string when no row has been materialized yet for the viewer.
func channelReadCursor(t *testing.T, srv *testsupport.Server, token, channelID string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.HTTPURL+"/api/channels", nil)
	if err != nil {
		t.Fatalf("build /api/channels req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed below
	if err != nil {
		t.Fatalf("get /api/channels: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get /api/channels: status %d body=%s", resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Channels []struct {
				ID                string  `json:"id"`
				LastReadMessageID *string `json:"last_read_message_id"`
			} `json:"channels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode /api/channels: %v body=%s", err, raw)
	}
	for _, c := range env.Data.Channels {
		if c.ID == channelID {
			if c.LastReadMessageID == nil {
				return ""
			}
			return *c.LastReadMessageID
		}
	}
	t.Fatalf("channel %q not found in: %s", channelID, raw)
	return ""
}

// TestDMListShowsConversations seeds two DMs from peer→viewer, then
// exercises `chatd dm list` and asserts the format and that the peer's
// username is present.
func TestDMListShowsConversations(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, aliceID, _ := registerAndLogin(t, srv)
	_, bobName, _, _, bobTok := registerAndLogin(t, srv)

	convID := createDMViaREST(t, srv, bobTok, aliceID)
	_ = postDMViaREST(t, srv, bobTok, convID, "msg-1")
	_ = postDMViaREST(t, srv, bobTok, convID, "msg-2")

	stdout, stderr, code := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "dm", "list")
	if code != 0 {
		t.Fatalf("dm list exit=%d stderr=%q", code, stderr)
	}
	out := strings.TrimRight(stdout, "\n")
	if out == "" {
		t.Fatalf("dm list produced empty stdout (tok ok? bob=%s)", bobTok[:8])
	}
	lines := strings.Split(out, "\n")
	found := false
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			t.Errorf("dm list row %q: want 4 tab-separated fields, got %d", line, len(parts))
			continue
		}
		if parts[1] == bobName {
			found = true
			if parts[2] != "2" {
				t.Errorf("dm list unread for %s: want 2, got %s (line=%q)", bobName, parts[2], line)
			}
			if parts[3] == "" {
				t.Errorf("dm list last_message_at empty for %s (line=%q)", bobName, line)
			}
		}
	}
	if !found {
		t.Errorf("dm list did not include peer %q; got:\n%s", bobName, out)
	}
}

// TestDMSendByUsernameAndID sends one DM resolving the peer by username
// and a second resolving by user-id, asserting both prints
// `<message_id>\t<body>`.
func TestDMSendByUsernameAndID(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, _, _ := registerAndLogin(t, srv)
	_, bobName, _, bobID, _ := registerAndLogin(t, srv)

	// By username.
	stdout, stderr, code := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "dm", "send", bobName, smallBody)
	if code != 0 {
		t.Fatalf("dm send by username: exit=%d stderr=%q", code, stderr)
	}
	parts := strings.Split(strings.TrimRight(stdout, "\n"), "\t")
	if len(parts) != 2 {
		t.Fatalf("dm send by username stdout %q: want id\\tbody", stdout)
	}
	if parts[1] != smallBody {
		t.Errorf("dm send body echoed=%q, want %q", parts[1], smallBody)
	}
	if len(parts[0]) != 26 {
		t.Errorf("dm send id %q: want 26-char ULID", parts[0])
	}

	// By user-id (ULID).
	stdout2, stderr2, code2 := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "dm", "send", bobID, "from-id-path")
	if code2 != 0 {
		t.Fatalf("dm send by id: exit=%d stderr=%q", code2, stderr2)
	}
	parts2 := strings.Split(strings.TrimRight(stdout2, "\n"), "\t")
	if len(parts2) != 2 || parts2[1] != "from-id-path" {
		t.Errorf("dm send by id stdout %q: want id\\tfrom-id-path", stdout2)
	}
}

// TestDMHistoryNewestFirst sends three DMs in order and asserts history
// returns them newest-first as `<id>\t<sender>\t<body>\t<created_at>`.
func TestDMHistoryNewestFirst(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, aliceID, aliceTok := registerAndLogin(t, srv)
	_, bobName, _, _, bobTok := registerAndLogin(t, srv)

	convID := createDMViaREST(t, srv, bobTok, aliceID)
	id1 := postDMViaREST(t, srv, bobTok, convID, "first")
	// CreatedAt resolution is RFC3339Nano so a zero-sleep would let the
	// SQLite clock skew rows into the same nanosecond on a fast box;
	// 2ms keeps the order assertion deterministic without the test
	// caring about the exact spacing.
	time.Sleep(2 * time.Millisecond)
	id2 := postDMViaREST(t, srv, bobTok, convID, "second")
	time.Sleep(2 * time.Millisecond)
	id3 := postDMViaREST(t, srv, aliceTok, convID, "third-from-alice")

	stdout, stderr, code := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "dm", "history", bobName)
	if code != 0 {
		t.Fatalf("dm history: exit=%d stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("dm history: want 3 lines, got %d:\n%s", len(lines), stdout)
	}
	wantOrder := []string{id3, id2, id1}
	for i, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			t.Errorf("dm history line %d %q: want 4 fields, got %d", i, line, len(parts))
			continue
		}
		if parts[0] != wantOrder[i] {
			t.Errorf("dm history line %d id=%q, want %q (full line=%q)", i, parts[0], wantOrder[i], line)
		}
	}
}

// TestDMReadAdvancesUnread sends two messages from bob to alice, asserts
// alice has 2 unread, runs `chatd dm read bob <id>` to mark the first
// message read, then asserts unread drops to 1.
func TestDMReadAdvancesUnread(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, aliceID, aliceTok := registerAndLogin(t, srv)
	_, bobName, _, _, bobTok := registerAndLogin(t, srv)

	convID := createDMViaREST(t, srv, bobTok, aliceID)
	mid1 := postDMViaREST(t, srv, bobTok, convID, "one")
	_ = postDMViaREST(t, srv, bobTok, convID, "two")

	if got := dmListUnread(t, srv, aliceTok, bobName); got != 2 {
		t.Fatalf("baseline unread for alice: want 2, got %d", got)
	}
	stdout, stderr, code := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "dm", "read", bobName, mid1)
	if code != 0 {
		t.Fatalf("dm read: exit=%d stderr=%q", code, stderr)
	}
	if got := strings.TrimRight(stdout, "\n"); got != "ok" {
		t.Errorf("dm read stdout=%q, want ok", stdout)
	}
	if got := dmListUnread(t, srv, aliceTok, bobName); got != 1 {
		t.Errorf("post-read unread for alice: want 1, got %d", got)
	}
}

// TestDMWatchNoArgPrintsAllFrames spawns `chatd dm watch`, posts a DM
// from bob to alice via REST, and asserts the watcher's stdout contains
// a tab-separated frame with the message body and conversation id.
func TestDMWatchNoArgPrintsAllFrames(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, aliceID, _ := registerAndLogin(t, srv)
	_, _, _, _, bobTok := registerAndLogin(t, srv)

	convID := createDMViaREST(t, srv, bobTok, aliceID)

	w := startWatchProc(t, srv.HTTPURL, xdgA, []string{"dm", "watch"})
	defer w.stop()
	w.waitConnected(t, 5*time.Second)

	body := "watch-no-arg-" + filepath.Base(t.TempDir())
	_ = postDMViaREST(t, srv, bobTok, convID, body)

	if !w.waitForLine(body, 5*time.Second) {
		t.Errorf("dm watch did not surface body %q in 5s\nstdout=%q\nstderr=%q",
			body, w.stdoutSnapshot(), w.stderrSnapshot())
	}
	if !w.waitForLine(convID, 5*time.Second) {
		t.Errorf("dm watch did not include conversation id %q\nstdout=%q",
			convID, w.stdoutSnapshot())
	}
}

// TestDMWatchWithPeerFilters spawns `chatd dm watch <peer>` and asserts
// frames from a different conversation are NOT printed while frames in
// the named conversation ARE. The filter is checked by sending one DM
// to a third user and confirming its body never appears.
func TestDMWatchWithPeerFilters(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, aliceID, _ := registerAndLogin(t, srv)
	_, bobName, _, _, bobTok := registerAndLogin(t, srv)
	_, _, _, _, carolTok := registerAndLogin(t, srv)

	convAB := createDMViaREST(t, srv, bobTok, aliceID)
	convAC := createDMViaREST(t, srv, carolTok, aliceID)

	w := startWatchProc(t, srv.HTTPURL, xdgA, []string{"dm", "watch", bobName})
	defer w.stop()
	w.waitConnected(t, 5*time.Second)

	bodyAB := "filter-want-ab"
	bodyAC := "filter-skip-ac"
	_ = postDMViaREST(t, srv, carolTok, convAC, bodyAC)
	// Give the carol→alice frame a moment to arrive (and be filtered).
	time.Sleep(200 * time.Millisecond)
	_ = postDMViaREST(t, srv, bobTok, convAB, bodyAB)

	if !w.waitForLine(bodyAB, 5*time.Second) {
		t.Fatalf("dm watch did not surface AB body %q\nstdout=%q\nstderr=%q",
			bodyAB, w.stdoutSnapshot(), w.stderrSnapshot())
	}
	if strings.Contains(w.stdoutSnapshot(), bodyAC) {
		t.Errorf("dm watch leaked unfiltered body %q in stdout=%q", bodyAC, w.stdoutSnapshot())
	}
}

// TestChannelsReadAdvancesCursor creates a channel, posts two messages
// as alice, then exercises `chatd channels read <name> <id>` and
// asserts the stored last_read_message_id matches the message id passed.
func TestChannelsReadAdvancesCursor(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	xdgA, _, _, _, aliceTok := registerAndLogin(t, srv)

	chanName := clihelp.RandomChannelName(t)
	chanID := createChannelViaREST(t, srv, aliceTok, chanName)
	_ = postChannelMessageViaREST(t, srv, aliceTok, chanID, "first")
	mid2 := postChannelMessageViaREST(t, srv, aliceTok, chanID, "second")

	// First call to GET /api/channels materializes alice's channel_reads
	// row with last_read = last_message_id at the time (decision §11).
	// channelsReadCursor below would observe that baseline; the explicit
	// call here freezes it before we POST /read.
	_ = channelReadCursor(t, srv, aliceTok, chanID)

	stdout, stderr, code := chatdRun(t, xdgA, "", nil,
		"--server", srv.HTTPURL, "channels", "read", chanName, mid2)
	if code != 0 {
		t.Fatalf("channels read: exit=%d stderr=%q", code, stderr)
	}
	if got := strings.TrimRight(stdout, "\n"); got != "ok" {
		t.Errorf("channels read stdout=%q, want ok", stdout)
	}
	if got := channelReadCursor(t, srv, aliceTok, chanID); got != mid2 {
		t.Errorf("channels read did not advance cursor: got %q, want %q", got, mid2)
	}
}

// TestDMSubcommandUsageErrors asserts that bare `chatd dm` and unknown
// `chatd dm <subcmd>` inputs exit non-zero with a usage hint, so a
// silent no-op never ships.
func TestDMSubcommandUsageErrors(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	xdg, _, _, _, _ := registerAndLogin(t, srv)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bare dm", []string{"dm"}, "usage:"},
		{"unknown subcmd", []string{"dm", "frobnicate"}, "unknown dm subcommand"},
		{"dm send no body", []string{"dm", "send", "alice"}, "usage:"},
		{"dm read missing args", []string{"dm", "read", "alice"}, "usage:"},
		{"channels read missing args", []string{"channels", "read", "general"}, "usage:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--server", srv.HTTPURL}, c.args...)
			_, stderr, code := chatdRun(t, xdg, "", nil, args...)
			if code == 0 {
				t.Fatalf("%s: exit=0 (expected non-zero) stderr=%q", c.name, stderr)
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("%s: stderr=%q, want to contain %q", c.name, stderr, c.want)
			}
		})
	}
}

// createChannelViaREST mirrors the helper in tests/e2e/phase-2 but is
// kept package-local because the e2e tree intentionally keeps each
// phase's harness self-contained.
func createChannelViaREST(t *testing.T, srv *testsupport.Server, token, name string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", token,
		map[string]string{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create channel %q: status %d body=%s", name, status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channel: %v body=%s", err, raw)
	}
	return data.ID
}

// postChannelMessageViaREST posts a channel message and returns its id.
func postChannelMessageViaREST(t *testing.T, srv *testsupport.Server, token, channelID, body string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		fmt.Sprintf("/api/channels/%s/messages", channelID), token,
		map[string]string{"body": body})
	if status != http.StatusCreated {
		t.Fatalf("post channel message: status %d body=%s", status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channel message: %v body=%s", err, raw)
	}
	return data.ID
}
