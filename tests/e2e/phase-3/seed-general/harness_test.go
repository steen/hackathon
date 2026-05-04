// Package seed_general_e2e_test exercises the first-boot #general seed
// from outside the server binary. The tests boot apps/server with a fresh
// (or pre-populated) sqlite db and assert behavior only via the public
// REST surface; no internal packages are imported.
package seed_general_e2e_test

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
	"sync"
	"testing"
	"time"
)

type runningServer struct {
	httpURL    string
	port       int
	inviteCode string
	jwtSecret  string
	dbPath     string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// startServerWithDB launches apps/server pointed at the given dbPath. The
// caller controls dbPath so a single test can reuse the same file across
// two boots (the idempotency check).
func startServerWithDB(t *testing.T, dbPath string) *runningServer {
	t.Helper()

	bin := serverBinary(t)
	port := freePort(t)
	invite := randomSecret(t, 8)
	jwt := randomSecret(t, 32)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+jwt,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
		"CHAT_REGISTER_BURST=1000",
		"CHAT_REGISTER_REFILL=1s",
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

	return &runningServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
		inviteCode: invite,
		jwtSecret:  jwt,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
}

// stop shuts the server down and waits for the child to exit. Tests that
// need to reboot against the same db call this between boots.
func (s *runningServer) stop() {
	s.cancel()
	<-s.wait
}

var (
	serverBuildOnce sync.Once
	serverBuildPath string
	serverBuildErr  error
	binBuildBaseDir string
)

// TestMain owns the per-package temp directory that caches the compiled
// chat-server binary across this package's tests, so it is removed when
// the package's tests finish. Without it, every `go test` run on this
// package would leave a `phase-3-seed-general-bin-*` directory behind
// under $TMPDIR (see issue #463).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "phase-3-seed-general-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdir bin base: %v\n", err)
		os.Exit(1)
	}
	binBuildBaseDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func serverBinary(t *testing.T) string {
	t.Helper()
	serverBuildOnce.Do(func() {
		root := repoRoot(t)
		out := filepath.Join(binBuildBaseDir, "chat-server")
		build := exec.Command("go", "build", "-o", out, "./apps/server")
		build.Dir = root
		if combined, err := build.CombinedOutput(); err != nil {
			serverBuildErr = fmt.Errorf("go build ./apps/server: %w\n%s", err, combined)
			return
		}
		serverBuildPath = out
	})
	if serverBuildErr != nil {
		t.Fatalf("%v", serverBuildErr)
	}
	return serverBuildPath
}

// envelope is the {ok,data,error} response shape from PRD §10.
type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *envelopeError  `json:"error"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeEnvelope(t *testing.T, body []byte) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody=%s", err, body)
	}
	return env
}

// register POSTs /api/auth/register and returns the bearer token.
func register(t *testing.T, srv *runningServer, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	resp, err := http.Post(srv.httpURL+"/api/auth/register", "application/json", bytes.NewReader(body)) //nolint:gosec,noctx // test helper, loopback URL
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", resp.StatusCode, raw)
	}
	env := decodeEnvelope(t, raw)
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v\nbody=%s", err, raw)
	}
	if data.Token == "" {
		t.Fatalf("register: empty token in %s", raw)
	}
	return data.Token
}

type channelInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// listChannels GETs /api/channels and returns the parsed list.
func listChannels(t *testing.T, srv *runningServer, token string) []channelInfo {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.httpURL+"/api/channels", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list channels status=%d body=%s", resp.StatusCode, raw)
	}
	env := decodeEnvelope(t, raw)
	var data struct {
		Channels []channelInfo `json:"channels"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode channel list: %v\nbody=%s", err, raw)
	}
	return data.Channels
}

func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

func randomUsername(t *testing.T) string {
	t.Helper()
	return "u" + randomSecret(t, 6)
}

func randomPassword(t *testing.T) string {
	t.Helper()
	return randomSecret(t, 16)
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

// repoRoot walks up from this file until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", file)
		}
		dir = parent
	}
}
