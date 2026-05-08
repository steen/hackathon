// Package auth_endpoints_e2e_test holds black-box tests for the
// phase-1 auth feature (specs/plans/phase-1/feature-auth-endpoints.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets and a fresh sqlite DB, then drives the
// /api/auth/* endpoints through real HTTP. No internal-package imports
// from apps/** — the only coupling is the binary path passed to
// `go build` and the on-disk SQLite file we open read-only via the
// modernc.org/sqlite driver (already in go.mod).
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them).
package auth_endpoints_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// runningServer carries the per-test handle the helpers need to talk
// to the spawned chat-server.
type runningServer struct {
	httpURL    string // e.g. http://127.0.0.1:54321
	wsURL      string // e.g. ws://127.0.0.1:54321/ws
	port       int
	dbPath     string
	jwtSecret  string // hex; used by tests that mint their own JWTs
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// envelope mirrors the on-the-wire shape from apps/server/internal/http/errors.go.
// Tests use it via JSON so we stay decoupled from the production type.
type envelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *envelopeError   `json:"error"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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
//
// Do NOT call t.Parallel() in any test in this package while freePort
// works this way: two concurrent tests can land on the same port
// between Close here and the spawned server's Listen in startServer,
// flaking the suite. The fix is an FD-handoff (keep the listener open
// and pass it via cmd.ExtraFiles + LISTEN_FD=, with a small server-side
// hook) — see #196. Until that lands, every test in this file must run
// serially.
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

// repoRoot walks up from this file (.../tests/e2e/phase-1/auth-endpoints/harness_test.go)
// to the repo root (4 dirs up). Sanity-checked by stat-ing go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../tests/e2e/phase-1/auth-endpoints/harness_test.go ; up
	// five Dir() calls to the repo root.
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

// postJSON POSTs the given body to path with optional bearer. Returns
// the status, parsed envelope, and the raw body for callers that need
// the full bytes (e.g. byte-identical message comparisons).
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

// register POSTs /api/auth/register with the right invite code, fails
// the test on a non-2xx, and returns the user id and freshly-issued
// token from the success envelope.
// seededGeneralChannelID returns the ULID of the seeded "general"
// channel. The WS handler now requires ?channel=<id>; tests that need
// a successful upgrade dial against this id. Caller supplies a bearer
// token because /api/channels is auth-gated.
func seededGeneralChannelID(t *testing.T, srv *runningServer, bearer string) string {
	t.Helper()
	status, env, raw := getJSON(t, srv, "/api/channels", bearer)
	if status != http.StatusOK || !env.OK || env.Data == nil {
		t.Fatalf("GET /api/channels: status %d body %s", status, raw)
	}
	var data struct {
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channels: %v body=%s", err, raw)
	}
	for _, c := range data.Channels {
		if c.Name == "general" {
			return c.ID
		}
	}
	t.Fatalf("seeded 'general' channel not found in %s", raw)
	return ""
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

// login POSTs /api/auth/login and returns the token on a 200 envelope,
// failing the test on any non-2xx.
func login(t *testing.T, srv *runningServer, username, password string) string {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if status != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", username, status, raw)
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode login data: %v body=%s", err, raw)
	}
	if data.Token == "" {
		t.Fatalf("login %s: empty token (body=%s)", username, raw)
	}
	return data.Token
}

// decodeJWTPayload base64url-decodes the second segment of the JWT and
// returns the parsed claim map. We do not verify the signature — these
// tests trust the production code to sign correctly and only inspect
// the claim shape.
func decodeJWTPayload(t *testing.T, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT (segments=%d): %q", len(parts), tok)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("base64-decode JWT payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("json-decode JWT payload: %v", err)
	}
	return claims
}

// openDBReadOnly opens the running server's SQLite file in read-only,
// no-mutex mode so the test can SELECT without contending with the
// server's writes. Caller defers Close().
func openDBReadOnly(t *testing.T, srv *runningServer) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: srv.dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
