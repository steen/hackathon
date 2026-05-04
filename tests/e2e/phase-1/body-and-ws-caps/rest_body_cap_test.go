package body_and_ws_caps_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// AC-4 (verbatim from specs/plans/phase-1/feature-body-and-ws-caps.md):
//
//	REST request bodies are capped at 16 KiB; oversized bodies return 413.
//
// The cap lives in apps/server/internal/http/limits.go (BodyCap →
// http.MaxBytesReader at RESTBodyLimit = 16*1024). The middleware is
// wired in apps/server/internal/wiring/wiring.go just inside Recover so
// every mux route inherits it.
//
// Three boundary assertions, all against the production chat-server
// binary across the loopback wire:
//
//   - over_limit: POST /api/auth/register with a 16385-byte body (one
//     byte past RESTBodyLimit) → HTTP 413 with the canonical envelope
//     {"ok":false,"error":{"code":"body_too_large", ...}}.
//
//   - at_limit: POST /api/auth/register with exactly 16384 bytes of
//     parseable JSON → not 413, and the handler actually answers (2xx
//     or non-413 4xx). The body is a valid register envelope padded
//     via invite_code so json.Decode succeeds — a raw xxx... payload
//     would 400 on JSON parse before the cap was meaningfully
//     exercised against the handler's own decode. The fixture's
//     random invite_code does not match CHAT_INVITE_CODE, so Register
//     returns 403; the assertion accepts any 2xx-4xx other than 413.
//
//   - 413_carries_security_headers: the SecurityHeaders middleware is
//     outermost (wiring.go: SecurityHeaders(... BodyCap(mux))), so
//     even the 413 written by BodyCap must carry SEC-10 headers
//     (Content-Security-Policy, X-Content-Type-Options). This pins
//     the middleware ordering — if BodyCap were ever moved outside
//     SecurityHeaders, the 413 would leak unheaded and this assertion
//     would catch it.
//
// startServerWithDB is local to this test because the shared
// startServer in harness_test.go intentionally omits CHAT_DB_PATH for
// the AC-1 WS path. Auth handlers are skipped when Repo is nil
// (apps/server/internal/wiring/auth.go), so /api/auth/register would
// 404 instead of being a stable target for the at_limit case. We need
// the route to be wired so the at_limit body actually reaches the
// register handler.
func TestAC4_RESTBodyOver16KiBReturns413(t *testing.T) {
	srv := startServerWithDB(t)

	t.Run("over_limit_returns_413", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), int(16*1024+1))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp := postBytes(ctx, t, srv.httpURL+"/api/auth/register", body)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413; over-cap body should hit BodyCap before any handler", resp.StatusCode)
		}

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var env struct {
			OK    bool `json:"ok"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope: %v\nbody=%q", err, raw)
		}
		if env.OK {
			t.Fatalf("envelope ok = true; want false on 413; body=%q", raw)
		}
		// PRD §10 envelope shape; the canonical code is set in
		// limits.go WriteBodyTooLarge. Pin both ok=false and
		// code=body_too_large so a future rename breaks here loudly.
		if env.Error.Code != "body_too_large" {
			t.Fatalf("envelope code = %q, want %q", env.Error.Code, "body_too_large")
		}
		if env.Error.Message == "" {
			t.Fatalf("envelope message empty; PRD §10 requires a non-empty user-safe message")
		}
	})

	t.Run("at_limit_16KiB_not_capped", func(t *testing.T) {
		// A parseable JSON envelope of exactly 16384 bytes. Raw
		// xxx... would 400 on JSON parse before exercising the cap
		// against any handler-side decode; pad invite_code instead so
		// json.Decode succeeds and the request reaches Register.
		// Username and password fields are within their own caps
		// (3-32 chars; ≤72 bytes), so invite_code carries the slack.
		body := atLimitRegisterBody(t, 16*1024)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp := postBytes(ctx, t, srv.httpURL+"/api/auth/register", body)
		defer func() { _ = resp.Body.Close() }()
		// Drain body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)

		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			t.Fatalf("status = 413 at exactly 16 KiB; cap fired one byte too early — RESTBodyLimit must be inclusive of 16384")
		}
		// Stronger invariant than just "not 413": a parseable JSON
		// body must reach the handler. The fixture supplies a random
		// (wrong) invite_code via randomSecret(t, 8) — 8 random bytes
		// hex-encoded to 16 chars, ~2^64 collision space against the
		// server's per-process CHAT_INVITE_CODE — so Register answers
		// 403. A 2xx here would indicate a fixture regression (e.g.
		// the helper degenerating to a constant), not flake. 5xx (or
		// anything outside 2xx-4xx) means the handler did not produce
		// the response.
		if resp.StatusCode < 200 || resp.StatusCode >= 500 {
			t.Fatalf("status = %d at 16 KiB; want 2xx or non-413 4xx (parseable body must reach handler)", resp.StatusCode)
		}
	})

	t.Run("413_carries_security_headers", func(t *testing.T) {
		// Same over-cap POST as the first sub-test, but assert headers
		// from the outermost middleware survive the inner-middleware
		// 413. wiring.go currently composes:
		//   SecurityHeaders → RequestID → AccessLog → Recover → BodyCap → mux
		// so a 413 from BodyCap must still carry CSP + nosniff. If a
		// refactor ever moved BodyCap outside SecurityHeaders, this
		// assertion would catch it before the bad ordering shipped.
		body := bytes.Repeat([]byte("x"), int(16*1024+1))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp := postBytes(ctx, t, srv.httpURL+"/api/auth/register", body)
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413 (preconditions for header check)", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Security-Policy"); got == "" {
			t.Fatalf("Content-Security-Policy header missing on 413; SecurityHeaders is no longer outside BodyCap")
		} else if !strings.Contains(got, "default-src 'self'") {
			t.Fatalf("Content-Security-Policy = %q; want PRD §9 baseline starting with default-src 'self'", got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
	})
}

// postBytes posts raw bytes to url with Content-Type: application/json.
// We bypass any auth helper because the over-cap path must reach
// BodyCap before any handler-side parsing — this matches what a real
// client (browser, curl) would send.
func postBytes(ctx context.Context, t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	c := &http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// atLimitRegisterBody returns a JSON-parseable register payload whose
// serialized length is exactly size bytes. Username and password are
// fixed within their handler-side caps (3-32 chars; ≤72 bytes); the
// invite_code field absorbs the slack so the body decodes cleanly and
// the request reaches Register instead of bouncing on JSON parse.
//
// Letter-only padding keeps the body identical to the previous
// xxx-style fixture in spirit (single-byte ASCII, no escapes that
// would invalidate the byte count).
func atLimitRegisterBody(t *testing.T, size int) []byte {
	t.Helper()
	const (
		username = "atlimit"
		password = "abcdefghij"
		// registerPasswordMinLen mirrors apps/server/internal/auth/constants.go
		// PasswordMinLen (=10). Duplicated locally because this E2E lives
		// outside apps/server/internal/, so the production const is not
		// importable. If the handler-side minimum grows past this and the
		// fixture password is not lengthened to match, Register would 400
		// on policy instead of 403 on invite_code — the test would still
		// pass the [200, 500) window but the invariant under test
		// (parseable JSON reaches Register and produces 403) would
		// silently shift. The guard below catches that drift loudly.
		registerPasswordMinLen = 10
	)
	if len(password) < registerPasswordMinLen {
		t.Fatalf("atLimitRegisterBody: fixture password=%d chars, shorter than handler-side PasswordMinLen=%d; bump the fixture (and absorb the slack from invite_code padding) so Register sees policy-valid credentials and answers 403 on invite_code", len(password), registerPasswordMinLen)
	}
	prefix := fmt.Sprintf(`{"username":%q,"password":%q,"invite_code":"`, username, password)
	const suffix = `"}`
	overhead := len(prefix) + len(suffix)
	if size < overhead {
		t.Fatalf("atLimitRegisterBody: size=%d smaller than envelope overhead=%d", size, overhead)
	}
	pad := bytes.Repeat([]byte("a"), size-overhead)
	body := make([]byte, 0, size)
	body = append(body, prefix...)
	body = append(body, pad...)
	body = append(body, suffix...)
	if len(body) != size {
		t.Fatalf("atLimitRegisterBody: built %d bytes, want %d", len(body), size)
	}
	if !json.Valid(body) {
		t.Fatalf("atLimitRegisterBody: produced invalid JSON")
	}
	return body
}

// startServerWithDB mirrors startServer (harness_test.go) but adds
// CHAT_DB_PATH so /api/auth/register and /api/auth/login are wired.
// Local to this file rather than promoted to the shared harness — only
// AC-4 needs the auth surface live. AC-1/AC-2 dial /ws without a
// ticket, which the no-DB harness keeps verifiably auth-free.
func startServerWithDB(t *testing.T) *runningServer {
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
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+randomSecret(t, 32),
		"CHAT_INVITE_CODE="+randomSecret(t, 8),
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
		httpURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL:   fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		port:    port,
		cancel:  cancel,
		wait:    wait,
	}
}
