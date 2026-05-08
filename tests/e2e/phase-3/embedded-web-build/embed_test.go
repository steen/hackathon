// Package embedded_web_build_e2e_test holds black-box E2E coverage for
// specs/plans/phase-3/20-feature-embedded-web-build.md: the chat-server
// binary embeds the apps/web Vite build via //go:embed and serves it
// from non-API paths with SPA fallback.
//
// The tests build the production server binary and assert against its
// real HTTP surface. They rely on the placeholder index.html committed
// at apps/server/internal/web/dist/index.html — which is what a plain
// `go build ./apps/server` (no preceding `pnpm --filter web build`)
// embeds. That placeholder carries enough HTML structure to make the
// SPA-fallback contract observable: a <!doctype html> prefix and a
// "<div id=\"root\"></div>" mount point. Tests that exercise the real
// SPA bundle live in the e2e Playwright suite.
package embedded_web_build_e2e_test

import (
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
	"testing"
	"time"
)

// runningServer holds the per-test handle plus the secrets a sub-test
// might need to drive register/login. The embedded-web suite focuses
// on static-asset routing, so most tests don't need them — they're
// here to mirror the established harness pattern.
type runningServer struct {
	httpURL    string
	port       int
	dbPath     string
	jwtSecret  string
	inviteCode string
	cancel     context.CancelFunc
	wait       chan struct{}
}

type errEnvelope struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
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

// repoRoot walks 5 dirs up from this file
// (.../tests/e2e/phase-3/embedded-web-build/embed_test.go) to the
// repo root, then sanity-checks by stat-ing go.mod.
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
		port:       port,
		dbPath:     dbPath,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

// getRaw issues GET path against srv and returns (status, body, content-type).
func getRaw(t *testing.T, srv *runningServer, path string) (int, []byte, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.httpURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, body, resp.Header.Get("Content-Type")
}

// TestRootPathServesEmbeddedIndexHTML — AC: GET / returns the embedded
// SPA's index.html with HTML content-type.
func TestRootPathServesEmbeddedIndexHTML(t *testing.T) {
	srv := startServer(t)

	status, body, ct := getRaw(t, srv, "/")
	if status != http.StatusOK {
		t.Fatalf("GET /: status=%d body=%q", status, body)
	}
	if !strings.HasPrefix(strings.ToLower(ct), "text/html") {
		t.Fatalf("GET /: Content-Type=%q, want text/html*", ct)
	}
	low := strings.ToLower(string(body))
	if !strings.Contains(low, "<!doctype html>") {
		t.Fatalf("GET / body missing <!doctype html>: %q", body)
	}
	if !strings.Contains(low, `id="root"`) {
		t.Fatalf("GET / body missing SPA root mount point id=\"root\": %q", body)
	}
}

// TestUnknownSPAPathFallsBackToIndexHTML — AC: an arbitrary deep link
// that has no matching file in the embedded FS still returns
// index.html so the SPA's client-side router can resolve it.
func TestUnknownSPAPathFallsBackToIndexHTML(t *testing.T) {
	srv := startServer(t)

	// Get the canonical / response so the deep-link response can be
	// compared byte-for-byte. Two GETs against the placeholder must
	// produce the same body.
	rootStatus, rootBody, _ := getRaw(t, srv, "/")
	if rootStatus != http.StatusOK {
		t.Fatalf("GET / setup: status=%d", rootStatus)
	}

	for _, p := range []string{"/c/general", "/login", "/some/deep/spa/route", "/this-does-not-exist"} {
		status, body, ct := getRaw(t, srv, p)
		if status != http.StatusOK {
			t.Errorf("GET %s: status=%d, want 200 (SPA fallback) body=%q", p, status, body)
			continue
		}
		if !strings.HasPrefix(strings.ToLower(ct), "text/html") {
			t.Errorf("GET %s: Content-Type=%q, want text/html*", p, ct)
		}
		if string(body) != string(rootBody) {
			t.Errorf("GET %s: body differs from /; want SPA fallback to serve index.html identically.\ngot=%q\nwant=%q",
				p, body, rootBody)
		}
	}
}

