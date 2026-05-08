package web_app_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestAC5_WebAppPresenceListContract asserts AC-5 from
// specs/plans/phase-2/40-feature-web-app.md verbatim:
//
//	"Once `50-feature-presence.md` lands, the chat page renders an
//	online-users list driven by the initial `GET /api/presence` plus
//	`presence` events on the WS stream."
//
// Presence has landed (apps/web/src/hooks/usePresence.ts plus
// apps/server/internal/wsapi/presence.go), so this test exercises
// both halves of the AC's contract:
//
//  1. bundle_consumes_api_presence_and_presence_event — build apps/web
//     and assert the emitted JS bundle string-references "/api/presence"
//     and the "presence" WS event type. The chat page module is the only
//     surface in the app that uses these strings (usePresence.ts), so
//     finding both literals in the bundle is the cheapest proof the
//     production build is wired to AC-5's contract. Without this half
//     a unit-test-only refactor that drops the production import path
//     would still ship and the chat page would silently lose its
//     presence list.
//
//  2. server_emits_seed_and_join_leave_events — boot the production
//     apps/server binary and exercise the same contract end-to-end:
//     two registered users dial /ws; GET /api/presence (the seed source)
//     returns both with id+username; one user disconnects and the other
//     observes a {type:"presence", data:{kind:"leave", user_id}} frame.
//     This is the wire the chat page actually consumes; if the server
//     stopped honoring either half (REST shape or WS frame schema), the
//     bundle's wiring would render an empty / stale list at runtime.
//
// Server-internal presence behavior (broadcast on connect/disconnect,
// REST shape, hub bookkeeping) is also exercised under
// tests/e2e/phase-2/presence/. This test names AC-5 of the *web-app*
// feature: the chat page's contract with that layer.
func TestAC5_WebAppPresenceListContract(t *testing.T) {
	t.Run("bundle_consumes_api_presence_and_presence_event", func(t *testing.T) {
		for _, tool := range []string{"pnpm", "node"} {
			if _, err := exec.LookPath(tool); err != nil {
				t.Skipf("required tool %q not on PATH: %v", tool, err)
			}
		}

		root := repoRoot(t)
		webDir := filepath.Join(root, "apps", "web")

		// node_modules must already be installed for `pnpm --filter web
		// build` to type-check (tsc -p tsconfig.build.json needs the
		// `vite/client` types). The `go` CI job does not run pnpm
		// install; it pairs with the `pnpm` and `e2e` jobs which do.
		// Skipping here mirrors the AC-1 build_test.go contract: this
		// sub-test only runs when an install has happened upstream.
		if _, err := os.Stat(filepath.Join(webDir, "node_modules")); err != nil {
			t.Skipf("apps/web/node_modules missing — run `pnpm install --frozen-lockfile` first: %v", err)
		}

		distDir := filepath.Join(webDir, "dist")
		if err := os.RemoveAll(distDir); err != nil {
			t.Fatalf("clean %s: %v", distDir, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "pnpm", "--filter", "web", "build")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CI=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("pnpm --filter web build did not finish within 5m\n--- stdout ---\n%s\n--- stderr ---\n%s",
				stdout.String(), stderr.String())
		}
		if err != nil {
			t.Fatalf("pnpm --filter web build exited non-zero: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				err, stdout.String(), stderr.String())
		}

		// The bundle name is content-hashed (Vite default), so glob
		// dist/assets/*.js and concatenate. The two literals must
		// appear somewhere in the bundled code.
		assetsDir := filepath.Join(distDir, "assets")
		entries, err := os.ReadDir(assetsDir)
		if err != nil {
			t.Fatalf("read %s: %v", assetsDir, err)
		}
		var jsFiles []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
				jsFiles = append(jsFiles, filepath.Join(assetsDir, e.Name()))
			}
		}
		if len(jsFiles) == 0 {
			t.Fatalf("no .js bundles emitted under %s", assetsDir)
		}

		var combined strings.Builder
		for _, p := range jsFiles {
			raw, err := os.ReadFile(p)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("expected bundle %s missing after build", p)
				}
				t.Fatalf("read %s: %v", p, err)
			}
			combined.Write(raw)
			combined.WriteByte('\n')
		}
		bundle := combined.String()

		// "/api/presence" — the REST seed path the chat page must hit
		// for the initial online-users list. Wrapped in slashes so a
		// stray substring like "presence/foo" doesn't satisfy the
		// check. The literal in usePresence.ts is the exact path.
		if !strings.Contains(bundle, "/api/presence") {
			t.Errorf("web bundle does not contain %q — chat page is not wired to the REST seed half of AC-5",
				"/api/presence")
		}

		// "presence" event-type literal — the chat page filters WS
		// frames by ev.type === "presence". A bundle without this
		// literal can't dispatch presence frames into the user list.
		// Quoting forces a string-context match (matches both
		// "presence" and 'presence' minified output).
		if !strings.Contains(bundle, `"presence"`) && !strings.Contains(bundle, `'presence'`) {
			t.Errorf("web bundle does not contain a quoted %q literal — chat page cannot dispatch WS presence frames",
				"presence")
		}
	})

	t.Run("server_emits_seed_and_join_leave_events", func(t *testing.T) {
		srv := startWebAppPresenceServer(t)

		alicePassword := randomHex(t, 12)
		bobPassword := randomHex(t, 12)
		aliceID, aliceTok := registerUser(t, srv, "alice", alicePassword)
		bobID, bobTok := registerUser(t, srv, "bob", bobPassword)

		aliceConn := dialAuthedWS(t, srv, aliceTok)
		defer aliceConn.CloseNow()
		// Drain alice's self-join so the post-leave assertion below
		// can match the next presence frame unambiguously.
		aliceFrames := startPresenceFrameCollector(t, aliceConn)
		if got := awaitPresenceFrame(t, aliceFrames, 2*time.Second); got == nil {
			t.Fatalf("alice did not observe her own join frame within 2s")
		} else if got.Kind != "join" || got.UserID != aliceID {
			t.Fatalf("alice's first frame: kind=%q user_id=%q, want join/%s",
				got.Kind, got.UserID, aliceID)
		}

		bobConn := dialAuthedWS(t, srv, bobTok)

		// Bob's join must show up on alice's stream — this is the
		// "presence events on the WS stream" half of AC-5 for the
		// join direction. Asserting it here also drains the frame so
		// the leave assertion below sees an unambiguous next-frame.
		joinFrame := awaitPresenceFrame(t, aliceFrames, 2*time.Second)
		if joinFrame == nil {
			t.Fatalf("alice did not receive a presence frame for bob's connect within 2s")
		}
		if joinFrame.Kind != "join" || joinFrame.UserID != bobID {
			t.Fatalf("bob connect frame: kind=%q user_id=%q, want join/%s",
				joinFrame.Kind, joinFrame.UserID, bobID)
		}

		// Wait for both users to be reflected on the hub before the
		// REST seed assertion. Without this guard the test races the
		// server's AddPresence path on a fast machine.
		if !waitFor(2*time.Second, func() bool {
			users := fetchPresenceList(t, srv, aliceTok)
			return len(users) == 2
		}) {
			users := fetchPresenceList(t, srv, aliceTok)
			t.Fatalf("GET /api/presence did not reach 2 users within 2s; got %d: %+v",
				len(users), users)
		}

		// Half 1: REST seed. The chat page calls this on mount —
		// every entry must carry both id and username, since the UI
		// renders the username and falls back to the id only when the
		// row is missing (see apps/web/src/routes/Chat.tsx).
		seed := fetchPresenceList(t, srv, aliceTok)
		byID := make(map[string]presenceListUser, len(seed))
		for _, u := range seed {
			if u.ID == "" {
				t.Errorf("/api/presence seed entry has empty id: %+v (full=%+v)", u, seed)
			}
			if u.Username == "" {
				t.Errorf("/api/presence seed entry id=%s has empty username (chat page expects both fields): full=%+v",
					u.ID, seed)
			}
			byID[u.ID] = u
		}
		if alice, ok := byID[aliceID]; !ok {
			t.Errorf("seed missing alice (id=%s): got %+v", aliceID, seed)
		} else if alice.Username != "alice" {
			t.Errorf("seed alice.username = %q, want %q", alice.Username, "alice")
		}
		if bob, ok := byID[bobID]; !ok {
			t.Errorf("seed missing bob (id=%s): got %+v", bobID, seed)
		} else if bob.Username != "bob" {
			t.Errorf("seed bob.username = %q, want %q", bob.Username, "bob")
		}

		// Half 2: live WS event. Bob disconnects → alice's stream
		// must carry a presence/leave frame for bob's id within 3s.
		// (Server-side cleanup runs on the read loop after Close, so
		// the budget mirrors phase-2/presence/events_test.go.)
		if err := bobConn.Close(websocket.StatusNormalClosure, "test done"); err != nil {
			t.Fatalf("close bob conn: %v", err)
		}
		leave := awaitPresenceFrame(t, aliceFrames, 3*time.Second)
		if leave == nil {
			t.Fatalf("alice did not receive a presence frame for bob's disconnect within 3s")
		}
		if leave.Kind != "leave" {
			t.Errorf("bob disconnect frame: kind=%q, want %q", leave.Kind, "leave")
		}
		if leave.UserID != bobID {
			t.Errorf("bob disconnect frame: user_id=%q, want %q (bob)", leave.UserID, bobID)
		}
	})
}

