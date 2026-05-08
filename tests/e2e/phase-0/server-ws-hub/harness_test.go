// Package server_ws_hub_e2e_test holds black-box E2E tests for
// specs/plans/phase-0/feature-server-ws-hub.md. The tests boot the
// real apps/server binary on a loopback port and assert behavior
// only via its public network surface (/ws, /debug/subs, REST).
//
// Helpers in this file mirror the gold-standard pattern in
// tests/server-ws-hub/hub_test.go (intentionally copied, not imported,
// because that test's package is server_ws_hub_test and helpers are
// local).
package server_ws_hub_e2e_test

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

// runningServer is the handle startServer / startServerWithDB return.
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

// startServer launches apps/server on a fresh loopback port with random
// secrets and NO CHAT_DB_PATH. In this configuration repository == nil
// in main.go, so /ws does not enforce ws-ticket auth — the phase-0
// boot path the spec describes.
func startServer(t *testing.T) *runningServer {
	t.Helper()
	return launch(t, false)
}

// startServerWithDB launches apps/server with a per-test sqlite db and
// the auth/REST surface mounted. Used by AC-3 positive to exercise the
// REST producer path.
func startServerWithDB(t *testing.T) *runningServer {
	t.Helper()
	return launch(t, true)
}

func launch(t *testing.T, withDB bool) *runningServer {
	t.Helper()

	bin := serverBinary(t)
	port := freePort(t)
	invite := randomSecret(t, 8)
	jwt := randomSecret(t, 32)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	env := append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwt,
		"CHAT_INVITE_CODE="+invite,
	)
	dbPath := ""
	if withDB {
		dbPath = filepath.Join(t.TempDir(), "chatd.sqlite")
		env = append(env, "CHAT_DB_PATH="+dbPath)
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
		inviteCode: invite,
		jwtSecret:  jwt,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
}

// Build apps/server once per test binary so per-test launches are
// subprocess execs, not cold compiles.
var (
	serverBuildOnce sync.Once
	serverBuildPath string
	serverBuildErr  error
	binBuildBaseDir string
	binBuildBaseSet sync.Once
)

func binBuildBase(t *testing.T) string {
	t.Helper()
	binBuildBaseSet.Do(func() {
		dir, err := os.MkdirTemp("", "phase-0-server-ws-hub-bin-*")
		if err != nil {
			t.Fatalf("mkdir bin base: %v", err)
		}
		binBuildBaseDir = dir
	})
	return binBuildBaseDir
}

func serverBinary(t *testing.T) string {
	t.Helper()
	serverBuildOnce.Do(func() {
		root := repoRoot(t)
		out := filepath.Join(binBuildBase(t), "chat-server")
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

// fetchSubscriberCount queries /debug/subs and parses "<n>\n" into an
// int. Mirrors tests/server-ws-hub/hub_test.go::fetchSubscriberCount.
func fetchSubscriberCount(url string) (int, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper, loopback URL
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var count int
	if _, err := fmt.Fscanf(resp.Body, "%d", &count); err != nil {
		return 0, err
	}
	return count, nil
}

// waitForSubscriberCount polls /debug/subs?channel=<channel> until the
// count matches want or the deadline passes.
func waitForSubscriberCount(t *testing.T, srv *runningServer, channel string, want int, timeout time.Duration) {
	t.Helper()
	url := fmt.Sprintf("%s/debug/subs?channel=%s", srv.httpURL, encodeQuery(channel))
	deadline := time.Now().Add(timeout)
	var last int
	for {
		got, err := fetchSubscriberCount(url)
		if err == nil {
			last = got
			if got == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("debug/subs?channel=%s = %d after %s; want %d (last err=%v)", channel, last, timeout, want, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// encodeQuery percent-encodes a channel name for use in the
// /debug/subs?channel=<...> URL. Only encodes the characters that
// appear in legal channel ids: '#' (defaultChannel sentinel) and
// alphanumerics + '-'. Avoids pulling in net/url just to call
// QueryEscape on a known-shape literal.
func encodeQuery(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b = append(b, c)
			continue
		}
		b = append(b, '%', hexDigit(c>>4), hexDigit(c&0x0f))
	}
	return string(b)
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'A' + (b - 10)
}

// registerViaREST creates a user through the server's /api/auth/register
// endpoint and returns the bearer token + user id. Used by AC-3
// positive only (REST producer needs auth).
func registerViaREST(t *testing.T, srv *runningServer, username, password string) (token, userID string) {
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
		t.Fatalf("register status = %d, body = %s", resp.StatusCode, raw)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Token string `json:"token"`
			User  struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode register envelope: %v\nbody=%s", err, raw)
	}
	if !env.OK || env.Data.Token == "" {
		t.Fatalf("register did not return token: %s", raw)
	}
	return env.Data.Token, env.Data.User.ID
}

// createChannelViaREST creates a channel and returns its id.
func createChannelViaREST(t *testing.T, srv *runningServer, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/api/channels", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/channels: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create channel status = %d, body = %s", resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode channel envelope: %v\nbody=%s", err, raw)
	}
	if env.Data.ID == "" {
		t.Fatalf("create channel: empty id in %s", raw)
	}
	return env.Data.ID
}

// mintWSTicket calls POST /api/auth/ws-ticket and returns the one-shot
// ticket string for use as ws://.../ws?ticket=<...>.
func mintWSTicket(t *testing.T, srv *runningServer, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/ws-ticket", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/ws-ticket: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ws-ticket status = %d, body = %s", resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode ws-ticket envelope: %v\nbody=%s", err, raw)
	}
	if env.Data.Ticket == "" {
		t.Fatalf("ws-ticket: empty ticket in %s", raw)
	}
	return env.Data.Ticket
}

// postMessageViaREST POSTs a message to /api/channels/{id}/messages.
func postMessageViaREST(t *testing.T, srv *runningServer, token, channelID, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"body": body})
	url := fmt.Sprintf("%s/api/channels/%s/messages", srv.httpURL, channelID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post message status = %d, body = %s", resp.StatusCode, raw)
	}
}

// randomSecret returns a hex string sized for the SEC-1 minimum.
// Mirrors tests/server-ws-hub/hub_test.go::randomSecret.
func randomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// randomUsername generates a server-regex-legal username.
func randomUsername(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "u" + string(b[:11])
}

// randomChannelName matches the server's `^[a-z0-9][a-z0-9-]{0,39}$`
// regex.
func randomChannelName(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "c" + string(b[:9])
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