// TestAPIPathsAreNotShadowedByStaticHandler — AC: a request under the
// /api/, /ws/, or /debug/ prefix that does not match any registered
// route must NOT be rewritten to the SPA HTML; machine clients expect
// a parseable JSON envelope on these prefixes.
func TestAPIPathsAreNotShadowedByStaticHandler(t *testing.T) {
	srv := startServer(t)

	cases := []struct {
		name string
		path string
	}{
		{"unknown_api_path", "/api/does-not-exist"},
		{"unknown_api_subtree", "/api/v9/futures"},
		{"unknown_ws_subtree", "/ws/extra"},
		{"unknown_debug_subtree", "/debug/extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, ct := getRaw(t, srv, tc.path)
			if status != http.StatusNotFound {
				t.Fatalf("GET %s: status=%d, want 404 (must not fall through to SPA HTML)\nbody=%q",
					tc.path, status, body)
			}
			if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
				t.Fatalf("GET %s: Content-Type=%q, want application/json* (SPA shadow check)",
					tc.path, ct)
			}
			low := strings.ToLower(string(body))
			if strings.Contains(low, "<!doctype html>") || strings.Contains(low, `id="root"`) {
				t.Fatalf("GET %s: body looks like SPA HTML — static handler must not shadow API prefix\nbody=%q",
					tc.path, body)
			}
			var env errEnvelope
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("GET %s: body is not a JSON envelope: %v\nbody=%q", tc.path, err, body)
			}
			if env.OK {
				t.Fatalf("GET %s: envelope ok=true on a 404", tc.path)
			}
			if env.Error == nil || env.Error.Code != "not_found" {
				t.Fatalf("GET %s: envelope error.code=%v, want \"not_found\"", tc.path, env.Error)
			}
		})
	}
}

// TestRegisteredAPIRoutesStillReachHandlers — guardrails the routing
// precedence: the catch-all "/" and the /api/ shadow-protector this
// feature added must NOT shadow more-specific feature routes. Pick
// one well-known endpoint per prefix.
func TestRegisteredAPIRoutesStillReachHandlers(t *testing.T) {
	srv := startServer(t)

	// /api/auth/register exists; an empty POST body must reach the
	// handler and produce a 400 envelope (not a 404 from the
	// shadow-protector and not the SPA HTML).
	resp, err := http.Post(srv.httpURL+"/api/auth/register", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("POST /api/auth/register: status=404 body=%q — registered route was shadowed by /api/ catch-all",
			body)
	}
	low := strings.ToLower(string(body))
	if strings.Contains(low, "<!doctype html>") {
		t.Fatalf("POST /api/auth/register: body looks like SPA HTML\nbody=%q", body)
	}

	// /debug/subs is registered by registerWS for any boot path
	// (no DB required). Must reach the gauge handler, not the
	// shadow-protector. The handler itself rejects requests without
	// a ?channel=... query parameter with 400; the assertion below
	// accepts any non-404 status so the test stays resilient to
	// future handler-side validation changes — the only failure
	// mode it forbids is "shadow handler returned 404 (or SPA HTML)
	// before the registered handler ran".
	dStatus, dBody, dCT := getRaw(t, srv, "/debug/subs")
	if dStatus == http.StatusNotFound {
		t.Fatalf("GET /debug/subs: status=404 body=%q — registered /debug/subs route was shadowed",
			dBody)
	}
	if strings.HasPrefix(strings.ToLower(dCT), "text/html") &&
		strings.Contains(strings.ToLower(string(dBody)), "<!doctype html>") {
		t.Fatalf("GET /debug/subs: served SPA HTML — registered route was shadowed\nbody=%q", dBody)
	}
}
