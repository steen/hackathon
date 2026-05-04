// AC-4 (verbatim from specs/plans/phase-2/50-feature-presence.md):
//
//	"The web app shows online users in the chat page; the CLI `chatd
//	 watch` optionally surfaces presence events."
//
// AC-4 spans two clients: the web app and the chatd CLI.
//
// Web half: lives outside this directory (any Playwright spec belongs
// under tests/e2e/playwright/, which is outside the agent-owned
// presence/** footprint). This file does NOT cover the web half — see
// the cross-reference note in specs/test-analysis/phase-2/presence.md.
//
// CLI half: the spec word "optionally" makes it valid for chatd to
// either surface presence frames or stay silent. Reading
// apps/cli/cmd/watch.go shows the streamOnce loop only prints when
// ev.Message != nil, so the current impl drops presence frames
// silently. This test pins THAT behavior end-to-end:
//
//  1. `chatd watch <channel>` stays alive while a peer joins/leaves
//     via WS (no crash, no error on stderr).
//  2. The chatd watcher's stdout does NOT emit any presence-related
//     noise — no "presence", "join", or "leave" tokens, no JSON frame
//     with "type":"presence" leaking through. Silence-by-default is
//     the contract under "optionally".
//
// If chatd later grows a `--show-presence` flag (or similar) the test
// must be updated to assert the surfaced format explicitly; until
// then, "no garbage" is the live contract.
package presence_e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPresenceAC4_ChatdWatchStaysSilentOnPeerJoinAndLeave(t *testing.T) {
	srv := startServer(t)

	aliceName := randomCLIUsername(t)
	alicePass := randomCLIPassword(t)
	bobName := randomCLIUsername(t)
	bobPass := randomCLIPassword(t)

	aliceID, aliceToken := register(t, srv, aliceName, alicePass)
	_, bobToken := register(t, srv, bobName, bobPass)

	xdg := t.TempDir()
	chatdLoginAlice(t, srv, xdg, aliceName, alicePass)

	// Create a per-test channel so other tests' #general traffic and
	// presence state cannot interfere with the assertions below. The
	// channel ID is what the WS handler resolves; chatd watch and the
	// raw WS dial both pass that ID through ?channel=<id>.
	channelID := createChannel(t, srv, aliceToken, randomCLIChannelName(t))

	w := startChatdWatch(t, srv, xdg, channelID)
	defer w.Stop()

	// Wait for chatd's WS subscription to land before doing anything
	// else. Polling /debug/subs is cleaner than sleeping.
	if !waitFor(5*time.Second, func() bool {
		return fetchSubsCount(t, srv, channelID) >= 1
	}) {
		t.Fatalf("chatd watch did not subscribe to %s within 5s; stderr=%q", channelID, w.StderrSnapshot())
	}

	// Bob joins via raw WS on the same channel — this exercises the
	// presence broadcast path. Then bob leaves a moment later. Use
	// /debug/subs to confirm the lifecycle so the assertions below
	// run against a settled state.
	bobConn := dialAuthenticatedWSChannel(t, srv, bobToken, channelID)
	if !waitFor(5*time.Second, func() bool {
		return fetchSubsCount(t, srv, channelID) >= 2
	}) {
		_ = bobConn.CloseNow()
		t.Fatalf("bob's WS subscription to %s never settled to >=2; stderr=%q", channelID, w.StderrSnapshot())
	}

	// Sanity: GET /api/presence sees alice while she's online via the
	// chatd watch subscription. This proves the join broadcast path
	// actually fired and is what any "show online users" UI reads
	// from. AC-4 wires the web/CLI surface to this same source of
	// truth.
	if !waitFor(5*time.Second, func() bool {
		return containsID(fetchPresenceUsers(t, srv, bobToken), aliceID)
	}) {
		t.Fatalf("alice (%s) never appeared in /api/presence; stderr=%q", aliceID, w.StderrSnapshot())
	}

	if err := bobConn.Close(websocket.StatusNormalClosure, "test done"); err != nil {
		// Log but don't fail — close after the server has already
		// torn the connection down returns a benign error.
		t.Logf("bobConn.Close: %v", err)
	}

	// Wait for the leave to settle — the watcher must not have
	// crashed in the meantime.
	if !waitFor(5*time.Second, func() bool {
		return fetchSubsCount(t, srv, channelID) == 1
	}) {
		t.Fatalf("subs count never dropped back to 1 after bob left; stderr=%q", w.StderrSnapshot())
	}

	// Hold for a small window so any post-leave frame the server may
	// have sent has time to land in the chatd watcher's stdout
	// buffer. Keeping this small (250ms) so the test stays fast; the
	// scanner runs in a goroutine and writes synchronously.
	time.Sleep(250 * time.Millisecond)

	// Assertion 1: chatd watch did not exit.
	if w.HasExited() {
		t.Fatalf("chatd watch exited unexpectedly; stderr=%q stdout=%q", w.StderrSnapshot(), w.StdoutSnapshot())
	}

	// Assertion 2: stderr did not surface a watch:-prefixed error
	// (the impl prints `watch: %v (reconnecting in ...)` on stream
	// errors). Anything matching that prefix means the watcher
	// reconnect loop fired, which would mean the presence event path
	// crashed the WS subscription.
	if strings.Contains(w.StderrSnapshot(), "watch:") {
		t.Errorf("chatd watch emitted a watch: error during peer join/leave; stderr=%q", w.StderrSnapshot())
	}

	// Assertion 3: stdout contains no presence-related leakage. The
	// CLI's current contract under the spec's "optionally" wording is
	// silence — no JSON, no `+ joined`, no `presence`, `join`, or
	// `leave` tokens. The streamOnce loop only prints message lines.
	out := w.StdoutSnapshot()
	for _, needle := range []string{"presence", "\"type\":\"presence\"", "join", "leave"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(needle)) {
			t.Errorf("chatd watch stdout leaked presence noise (needle=%q): %q", needle, out)
		}
	}
}

