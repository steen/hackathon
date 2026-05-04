// Package security_headers_e2e_test holds black-box tests for the
// phase-1 feature wiring SecurityHeaders + db.EnsureFile at startup
// (specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB, then exercises the
// HTTP and WebSocket surface through real network calls. Helpers live
// in this file rather than a shared package because the gold-standard
// pattern (tests/e2e/phase-1/auth-endpoints/harness_test.go) keeps each
// feature dir self-contained until 3+ feature dirs need the same code.
package security_headers_e2e_test

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

// expectedCSP is pinned verbatim to the value set by
// apps/server/internal/http/headers_middleware.go (PRD §9, kept
// byte-identical). The internal package is not import-reachable from
// tests/, so the test re-states the literal; any change must update
// both sites.
const expectedCSP = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// runningServer carries the per-test handle the helpers need to talk
// to the spawned chat-server.
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

// repoRoot walks up from this file (.../tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/harness_test.go)
// to the repo root (5 dirs up). Sanity-checked by stat-ing go.mod.
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
	return startServerWithTags(t, "")
}

// startServerWithTags is identical to startServer except it forwards
// `-tags=<tags>` to `go build`. Used by the AC-1 panic arm to compile
// in apps/server/internal/wiring/panicprobe.go's /debug/panic route.
// Empty tags means a default build, equivalent to startServer.
func startServerWithTags(t *testing.T, tags string) *runningServer {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	args := []string{"build"}
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, "-o", binPath, "./apps/server")
	build := exec.Command("go", args...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server (tags=%q) failed: %v\n%s", tags, err, out)
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

// register POSTs /api/auth/register and returns the token. Used to
// obtain the bearer needed to mint a /ws ticket.
func register(t *testing.T, srv *runningServer, username, password string) string {
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
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.Token == "" {
		t.Fatalf("register %s: empty token (body=%s)", username, raw)
	}
	return data.Token
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
	return data.Ticket
}

// requireSecHeaders fails the test if any of the four SEC-10 baseline
// headers is missing or has a value other than the one set by
// apps/server/internal/http/headers_middleware.go. Values are imported
// from that package for CSP (kept verbatim per spec) and pinned to the
// other three literals from the same source.
func requireSecHeaders(t *testing.T, label string, h http.Header) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": expectedCSP,
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
	}
	for name, expected := range want {
		got := h.Get(name)
		if got == "" {
			t.Errorf("%s: missing header %s", label, name)
			continue
		}
		if got != expected {
			t.Errorf("%s: header %s = %q, want %q", label, name, got, expected)
		}
	}
}
