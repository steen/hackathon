package cli_full_commands_e2e_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/clihelp"
)

// AC-5: `chatd watch <channel>` streams new messages to stdout, with
// reconnect on disconnect.
//
// Two halves:
//   - TestAC5_Watch_StreamsNewMessages — proves the streaming half end-to-end.
//   - TestAC5_Watch_ReconnectsAfterServerRestart — proves the reconnect
//     half by bouncing the server on the same port; this one is more
//     brittle (relies on the OS releasing the port quickly) so it has
//     a wider patience budget and a clear failure message.
//
// Both spawn chatd as a long-running subprocess and scan its stdout
// in a goroutine via bufio.Scanner. A line buffer cap is set
// generously so a single oversized line cannot cause a silent drop.
func TestAC5_Watch_StreamsNewMessages(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	clihelp.LoginViaFlags(t, srv.url, xdg, username, password)

	channel := clihelp.RandomChannelName(t)
	channelID := createChannelViaREST(t, srv, token, channel)

	w := startWatch(t, srv, xdg, channelID)
	defer w.Stop()

	if !w.WaitConnected(5 * time.Second) {
		t.Fatalf("AC-5: chatd watch did not appear to connect within 5s; stderr=%q", w.StderrSnapshot())
	}

	body := "live-message-from-rest"
	_ = postMessageViaREST(t, srv, token, channelID, body)

	if !w.WaitForLineContaining(body, 5*time.Second) {
		t.Errorf("AC-5: watch stdout did not surface posted message %q within 5s\nstdout=%q\nstderr=%q",
			body, w.StdoutSnapshot(), w.StderrSnapshot())
	}
}

// AC-5 (reconnect half): kill the server, bring it back up on the
// same port, post a message, and assert chatd reconnected and printed
// the new message. The same-port restart depends on the OS releasing
// the listener quickly — if this test is flaky in CI the followup is
// to introduce a small TCP proxy in this directory's harness rather
// than weakening the assertion.
func TestAC5_Watch_ReconnectsAfterServerRestart(t *testing.T) {
	srv := startServer(t)
	xdg := t.TempDir()

	username := clihelp.RandomUsername(t)
	password := clihelp.RandomPassword(t)
	token, _ := registerViaREST(t, srv, username, password)
	clihelp.LoginViaFlags(t, srv.url, xdg, username, password)

	channel := clihelp.RandomChannelName(t)
	channelID := createChannelViaREST(t, srv, token, channel)

	w := startWatch(t, srv, xdg, channelID)
	defer w.Stop()

	if !w.WaitConnected(5 * time.Second) {
		t.Fatalf("AC-5 reconnect: initial connect did not complete in 5s; stderr=%q", w.StderrSnapshot())
	}

	// Drop the server. The Go subprocess is signalled via its cancel
	// fn; close its wait channel before rebinding to avoid bind races.
	srv.cancel()
	<-srv.wait

	// Rebind on the same port with the same secrets and the same DB
	// path so the server resumes serving the same user/channel.
	srv2, err := restartServer(t, srv)
	if err != nil {
		t.Fatalf("AC-5 reconnect: re-bind failed: %v", err)
	}

	// Give chatd a moment to notice the disconnect and back off, then
	// post the recovery message.
	body := "post-reconnect-message"
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_ = postMessageViaREST(t, srv2, token, channelID, body)
		if w.WaitForLineContaining(body, 2*time.Second) {
			return // success
		}
	}
	t.Errorf("AC-5 reconnect: chatd did not resurface after server restart\nstderr=%q",
		w.StderrSnapshot())
}

// watchProc bundles a running `chatd watch` subprocess + accumulated
// stdout/stderr. Tests use the helper methods rather than poking the
// internals so any future change (line buffering, additional
// mtx-guards) lands in one place.
type watchProc struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	stdoutR io.ReadCloser
	stderrR io.ReadCloser

	mu     sync.Mutex
	stdout strings.Builder
	stderr strings.Builder

	cond     *sync.Cond
	waitOnce sync.Once
	done     chan struct{}
}

func startWatch(t *testing.T, srv *runningServer, xdg, channelID string) *watchProc {
	t.Helper()
	bin := clihelp.BuildChatd(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--server", srv.url, "watch", channelID)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdg,
		"HOME="+xdg,
		"CHATD_CONFIG_DIR=",
	)

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("startWatch stdout pipe: %v", err)
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("startWatch stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("startWatch: %v", err)
	}

	w := &watchProc{
		cmd:     cmd,
		cancel:  cancel,
		stdoutR: stdoutR,
		stderrR: stderrR,
		done:    make(chan struct{}),
	}
	w.cond = sync.NewCond(&w.mu)

	go w.scan(stdoutR, &w.stdout)
	go w.scan(stderrR, &w.stderr)
	go func() {
		_ = cmd.Wait()
		close(w.done)
		w.cond.Broadcast()
	}()
	return w
}

func (w *watchProc) scan(r io.Reader, into *strings.Builder) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		w.mu.Lock()
		into.WriteString(scanner.Text())
		into.WriteByte('\n')
		w.cond.Broadcast()
		w.mu.Unlock()
	}
}

// WaitConnected returns true once chatd has either:
//   - emitted a line on stdout (proves the WS upgrade succeeded), or
//   - been running for at least 500ms with no error on stderr (the
//     watch command stays silent on the happy path until a message
//     arrives, so a "no error after settle" reading is acceptable).
//
// Returns false on timeout.
func (w *watchProc) WaitConnected(timeout time.Duration) bool {
	settleAt := time.Now().Add(500 * time.Millisecond)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		// Any stdout output is a strong positive signal.
		if w.stdout.Len() > 0 {
			w.mu.Unlock()
			return true
		}
		// stderr containing "watch:" indicates an error path.
		err := strings.Contains(w.stderr.String(), "watch:")
		w.mu.Unlock()

		if err {
			return false
		}
		if time.Now().After(settleAt) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// WaitForLineContaining polls stdout for a line containing needle.
func (w *watchProc) WaitForLineContaining(needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		hit := strings.Contains(w.stdout.String(), needle)
		w.mu.Unlock()
		if hit {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (w *watchProc) StdoutSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stdout.String()
}

func (w *watchProc) StderrSnapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderr.String()
}

func (w *watchProc) Stop() {
	w.waitOnce.Do(func() {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(2 * time.Second):
		}
	})
}

// restartServer brings up a fresh server bound to the prior server's
// port, reusing the prior secrets and DB path. Polls the listener up
// to 5s while the OS releases the port.
func restartServer(t *testing.T, prev *runningServer) (*runningServer, error) {
	t.Helper()
	bin := serverBinary(t)

	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, bin)
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("CHAT_SERVER_PORT=%d", prev.port),
			"CHAT_JWT_SECRET="+prev.jwtSecret,
			"CHAT_INVITE_CODE="+prev.inviteCode,
			"CHAT_DB_PATH="+prev.dbPath,
		)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			cancel()
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("re-start exec: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		wait := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(wait)
		}()

		if err := waitForPort(prev.port, 3*time.Second); err == nil {
			next := &runningServer{
				url:        prev.url,
				port:       prev.port,
				inviteCode: prev.inviteCode,
				jwtSecret:  prev.jwtSecret,
				dbPath:     prev.dbPath,
				cancel:     cancel,
				wait:       wait,
			}
			t.Cleanup(func() {
				cancel()
				<-wait
			})
			return next, nil
		}

		// Bind didn't take. Tear this attempt down and retry.
		cancel()
		<-wait
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("port %d never freed up after server stop", prev.port)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