// createChannel creates a channel via REST and returns its server-
// assigned id. The id is what /ws?channel=<id> resolves through the
// handler's ChannelLookup; chatd watch and the raw bob WS dial both
// pass that id.
func createChannel(t *testing.T, srv *runningServer, bearer, name string) string {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/channels", bearer, map[string]string{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("POST /api/channels: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("POST /api/channels envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/channels data: %v body=%s", err, raw)
	}
	if data.ID == "" {
		t.Fatalf("POST /api/channels: empty id (body=%s)", raw)
	}
	return data.ID
}

// fetchSubsCount queries /debug/subs?channel=<id> and returns the
// integer body. The endpoint is unauthenticated. Mirrors the harness's
// fetchSubscriberCount but takes a channel id argument so AC-4 can
// poll its per-test channel rather than the #general default.
func fetchSubsCount(t *testing.T, srv *runningServer, channelID string) int {
	t.Helper()
	u := fmt.Sprintf("%s/debug/subs?channel=%s", srv.httpURL, channelID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("new GET /debug/subs: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /debug/subs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /debug/subs: status %d", resp.StatusCode)
	}
	var count int
	if _, err := fmt.Fscanf(resp.Body, "%d", &count); err != nil {
		t.Fatalf("scan /debug/subs body: %v", err)
	}
	return count
}

// dialAuthenticatedWSChannel mirrors dialAuthenticatedWS but pins a
// channel query parameter. The harness's default helper subscribes to
// #general; AC-4 wants a per-test channel so other tests' #general
// subscribers cannot bleed into the assertions below.
func dialAuthenticatedWSChannel(t *testing.T, srv *runningServer, bearer, channelID string) *websocket.Conn {
	t.Helper()
	ticket := mintTicket(t, srv, bearer)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := srv.wsURL + "?ticket=" + ticket + "&channel=" + channelID
	c, resp, err := websocket.Dial(dialCtx, url, nil)
	if err != nil {
		body := ""
		if resp != nil {
			body = fmt.Sprintf(" status=%d", resp.StatusCode)
		}
		t.Fatalf("dial /ws: %v%s", err, body)
	}
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial /ws: status=%v want 101", resp)
	}
	return c
}

