// Package logging_and_error_envelope_e2e_test holds black-box tests for
// the phase-1 logging-and-error-envelope feature
// (specs/plans/phase-1/feature-logging-and-error-envelope.md).
//
// Each test boots the production chat-server binary on a free loopback
// port with random secrets, captures the binary's stderr into a thread-
// safe buffer, and drives real HTTP against it. Assertions about access
// logs read out of that buffer.
//
// Helpers live in this file rather than a shared `tests/e2e/internal/`
// package because there is no third call site yet (CLAUDE.md: no shared
// abstractions until 3+ features need them).
package logging_and_error_envelope_e2e_test

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
	"strings"
	"sync"
	"testing"
	"time"
)

// runningServer carries the per-test handle the helpers need to talk to
// the spawned chat-server.
type runningServer struct {
	httpURL    string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	logs       *syncBuf
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

// syncBuf is a goroutine-safe io.Writer the test uses to capture the
// server's stderr. The server writes from its own goroutines; the test
// reads while the request flow is in flight.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// String returns a snapshot of the buffer contents.
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
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

// repoRoot walks five Dir() calls up from this file
// (.../tests/e2e/phase-1/logging-and-error-envelope/harness_test.go) to
// the repo root and stat-checks go.mod.
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

// startServerOpts lets a test override the default `go build` line.
// AC-4's panic half (internal_detail_redaction_test.go) sets
// BuildTags = "panicprobe" so the binary registers /debug/panic from
// apps/server/internal/wiring/panicprobe.go (the dead-code-by-default
// route gated behind `//go:build panicprobe`).
type startServerOpts struct {
	// TODO(#306): BuildTags is the call-site link to the panicprobe
	// wiring tracked at #306. A `git grep -n '#306'` lands here.
	BuildTags string
}

// startServer builds apps/server, picks a free port, starts the binary
// with random secrets and a tempdir DB, captures stderr into srv.logs,
// and registers a Cleanup that stops it. Returns once the port is
// listening. Variadic opts keeps the existing zero-arg call sites
// untouched while allowing AC-4's panic sub-test to opt into the
// panicprobe build tag.
func startServer(t *testing.T, opts ...startServerOpts) *runningServer {
	t.Helper()

	var opt startServerOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	buildArgs := []string{"build"}
	if opt.BuildTags != "" {
		buildArgs = append(buildArgs, "-tags="+opt.BuildTags)
	}
	buildArgs = append(buildArgs, "-o", binPath, "./apps/server")
	build := exec.Command("go", buildArgs...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	port := freePort(t)
	jwtSecret := randomSecret(t, 32)
	invite := randomSecret(t, 8)
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")

	logs := &syncBuf{}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
	)
	// AccessLog uses the stdlib log package whose default destination is
	// stderr; capture both streams to be safe.
	cmd.Stdout = io.MultiWriter(os.Stderr, logs)
	cmd.Stderr = io.MultiWriter(os.Stderr, logs)
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
		logs:       logs,
		cancel:     cancel,
		wait:       wait,
	}
}

func postJSON(t *testing.T, srv *runningServer, path, bearer string, body any) (int, http.Header, envelope, []byte) {
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

func getRaw(t *testing.T, srv *runningServer, path, bearer string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.httpURL+path, nil)
	if err != nil {
		t.Fatalf("new GET %s: %v", path, err)
	}
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
	return resp.StatusCode, resp.Header, raw
}

func getJSON(t *testing.T, srv *runningServer, path, bearer string) (int, http.Header, envelope, []byte) {
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

func doRequest(t *testing.T, req *http.Request) (int, http.Header, envelope, []byte) {
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
	return resp.StatusCode, resp.Header, env, raw
}

func register(t *testing.T, srv *runningServer, username, password string) (userID, token string) {
	t.Helper()
	status, _, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
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

// awaitLogLine polls srv.logs for a line containing every needle in
// order on the same line. Returns the matched line on success; fatal on
// timeout (with the captured log dumped for the test author).
func awaitLogLine(t *testing.T, srv *runningServer, needles []string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := srv.logs.String()
		for _, line := range strings.Split(s, "\n") {
			if containsAll(line, needles) {
				return line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("did not see access log with needles %v within %s; captured logs:\n%s", needles, timeout, srv.logs.String())
	return ""
}

func containsAll(line string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(line, n) {
			return false
		}
	}
	return true
}
