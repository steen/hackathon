// Package rate_limits_e2e_test holds black-box tests for the phase-1
// rate-limits feature (specs/plans/phase-1/feature-rate-limits.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB, then drives the
// /api/auth/* endpoints through real HTTP. No internal-package imports
// from apps/** — coupling is limited to the binary path passed to
// `go build` and the on-disk env vars.
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them). This mirrors the
// gold-standard pattern in tests/e2e/phase-1/auth-endpoints.
package rate_limits_e2e_test

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

// runningServer carries the per-test handle the helpers need to talk
// to the spawned chat-server.
type runningServer struct {
	httpURL    string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// envelope mirrors the on-the-wire shape from
// apps/server/internal/http/errors.go. Tests use it via JSON so we stay
// decoupled from the production type.
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
// (.../tests/e2e/phase-1/rate-limits/harness_test.go) to the repo root
// (5 dirs up). Sanity-checked by stat-ing go.mod.
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

// startServer builds apps/server, picks a free port, starts the binary
// with random secrets and a tempdir DB, and registers a Cleanup that
// stops it. Returns once the port is listening.
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
		port:       port,
		dbPath:     dbPath,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

// httpClientFromIP returns an http.Client that dials the loopback
// address from the given source IP. Used to exercise the per-IP
// differentiation arm of the rate limiter (e.g. 127.0.0.2 should not
// share a bucket with 127.0.0.1). Returns (nil, err) if the kernel
// refuses to bind the source IP — caller is expected to t.Skip in that
// case.
func httpClientFromIP(srcIP string) (*http.Client, error) {
	localAddr := &net.TCPAddr{IP: net.ParseIP(srcIP)}
	if localAddr.IP == nil {
		return nil, fmt.Errorf("invalid source IP %q", srcIP)
	}
	// Probe once so the test can decide quickly whether to skip.
	probe, err := net.ListenTCP("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("bind %s: %w", srcIP, err)
	}
	_ = probe.Close()

	dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second}
	tr := &http.Transport{
		DialContext: dialer.DialContext,
		// Disable connection reuse so every request gets a fresh
		// source-port binding from srcIP. Otherwise a pooled
		// connection from an earlier call could carry the wrong
		// source IP across tests.
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}, nil
}

// loginRaw POSTs to /api/auth/login using the provided client and
// returns the status, parsed envelope, and the raw body. It does not
// fail the test on non-2xx — callers that drive the rate limiter need
// to observe both 401s and 429s.
func loginRaw(t *testing.T, client *http.Client, srv *runningServer, username, password string) (int, envelope, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return doRaw(t, client, req)
}

// registerRaw POSTs to /api/auth/register using the provided client and
// returns the status, parsed envelope, and the raw body. Like loginRaw
// it does not fail on non-2xx.
func registerRaw(t *testing.T, client *http.Client, srv *runningServer, username, password, invite string) (int, envelope, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": invite,
	})
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/register", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new register request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return doRaw(t, client, req)
}

func doRaw(t *testing.T, client *http.Client, req *http.Request) (int, envelope, []byte) {
	t.Helper()
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
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
