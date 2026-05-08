// Package channels_and_messages_e2e_test holds black-box E2E tests for
// specs/plans/phase-1/feature-channels-and-messages.md. The tests boot
// the real apps/server binary on a loopback port with a fresh sqlite
// db and exercise behavior only via its public network surface
// (REST under /api/* and WS under /ws).
//
// Helpers in this file mirror the gold-standard pattern from
// tests/server-ws-hub/hub_test.go and tests/e2e/phase-0/server-ws-hub/
// (intentionally copied, not imported, because each test directory's
// helpers are intentionally local to that package).
package channels_and_messages_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
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
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // read-only DB inspection from tests
)

type runningServer struct {
	httpURL    string
	wsURL      string
	port       int
	inviteCode string
	jwtSecret  string
	dbPath     string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// startServer launches apps/server with a per-test sqlite db, random
// secrets, and a relaxed register rate-limit so each test can register
// the few users it needs without tripping the per-IP bucket. The
// CHAT_REGISTER_BURST/CHAT_REGISTER_REFILL knobs are documented in
// apps/server/internal/ratelimit/config.go for this exact purpose.
func startServer(t *testing.T) *runningServer {
	t.Helper()

	bin := serverBinary(t)
	port := freePort(t)
	invite := randomSecret(t, 8)
	jwt := randomSecret(t, 32)
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
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

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &runningServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL:      fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:       port,
		inviteCode: invite,
		jwtSecret:  jwt,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
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
// package would leave a `phase-1-channels-and-messages-bin-*` directory
// behind under $TMPDIR (see issue #307).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "phase-1-channels-and-messages-bin-*")
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

// register POSTs /api/auth/register and returns (token, userID).
func register(t *testing.T, srv *runningServer, username, password string) (string, string) {
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
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v\nbody=%s", err, raw)
	}
	if data.Token == "" || data.User.ID == "" {
		t.Fatalf("register: empty token/id in %s", raw)
	}
	return data.Token, data.User.ID
}

// authedRequest issues an HTTP request with a Bearer token and returns
// the response. It does NOT close the body — the caller does.
func authedRequest(t *testing.T, srv *runningServer, token, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.httpURL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// readBody reads + closes the response body and returns the bytes.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return raw
}

// channelInfo is the shape returned by POST /api/channels and entries in
// GET /api/channels.
type channelInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// createChannel POSTs and asserts 201; returns the channel info.
func createChannel(t *testing.T, srv *runningServer, token, name string) channelInfo {
	t.Helper()
	status, raw := createChannelRaw(t, srv, token, name)
	if status != http.StatusCreated {
		t.Fatalf("create channel %q: status=%d body=%s", name, status, raw)
	}
	env := decodeEnvelope(t, raw)
	var ch channelInfo
	if err := json.Unmarshal(env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v\nbody=%s", err, raw)
	}
	if ch.ID == "" || ch.Name != name {
		t.Fatalf("create channel %q: bad payload %s", name, raw)
	}
	return ch
}

// createChannelRaw returns status + body without asserting.
func createChannelRaw(t *testing.T, srv *runningServer, token, name string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	resp := authedRequest(t, srv, token, http.MethodPost, "/api/channels", body)
	raw := readBody(t, resp)
	return resp.StatusCode, raw
}

// listChannels GETs /api/channels and returns the parsed list.
func listChannels(t *testing.T, srv *runningServer, token string) []channelInfo {
	t.Helper()
	resp := authedRequest(t, srv, token, http.MethodGet, "/api/channels", nil)
	raw := readBody(t, resp)
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

// messageInfo mirrors repo.Message JSON tags.
type messageInfo struct {
	ID           string    `json:"id"`
	ChannelID    string    `json:"channel_id"`
	SenderUserID string    `json:"sender_user_id"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

// sendMessage POSTs /api/channels/{id}/messages and returns the message.
func sendMessage(t *testing.T, srv *runningServer, token, channelID, body string) messageInfo {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp := authedRequest(t, srv, token, http.MethodPost,
		fmt.Sprintf("/api/channels/%s/messages", channelID), payload)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post message status=%d body=%s", resp.StatusCode, raw)
	}
	env := decodeEnvelope(t, raw)
	var msg messageInfo
	if err := json.Unmarshal(env.Data, &msg); err != nil {
		t.Fatalf("decode message: %v\nbody=%s", err, raw)
	}
	return msg
}

// listMessagesOpts is a small option bag for the messages listing
// endpoint; both fields are optional.
type listMessagesOpts struct {
	limit  int
	before string
}

// listMessages GETs the per-channel history with optional ?before= and
// ?limit= and returns the parsed slice.
func listMessages(t *testing.T, srv *runningServer, token, channelID string, opts listMessagesOpts) []messageInfo {
	t.Helper()
	q := url.Values{}
	if opts.limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.limit))
	}
	if opts.before != "" {
		q.Set("before", opts.before)
	}
	path := fmt.Sprintf("/api/channels/%s/messages", channelID)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	resp := authedRequest(t, srv, token, http.MethodGet, path, nil)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list messages status=%d body=%s", resp.StatusCode, raw)
	}
	env := decodeEnvelope(t, raw)
	var data struct {
		Messages []messageInfo `json:"messages"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode messages list: %v\nbody=%s", err, raw)
	}
	return data.Messages
}

// mintWSTicket calls POST /api/auth/ws-ticket and returns the one-shot
// ticket string.
func mintWSTicket(t *testing.T, srv *runningServer, token string) string {
	t.Helper()
	resp := authedRequest(t, srv, token, http.MethodPost, "/api/auth/ws-ticket", nil)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ws-ticket status=%d body=%s", resp.StatusCode, raw)
	}
	env := decodeEnvelope(t, raw)
	var data struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode ws-ticket data: %v\nbody=%s", err, raw)
	}
	if data.Ticket == "" {
		t.Fatalf("ws-ticket: empty in %s", raw)
	}
	return data.Ticket
}

// openDBReadOnly opens the per-test sqlite file in read-only mode.
// Tests use this for direct row assertions without going through the
// REST surface, e.g. confirming that a rejected POST left no row behind.
func openDBReadOnly(t *testing.T, srv *runningServer) *sql.DB {
	t.Helper()
	dsn := "file:" + srv.dbPath + "?mode=ro&_pragma=busy_timeout(2000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// randomSecret returns a hex string of byteLen*2 chars.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// randomUsername generates a username matching the server's regex.
func randomUsername(t *testing.T) string {
	t.Helper()
	return "u" + randomSecret(t, 6) // 12 hex chars + 'u' = 13 chars, all [a-z0-9]
}

// randomChannelName matches the server's `^[a-z0-9][a-z0-9-]{0,39}$`.
func randomChannelName(t *testing.T) string {
	t.Helper()
	return "c" + randomSecret(t, 6)
}

// randomPassword returns a hex string long enough to satisfy the
// server's 10-byte minimum policy.
func randomPassword(t *testing.T) string {
	t.Helper()
	return randomSecret(t, 16) // 32 hex chars
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
