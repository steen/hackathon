// Package cli_full_commands_e2e_test holds black-box E2E tests for the
// chatd CLI defined in specs/plans/phase-2/20-feature-cli-full-commands.md.
//
// Each test boots a fresh apps/server binary on a loopback port with a
// per-test SQLite DB and per-test JWT/invite secrets, then exec's the
// chatd binary against it. The package owns no mocks; the only public
// surface tested is the CLI's stdout/stderr/exit-code + the HTTP/WS
// behavior visible to a peer of the running server.
//
// Helpers in this file mirror the gold-standard pattern in
// tests/server-ws-hub/hub_test.go (startServer, randomSecret, freePort,
// waitForPort) and add CLI-specific helpers (chatd binary builder,
// chatdRun, REST setup helpers).
package cli_full_commands_e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"hackathon/tests/e2e/internal/clihelp"
)

// runningServer is the handle startServer returns. URL is the http://
// base; port is the listening port; inviteCode and jwtSecret are the
// per-test secrets so individual tests can reuse them when they need
// to drive REST setup directly.
type runningServer struct {
	url        string
	port       int
	inviteCode string
	jwtSecret  string
	dbPath     string
	cancel     context.CancelFunc
	wait       chan struct{}
}

// startServer builds apps/server (cached at package level) and
// launches it on a fresh port with fresh secrets and a fresh sqlite
// db. Registers a Cleanup hook that signals the server to exit.
func startServer(t *testing.T) *runningServer {
	t.Helper()

	bin := serverBinary(t)
	port := freePort(t)
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
	invite := randomSecret(t, 8)
	jwt := randomSecret(t, 32)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+jwt,
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
		url:        fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
		inviteCode: invite,
		jwtSecret:  jwt,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
}

// Build apps/server once per test binary so the per-test startServer
// calls are subprocess-launches, not compiles. The chatd binary is
// built and cached by clihelp.BuildChatd so the presence package and
// this one share a single compile per `go test ./...` run.
var (
	serverBuildOnce  sync.Once
	serverBuildPath  string
	serverBuildErr   error
	binBuildBaseDir  string
	binBuildBaseOnce sync.Once
)

func binBuildBase(t *testing.T) string {
	t.Helper()
	binBuildBaseOnce.Do(func() {
		// Use os.MkdirTemp — package-scoped, not bound to a specific t.
		dir, err := os.MkdirTemp("", "cli-full-commands-e2e-bin-*")
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

// chatdResult bundles stdout/stderr/exit-code of one chatd run so test
// assertions can keep noise off the call site.
type chatdResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// chatdRun execs chatd with the given args, an isolated XDG_CONFIG_HOME
// rooted at xdgDir, the given stdin, and the given extra env. The
// per-server --server flag is NOT injected automatically — tests pass
// it (or omit it) explicitly so the server-override AC has a place to
// vary.
//
// Returns (stdout, stderr, exit-code, runtime-error). The returned
// error is non-nil only for setup errors (cannot start subprocess);
// non-zero exit codes are reported via exitCode.
func chatdRun(t *testing.T, xdgDir string, stdin string, extraEnv []string, args ...string) chatdResult {
	t.Helper()
	bin := clihelp.BuildChatd(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir,
		"HOME="+xdgDir,
		// Pin CHATD_CONFIG_DIR off — tests should drive XDG path
		// resolution so AC-2's "stores in XDG_CONFIG_HOME/chatd" is
		// genuinely tested. extraEnv may re-override.
		"CHATD_CONFIG_DIR=",
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			err = nil // surface via exitCode, not err
		}
		// other launch error (binary missing, etc.) — surface via err
	}

	return chatdResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode,
		err:      err,
	}
}

// registerViaREST creates a user directly through the server's REST
// API, bypassing the CLI. Used to set up state in tests that focus on
// CLI behavior other than register itself. Returns the bearer token.
func registerViaREST(t *testing.T, srv *runningServer, username, password string) (token string, userID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	resp, err := http.Post(srv.url+"/api/auth/register", "application/json", bytes.NewReader(body)) //nolint:gosec,noctx // test helper, loopback URL
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
				ID       string `json:"id"`
				Username string `json:"username"`
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
	req, _ := http.NewRequest(http.MethodPost, srv.url+"/api/channels", bytes.NewReader(body))
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
	// Server envelope: {ok, data:{id, name, created_at}, error}.
	// (POST /api/channels returns the channel object inline as
	// `data`, not wrapped in a `channel` field — see
	// apps/server/internal/http/channels_handlers.go::Create.)
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

// postMessageViaREST posts a message and returns the message id.
func postMessageViaREST(t *testing.T, srv *runningServer, token, channelID, body string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"body": body})
	url := fmt.Sprintf("%s/api/channels/%s/messages", srv.url, channelID)
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
	// Server envelope: {ok, data:{id, body, sender_user_id,
	// created_at}, error}. POST returns the message inline as
	// `data` per messages_handlers.go::Create.
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode message envelope: %v\nbody=%s", err, raw)
	}
	return env.Data.ID
}

// listMessagesViaREST returns the message bodies in the channel,
// newest-first as the server returns them.
func listMessagesViaREST(t *testing.T, srv *runningServer, token, channelID string) []string {
	t.Helper()
	url := fmt.Sprintf("%s/api/channels/%s/messages", srv.url, channelID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list messages status = %d, body = %s", resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Messages []struct {
				Body string `json:"body"`
			} `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode messages envelope: %v\nbody=%s", err, raw)
	}
	out := make([]string, len(env.Data.Messages))
	for i, m := range env.Data.Messages {
		out[i] = m.Body
	}
	return out
}

// meStatus returns the HTTP status of GET /api/auth/me with the given
// bearer token. Used to confirm logout actually invalidates a token
// server-side.
func meStatus(t *testing.T, srv *runningServer, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.url+"/api/auth/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// configFile reads the JSON config file the chatd CLI persists at
// $XDG_CONFIG_HOME/chatd/config.json. Returns the parsed shape — the
// fields mirror apps/cli/internal/config/config.go.File but kept local
// so the test does not import internals.
type configFile struct {
	Server string `json:"server"`
	Token  string `json:"token"`
	User   *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

func readConfigFile(t *testing.T, xdgDir string) (*configFile, error) {
	t.Helper()
	path := filepath.Join(xdgDir, "chatd", "config.json")
	data, err := os.ReadFile(path) //nolint:gosec // test helper, path under per-test tempdir
	if err != nil {
		return nil, err
	}
	var cf configFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &cf, nil
}

// chatdRegisterViaFlags drives `chatd register` using --password /
// --invite-code so setup paths skip the prompt path. Mirrors the
// flag-based setup the in-package cmd tests use. Stays here rather
// than in clihelp because it wraps chatdRun, which is package-local.
func chatdRegisterViaFlags(t *testing.T, srv *runningServer, xdg, username, password string) {
	t.Helper()
	res := chatdRun(t, xdg, "", nil,
		"--server", srv.url, "register",
		"--password", password,
		"--invite-code", srv.inviteCode,
		username,
	)
	if res.exitCode != 0 {
		t.Fatalf("chatdRegisterViaFlags: exit=%d stderr=%q", res.exitCode, res.stderr)
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

// repoRoot walks up from this file until it finds go.mod. Lets every
// test resolve `go build` paths regardless of where `go test` is
// invoked from.
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