// chatd subprocess plumbing — kept local to this file so a future
// change to harness_test.go for an unrelated AC doesn't ripple.

type chatdWatchProc struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	stdoutR io.ReadCloser
	stderrR io.ReadCloser

	mu       sync.Mutex
	stdout   strings.Builder
	stderr   strings.Builder
	waitOnce sync.Once
	done     chan struct{}
	exited   bool
}

func startChatdWatch(t *testing.T, srv *runningServer, xdg, channelArg string) *chatdWatchProc {
	t.Helper()
	bin := buildChatd(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--server", srv.httpURL, "watch", channelArg)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdg,
		"HOME="+xdg,
		"CHATD_CONFIG_DIR=",
	)

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("startChatdWatch stdout pipe: %v", err)
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("startChatdWatch stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("startChatdWatch: %v", err)
	}

	w := &chatdWatchProc{
		cmd:     cmd,
		cancel:  cancel,
		stdoutR: stdoutR,
		stderrR: stderrR,
		done:    make(chan struct{}),
	}

	go w.scan(stdoutR, &w.stdout)
	go w.scan(stderrR, &w.stderr)
	go func() {
		_ = cmd.Wait()
		w.mu.Lock()
		w.exited = true
		w.mu.Unlock()
		close(w.done)
	}()
	return w
}

func (w *chatdWatchProc) scan(r io.Reader, into *strings.Builder) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		w.mu.Lock()
		into.WriteString(scanner.Text())
		into.WriteByte('\n')
		w.mu.Unlock()
	}
}

func (w *chatdWatchProc) StdoutSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stdout.String()
}

func (w *chatdWatchProc) StderrSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderr.String()
}

func (w *chatdWatchProc) HasExited() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exited
}

func (w *chatdWatchProc) Stop() {
	w.waitOnce.Do(func() {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(2 * time.Second):
		}
	})
}

// buildChatd builds apps/cli once per test process and caches the
// path. Mirrors the cli-full-commands harness pattern so failures
// surface the same way (build output dumped on first call).
var (
	chatdBuildOnce sync.Once
	chatdBuildPath string
	chatdBuildErr  error
)

func buildChatd(t *testing.T) string {
	t.Helper()
	chatdBuildOnce.Do(func() {
		root := repoRoot(t)
		// chatdBuildOnce gates this once per test process, so the
		// build directory must outlive any single test's t.TempDir.
		// Use os.TempDir + a random suffix and let the OS reap it.
		stable := filepath.Join(os.TempDir(), "presence-ac4-chatd-"+randHex(8))
		if err := os.MkdirAll(stable, 0o755); err != nil {
			chatdBuildErr = fmt.Errorf("mkdir chatd build dir: %w", err)
			return
		}
		out := filepath.Join(stable, "chatd")
		build := exec.Command("go", "build", "-o", out, "./apps/cli")
		build.Dir = root
		if combined, err := build.CombinedOutput(); err != nil {
			chatdBuildErr = fmt.Errorf("go build ./apps/cli: %w\n%s", err, combined)
			return
		}
		chatdBuildPath = out
	})
	if chatdBuildErr != nil {
		t.Fatalf("%v", chatdBuildErr)
	}
	return chatdBuildPath
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}
	return fmt.Sprintf("%x", b)
}

func chatdLoginAlice(t *testing.T, srv *runningServer, xdg, username, password string) {
	t.Helper()
	bin := buildChatd(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--server", srv.httpURL,
		"login", "--username", username, "--password", password)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdg,
		"HOME="+xdg,
		"CHATD_CONFIG_DIR=",
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("chatd login: exit=%d stderr=%q", exitErr.ExitCode(), errOut.String())
		}
		t.Fatalf("chatd login: %v stderr=%q", err, errOut.String())
	}
}

// randomCLIUsername / Password / ChannelName are scoped to this file
// to avoid colliding with the harness's existing helpers if any
// future PR adds them there. The character classes match the
// server-side regexes.

func randomCLIUsername(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "u" + string(b[:11])
}

func randomCLIPassword(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return fmt.Sprintf("%x", b)
}

func randomCLIChannelName(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "c" + string(b[:9])
}
