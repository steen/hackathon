// Package ws_hardening_e2e_test holds black-box tests for the
// phase-1 ws-hardening feature
// (specs/plans/phase-1/feature-ws-hardening.md).
//
// Each test boots the production chat-server binary on a free
// loopback port with random secrets and a fresh sqlite DB, then
// drives /ws via the coder/websocket client. No internal-package
// imports from apps/** — coupling is just the binary path passed to
// `go build`.
//
// Helpers live next to the tests (not in tests/e2e/internal) until a
// third call site needs them, per CLAUDE.md "no shared abstractions
// until 3+ features need them".
package ws_hardening_e2e_test

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
)

type envelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *envelopeError   `json:"error"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

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

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../tests/e2e/phase-1/ws-hardening/harness_test.go ; up
	// five Dir() calls to the repo root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at %s: %v", root, err)
	}
	return root
}

// startServerOpts boots apps/server with caller-controlled env. The
// AC-1 origin test needs CHAT_ALLOWED_ORIGINS set to a non-loopback
// host so cross-origin rejection can be observed; the standard
// startServer in sibling test dirs does not expose that knob.
type startServerOpts struct {
	allowedOrigins string
}

func startServer(t *testing.T, opts startServerOpts) *runningServer {
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
	env := append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
	)
	if opts.allowedOrigins != "" {
		env = append(env, "CHAT_ALLOWED_ORIGINS="+opts.allowedOrigins)
	}
	cmd.Env = env
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

// originHeader builds the per-dial header set used by AC-1. A nil
// return means "do not set Origin" (loopback dev path).
func originHeader(origin string) http.Header {
	if origin == "" {
		return nil
	}
	h := http.Header{}
	h.Set("Origin", origin)
	return h
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

// register + login + mint ws-ticket. Returns a fresh single-use
// ticket value redeemable by /ws once.
func mintTicket(t *testing.T, srv *runningServer) string {
	t.Helper()
	username := "alice-" + randomSecret(t, 4)
	password := randomSecret(t, 12)

	status, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register: status %d body %s", status, raw)
	}
	var regData struct {
		Token string `json:"token"`
	}
	if env.Data == nil {
		t.Fatalf("register: nil data body=%s", raw)
	}
	if err := json.Unmarshal(*env.Data, &regData); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if regData.Token == "" {
		t.Fatalf("register: empty token body=%s", raw)
	}

	status, env, raw = postJSON(t, srv, "/api/auth/ws-ticket", regData.Token, nil)
	if status != http.StatusOK {
		t.Fatalf("/ws-ticket: status %d body %s", status, raw)
	}
	if env.Data == nil {
		t.Fatalf("/ws-ticket: nil data body=%s", raw)
	}
	var tdata struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(*env.Data, &tdata); err != nil {
		t.Fatalf("decode /ws-ticket data: %v body=%s", err, raw)
	}
	if tdata.Ticket == "" {
		t.Fatalf("/ws-ticket: empty ticket body=%s", raw)
	}
	return tdata.Ticket
}
