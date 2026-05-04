// Package presence_e2e_test holds black-box E2E tests for the phase-2
// presence feature (specs/plans/phase-2/50-feature-presence.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB, then drives the
// /api/* endpoints through real HTTP and /ws over a real WebSocket.
//
// The harness mirrors tests/e2e/phase-1/auth-endpoints/harness_test.go
// but adds a dialAuthenticatedWS helper because every presence test
// needs the ticket-mint + ws-dial dance.
package presence_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type runningServer struct {
	httpURL    string
	wsURL      string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

type envelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *envelopeError   `json:"error"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func waitForPort(port int, timeout time.Duration) error {
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

// repoRoot walks up from this file
// (.../tests/e2e/phase-2/presence/harness_test.go) to the repo root —
// five Dir() calls. Sanity-checked by stat-ing go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at %s: %v", root, err)
	}
	return root
}

func startServer(t *testing.T) *runningServer {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	port := freePort(t)
	jwtSecret := randomSecret(t, 32)
	invite := randomSecret(t, 8)
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
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

	if err := waitForPort(port, 10*time.Second); err != nil {
		cancel()
		<-wait
		t.Fatalf("server did not listen on :%d in time: %v", port, err)
	}

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &runningServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL:      fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:       port,
		dbPath:     dbPath,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

func postJSON(t *testing.T, srv *runningServer, path, bearer string, body any) (int, envelope, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s body: %v", path, err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+path, &buf)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return doRequest(t, req)
}

func getJSON(t *testing.T, srv *runningServer, path, bearer string) (int, envelope, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.httpURL+path, nil)
	if err != nil {
		t.Fatalf("new GET %s: %v", path, err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return doRequest(t, req)
}

func doRequest(t *testing.T, req *http.Request) (int, envelope, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s %s: %v", req.Method, req.URL, err)
	}
	var env envelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope from %s %s (status %d): %v\nbody=%q", req.Method, req.URL, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode, env, raw
}

func register(t *testing.T, srv *runningServer, username, password string) (userID, token string) {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
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

func mintTicket(t *testing.T, srv *runningServer, bearer string) string {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/auth/ws-ticket", bearer, nil)
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
		t.Fatalf("/ws-ticket: empty ticket (body=%s)", raw)
	}
	return data.Ticket
}

// dialAuthenticatedWS mints a one-shot ticket for `bearer` and dials
// /ws (default channel #general). On success the returned conn is
// already subscribed; on failure the test is failed via t.Fatalf.
func dialAuthenticatedWS(t *testing.T, srv *runningServer, bearer string) *websocket.Conn {
	t.Helper()
	ticket := mintTicket(t, srv, bearer)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
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

// fetchSubscriberCount queries /debug/subs?channel=#general and parses
// "<n>\n" into an int. The endpoint is unauthenticated.
func fetchSubscriberCount(t *testing.T, srv *runningServer) int {
	t.Helper()
	u := fmt.Sprintf("%s/debug/subs?channel=%%23general", srv.httpURL)
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

// waitFor polls `check` every 25ms until it returns true or `timeout`
// elapses. Returns true on success, false on timeout.
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