// --- harness ----------------------------------------------------------
//
// Mirrors tests/e2e/phase-2/presence/harness_test.go but kept local to
// this package so the web-app E2E tree stays self-contained. Names are
// suffixed (`startWebAppPresenceServer`, `dialAuthedWS`,
// `fetchPresenceList`, `presenceListUser`, etc.) to avoid colliding
// with sibling helpers in the same package if future tests add their
// own harness.

type webAppPresenceServer struct {
	httpURL    string
	wsURL      string
	port       int
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

type presenceEnvelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type presenceListUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type presenceFrame struct {
	Kind   string
	UserID string
}

func randomHex(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func waitForListen(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}

func startWebAppPresenceServer(t *testing.T) *webAppPresenceServer {
	t.Helper()
	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	port := freeTCPPort(t)
	jwtSecret := randomHex(t, 32)
	invite := randomHex(t, 8)
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	wait := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(wait)
	}()
	if err := waitForListen(port, 10*time.Second); err != nil {
		cancel()
		<-wait
		t.Fatalf("server did not listen on :%d in time: %v", port, err)
	}
	t.Cleanup(func() {
		cancel()
		<-wait
	})
	return &webAppPresenceServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL:      fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:       port,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

func registerUser(t *testing.T, srv *webAppPresenceServer, username, password string) (id, token string) {
	t.Helper()
	body := map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode register: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/register", &buf)
	if err != nil {
		t.Fatalf("new register req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	status, env, raw := doPresenceReq(t, req)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register %s: status %d body %s", username, status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("register %s: envelope ok=%v data=%v", username, env.OK, env.Data)
	}
	var data struct {
		Token string `json:"token"`
		User  struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.User.ID == "" {
		t.Fatalf("register %s: empty user id (body=%s)", username, raw)
	}
	return data.User.ID, data.Token
}

func mintWSTicket(t *testing.T, srv *webAppPresenceServer, bearer string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/ws-ticket", nil)
	if err != nil {
		t.Fatalf("new ws-ticket req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	status, env, raw := doPresenceReq(t, req)
	if status != http.StatusOK {
		t.Fatalf("/ws-ticket: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/ws-ticket envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /ws-ticket data: %v body=%s", err, raw)
	}
	if data.Ticket == "" {
		t.Fatalf("/ws-ticket: empty ticket")
	}
	return data.Ticket
}

func dialAuthedWS(t *testing.T, srv *webAppPresenceServer, bearer string) *websocket.Conn {
	t.Helper()
	ticket := mintWSTicket(t, srv, bearer)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// resp.Body is owned by the *websocket.Conn after a successful
	// upgrade; closing it here would race the WS close handshake. The
	// .golangci.yml note on bodyclose for _test.go covers the lint
	// suppression.
	c, resp, err := websocket.Dial(dialCtx, srv.wsURL+"?ticket="+ticket, nil)
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

func fetchPresenceList(t *testing.T, srv *webAppPresenceServer, bearer string) []presenceListUser {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.httpURL+"/api/presence", nil)
	if err != nil {
		t.Fatalf("new GET /api/presence: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	status, env, raw := doPresenceReq(t, req)
	if status != http.StatusOK {
		t.Fatalf("GET /api/presence: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("GET /api/presence: envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Users []presenceListUser `json:"users"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/presence data: %v body=%s", err, raw)
	}
	return data.Users
}

func doPresenceReq(t *testing.T, req *http.Request) (int, presenceEnvelope, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s %s: %v", req.Method, req.URL, err)
	}
	var env presenceEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope from %s %s (status %d): %v\nbody=%q",
				req.Method, req.URL, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode, env, raw
}

// startPresenceFrameCollector forwards parsed `{type:"presence",
// data:{kind, user_id}}` frames from `conn` onto the returned channel.
// Non-presence frames are dropped.
func startPresenceFrameCollector(t *testing.T, conn *websocket.Conn) <-chan presenceFrame {
	t.Helper()
	out := make(chan presenceFrame, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer close(out)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var env struct {
				Type string `json:"type"`
				Data struct {
					Kind   string `json:"kind"`
					UserID string `json:"user_id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}
			if env.Type != "presence" {
				continue
			}
			select {
			case out <- presenceFrame{Kind: env.Data.Kind, UserID: env.Data.UserID}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func awaitPresenceFrame(t *testing.T, frames <-chan presenceFrame, timeout time.Duration) *presenceFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			return nil
		}
		return &f
	case <-time.After(timeout):
		return nil
	}
}

func waitFor(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return check()
}
