// Package body_and_ws_caps_e2e_test holds black-box tests for the
// phase-1 body-and-ws-caps feature
// (specs/plans/phase-1/feature-body-and-ws-caps.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets, then drives the /ws and /api/* endpoints
// through real network clients. No internal-package imports from
// apps/** — the only coupling is the binary path passed to `go build`.
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them). The pattern mirrors
// tests/e2e/phase-1/auth-endpoints/harness_test.go.
package body_and_ws_caps_e2e_test

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
	wsURL      string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// randomSecret returns a hex string of byteLen random bytes (so the
// printed string is 2*byteLen chars). Per CLAUDE.md "No hardcoded
// secrets" — every JWT secret and invite code in this test tree is
// process-local.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// freePort asks the kernel for a free TCP port on loopback and returns
// it. There is a tiny race window between Close and the server's
// Listen; in practice this loopback path doesn't lose the port.
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

// repoRoot walks up from this file to the repo root (5 dirs up).
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
// with random secrets and a fresh sqlite DB, and registers a Cleanup
// that stops it. CHAT_DB_PATH is required at startup since phase-0 boot
// mode was removed; tests that need a /ws upgrade go through the seeded
// general channel ULID via seededChannelID.
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

// envelope mirrors the wire-shape envelope from internal/http.
type envelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// registerAndMintTicket registers a fresh user and returns (bearer, ticket).
// Each call mints its own ticket because tickets are one-shot.
func registerAndMintTicket(t *testing.T, srv *runningServer) (bearer, ticket string) {
	t.Helper()
	username := "u-" + randomSecret(t, 4)
	password := randomSecret(t, 12)
	regBody, _ := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	resp, err := http.Post(srv.httpURL+"/api/auth/register", "application/json", bytes.NewReader(regBody)) //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("register: status %d body %s", resp.StatusCode, raw)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode register envelope: %v body=%s", err, raw)
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	bearer = data.Token

	// Mint ws-ticket.
	req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/ws-ticket", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	tResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ws-ticket: %v", err)
	}
	defer tResp.Body.Close()
	tRaw, _ := io.ReadAll(tResp.Body)
	if tResp.StatusCode != http.StatusOK {
		t.Fatalf("ws-ticket: status %d body %s", tResp.StatusCode, tRaw)
	}
	var tEnv envelope
	if err := json.Unmarshal(tRaw, &tEnv); err != nil {
		t.Fatalf("decode ws-ticket envelope: %v body=%s", err, tRaw)
	}
	var tData struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(*tEnv.Data, &tData); err != nil {
		t.Fatalf("decode ws-ticket data: %v body=%s", err, tRaw)
	}
	ticket = tData.Ticket
	return bearer, ticket
}

// seededChannelID returns the ULID of the seeded "general" channel.
func seededChannelID(t *testing.T, srv *runningServer, bearer string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.httpURL+"/api/channels", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/channels: status %d body %s", resp.StatusCode, raw)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode /api/channels envelope: %v body=%s", err, raw)
	}
	var data struct {
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/channels: %v body=%s", err, raw)
	}
	for _, c := range data.Channels {
		if c.Name == "general" {
			return c.ID
		}
	}
	t.Fatalf("seeded 'general' channel not found in %s", raw)
	return ""
}
