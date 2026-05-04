// Package single_binary_demo_e2e_test verifies the Phase 3 single-binary
// demo path documented in specs/plans/phase-3/40-feature-single-binary-demo-verified.md.
//
// Unlike tests/e2e/phase-3/embedded-web-build, which builds with the
// committed placeholder index.html via plain `go build ./apps/server`,
// this suite exercises scripts/build-single-binary.sh end-to-end:
//
//  1. pnpm --filter web build       -> apps/web/dist/
//  2. copy apps/web/dist/* into apps/server/internal/web/dist/
//  3. go build -o <bin> ./apps/server
//
// then boots the produced binary with the auth-enabled env vars
// (CHAT_JWT_SECRET, CHAT_INVITE_CODE, CHAT_DB_PATH) and asserts both
// arms of the single-binary surface:
//
//   - HTTP /  serves the real Vite SPA index.html (carries a hashed
//     /assets/*.js bundle reference, which the placeholder lacks), and
//     that referenced asset is in turn fetchable from the same binary.
//   - /api/auth/register returns the {ok,data,error} envelope from
//     PRD §10 on a bad request — i.e. unauthenticated /api routes are
//     not shadowed by the SPA fallback handler.
//
// CI's `go` job runs `go test ./...` without setting up pnpm or running
// `pnpm install`, so the suite skips there (no pnpm on PATH or no
// node_modules). The intended runner is a developer or any future CI
// job that combines a pnpm-installed workspace with `go test`. Running
// locally without pnpm or without `pnpm install` produces a clean skip
// rather than a hard failure.
package single_binary_demo_e2e_test

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
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

type runningServer struct {
	httpURL    string
	port       int
	jwtSecret  string
	inviteCode string
	dbPath     string
	cancel     context.CancelFunc
	wait       chan struct{}
}

type envelope struct {
	OK    bool             `json:"ok"`
	Data  json.RawMessage  `json:"data"`
	Error *envelopeErrBody `json:"error"`
}

type envelopeErrBody struct {
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

// requirePNPMWorkspace skips the test when the conditions for running
// scripts/build-single-binary.sh are not met:
//
//   - `pnpm` is not on PATH; or
//   - the workspace `node_modules/` directory does not exist (i.e.
//     `pnpm install` has never been run in this checkout, so vite and
//     tsc are not resolvable).
//
// CI's `go` job runs `go test ./...` without pnpm or `pnpm install`,
// so a hard failure here would block that job. The CI `pnpm`, `lint`,
// and `e2e` jobs do `pnpm install --frozen-lockfile`, but they don't
// run `go test`. The intended runners are local developers (and any
// future job that combines `go test` with a pnpm-installed workspace).
func requirePNPMWorkspace(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("pnpm"); err != nil {
		t.Skipf("pnpm not on PATH: %v — single-binary demo build needs pnpm to produce the real Vite SPA bundle", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
		t.Skipf("%s/node_modules missing (run `pnpm install` to enable this test): %v", root, err)
	}
}

// buildSingleBinary runs scripts/build-single-binary.sh with OUT
// redirected to a per-test temp path so the suite never overwrites the
// repo's bin/chat-server.
//
// The script overlays apps/web/dist/* onto apps/server/internal/web/dist/.
// Most of those copied files are .gitignored, but the dist/index.html
// placeholder is *tracked* (it's the file //go:embed pulls in for plain
// `go build ./apps/server`). The script's `cp -R` overwrites it with
// the real Vite build's index.html, dirtying the working tree from a
// developer's perspective. We snapshot the placeholder before the build
// and restore it on test cleanup so running this suite does not leave
// `git status` showing a modified placeholder.
func buildSingleBinary(t *testing.T, root string) string {
	t.Helper()

	placeholder := filepath.Join(root, "apps", "server", "internal", "web", "dist", "index.html")
	saved, readErr := os.ReadFile(placeholder) //nolint:gosec // reading our own repo path
	if readErr != nil {
		t.Fatalf("snapshot placeholder %s: %v", placeholder, readErr)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(placeholder, saved, 0o644); err != nil { //nolint:gosec // restoring tracked file
			t.Logf("restore placeholder %s: %v", placeholder, err)
		}
	})

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "chatd-bundled")

	script := filepath.Join(root, "scripts", "build-single-binary.sh")
	cmd := exec.Command("bash", script, binPath)
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("scripts/build-single-binary.sh %s: %v", binPath, err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("expected binary at %s after build: %v", binPath, err)
	}
	return binPath
}

func startServer(t *testing.T, binPath string) *runningServer {
	t.Helper()

	tmp := t.TempDir()
	port := freePort(t)
	jwt := randomSecret(t, 32)
	invite := randomSecret(t, 8)
	dbPath := filepath.Join(tmp, "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
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

	if err := waitForPort(port, 15*time.Second); err != nil {
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
		jwtSecret:  jwt,
		inviteCode: invite,
		dbPath:     dbPath,
		cancel:     cancel,
		wait:       wait,
	}
}

func getRaw(t *testing.T, srv *runningServer, path string) (int, []byte, string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
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

// viteAssetRe matches the hashed asset path Vite injects into index.html
// when it builds for production: <script type="module" src="/assets/<name>-<hash>.js">.
// The placeholder index.html in apps/server/internal/web/dist/ has no
// /assets/ reference at all, so a match here proves the test built and
// embedded a real Vite bundle, not the placeholder.
var viteAssetRe = regexp.MustCompile(`/assets/[A-Za-z0-9._-]+\.js`)

// TestBinaryStartsWithMinimalEnv covers the `test_binary_starts_with_minimal_env`
// AC from issue #73: the single binary produced by scripts/build-single-binary.sh
// boots with only CHAT_JWT_SECRET + CHAT_INVITE_CODE + CHAT_DB_PATH set,
// serves the embedded SPA on /, and serves the API envelope on /api/.
func TestBinaryStartsWithMinimalEnv(t *testing.T) {
	root := repoRoot(t)
	requirePNPMWorkspace(t, root)

	binPath := buildSingleBinary(t, root)
	srv := startServer(t, binPath)

	t.Run("root_serves_real_vite_spa", func(t *testing.T) {
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
			t.Fatalf("GET / body missing SPA root mount point: %q", body)
		}
		// The placeholder index.html in apps/server/internal/web/dist/
		// carries no /assets/*.js reference. A real Vite production
		// build always injects one (or more). Asserting the match
		// proves scripts/build-single-binary.sh wrote the real bundle
		// over the placeholder before `go build` ran.
		if !viteAssetRe.MatchString(string(body)) {
			t.Fatalf("GET / body has no /assets/*.js reference; build-single-binary.sh did not overlay the real Vite bundle.\nbody=%q", body)
		}
	})

	t.Run("hashed_asset_is_fetchable", func(t *testing.T) {
		_, body, _ := getRaw(t, srv, "/")
		match := viteAssetRe.FindString(string(body))
		if match == "" {
			t.Skip("no /assets/*.js reference to follow; covered by root_serves_real_vite_spa")
		}
		status, assetBody, ct := getRaw(t, srv, match)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%q", match, status, assetBody)
		}
		// Vite ships ESM bundles. http.FileServer infers the
		// Content-Type from the .js extension; accept any javascript
		// MIME (some Go versions emit application/javascript, others
		// text/javascript) so this check stays stable across stdlib
		// updates.
		lct := strings.ToLower(ct)
		if !strings.Contains(lct, "javascript") {
			t.Fatalf("GET %s: Content-Type=%q, want a javascript MIME", match, ct)
		}
		if len(assetBody) == 0 {
			t.Fatalf("GET %s: empty asset body", match)
		}
	})

	t.Run("api_returns_envelope_not_spa", func(t *testing.T) {
		// POST an empty body to /api/auth/register. The handler is
		// unauthenticated and rejects the malformed JSON with a
		// {ok:false, error:{code:"bad_request"}} envelope. The point
		// of this assertion is not the specific error text — it's
		// that the SPA fallback did NOT shadow the /api/ route, so
		// machine clients see a parseable JSON envelope.
		resp, err := http.Post(srv.httpURL+"/api/auth/register", "application/json", bytes.NewReader(nil)) //nolint:gosec,noctx // test helper, loopback URL
		if err != nil {
			t.Fatalf("POST /api/auth/register: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
			t.Fatalf("POST /api/auth/register: Content-Type=%q, want application/json* (SPA shadow check)", ct)
		}
		low := strings.ToLower(string(body))
		if strings.Contains(low, "<!doctype html>") || strings.Contains(low, `id="root"`) {
			t.Fatalf("POST /api/auth/register: body looks like SPA HTML — /api/ was shadowed by the static handler\nbody=%q", body)
		}

		var env envelope
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("POST /api/auth/register: body is not a JSON envelope: %v\nbody=%q", err, body)
		}
		if env.OK {
			t.Fatalf("POST /api/auth/register: envelope ok=true on a bad request, want ok=false\nbody=%q", body)
		}
		if env.Error == nil || env.Error.Code == "" {
			t.Fatalf("POST /api/auth/register: missing error code in envelope\nbody=%q", body)
		}
	})
}
