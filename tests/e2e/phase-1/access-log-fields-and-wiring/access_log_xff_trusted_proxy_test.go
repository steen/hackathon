package access_log_fields_and_wiring_e2e_test

import (
	"context"
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

// TestAC1_AccessLog_XFF_HonoredWithTrustedProxy locks in the
// trusted-proxy branch of AC-1 in
// specs/plans/phase-1/feature-access-log-fields-and-wiring.md:
//
//	The access log line emitted by AccessLog includes remote_ip=<ip>
//	(host portion of r.RemoteAddr, leftmost X-Forwarded-For when
//	CHAT_TRUSTED_PROXY=1) ...
//
// Companion to TestAC1_AccessLog_XFF_IgnoredWithoutTrustedProxy
// (access_log_xff_ignored_test.go), which covers the safe default.
// This one boots the binary with CHAT_TRUSTED_PROXY=1 and asserts:
//
//   - Authenticated request with `X-Forwarded-For: 1.2.3.4, 5.6.7.8`
//     records remote_ip=1.2.3.4 (leftmost, NOT loopback, NOT rightmost).
//   - Anonymous request with the same header records remote_ip=1.2.3.4.
//   - Request without XFF still records the loopback (sanity check that
//     trusted-proxy mode does not mangle non-XFF requests).
//   - Request with malformed leftmost XFF (`not-an-ip, 1.2.3.4`) falls
//     back to the loopback — matching LeftmostForwardedFor's
//     netip.ParseAddr validation in apps/server/internal/http/clientaddr.go.
func TestAC1_AccessLog_XFF_HonoredWithTrustedProxy(t *testing.T) {
	srv := startServerWithTrustedProxy(t)

	const xff = "1.2.3.4, 5.6.7.8"
	const wantLeftmost = "1.2.3.4"
	const rightmost = "5.6.7.8"

	// Authenticated leg: register, then GET /api/auth/me with the
	// bearer + XFF.
	uid, tok := register(t, srv, "alice-xff", "correct-horse-battery")

	authReq, err := http.NewRequest(http.MethodGet, srv.httpURL+"/api/auth/me", nil)
	if err != nil {
		t.Fatalf("new auth GET: %v", err)
	}
	authReq.Header.Set("Authorization", "Bearer "+tok)
	authReq.Header.Set("X-Forwarded-For", xff)
	authReqID := doAndReadRequestID(t, authReq, http.StatusOK, "/api/auth/me")

	// Anonymous leg: GET /debug/subs with XFF.
	anonReq, err := http.NewRequest(http.MethodGet, srv.httpURL+"/debug/subs?channel=%23general", nil)
	if err != nil {
		t.Fatalf("new anon GET: %v", err)
	}
	anonReq.Header.Set("X-Forwarded-For", xff)
	anonReqID := doAndReadRequestID(t, anonReq, http.StatusOK, "/debug/subs")

	// Sanity leg: no XFF — must still report the loopback.
	plainReq, err := http.NewRequest(http.MethodGet, srv.httpURL+"/debug/subs?channel=%23general", nil)
	if err != nil {
		t.Fatalf("new plain GET: %v", err)
	}
	plainReqID := doAndReadRequestID(t, plainReq, http.StatusOK, "/debug/subs")

	// Malformed-XFF leg: leftmost entry fails netip.ParseAddr, so the
	// helper returns "" and remoteIP falls back to RemoteAddr's host.
	badReq, err := http.NewRequest(http.MethodGet, srv.httpURL+"/debug/subs?channel=%23general", nil)
	if err != nil {
		t.Fatalf("new malformed GET: %v", err)
	}
	badReq.Header.Set("X-Forwarded-For", "not-an-ip, 1.2.3.4")
	badReqID := doAndReadRequestID(t, badReq, http.StatusOK, "/debug/subs")

	ids := []string{authReqID, anonReqID, plainReqID, badReqID}
	for i, a := range ids {
		for j := i + 1; j < len(ids); j++ {
			if a == ids[j] {
				t.Fatalf("expected distinct request ids, got %q for legs %d and %d", a, i, j)
			}
		}
	}

	authLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + authReqID}, 5*time.Second)
	anonLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + anonReqID}, 5*time.Second)
	plainLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + plainReqID}, 5*time.Second)
	badLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + badReqID}, 5*time.Second)

	required := []string{"remote_ip=", "request_id=", "user_id="}
	authFields := parseAccessLine(t, authLine, required)
	anonFields := parseAccessLine(t, anonLine, required)
	plainFields := parseAccessLine(t, plainLine, required)
	badFields := parseAccessLine(t, badLine, required)

	if got := authFields["user_id"]; got != uid {
		t.Errorf("auth log user_id=%q, want %q (registered uid via context helper)", got, uid)
	}
	if got := anonFields["user_id"]; got != "-" {
		t.Errorf("anon log user_id=%q, want %q (absent-user placeholder)", got, "-")
	}

	// The headline assertions: leftmost XFF wins on both auth and anon
	// requests when CHAT_TRUSTED_PROXY=1.
	if got := authFields["remote_ip"]; got != wantLeftmost {
		t.Errorf("auth log remote_ip=%q, want %q (leftmost X-Forwarded-For with CHAT_TRUSTED_PROXY=1)", got, wantLeftmost)
	}
	if got := anonFields["remote_ip"]; got != wantLeftmost {
		t.Errorf("anon log remote_ip=%q, want %q (leftmost X-Forwarded-For with CHAT_TRUSTED_PROXY=1)", got, wantLeftmost)
	}

	// Defense in depth: the rightmost entry must not appear, and the
	// loopback must not appear, in either logged leg.
	for tag, got := range map[string]string{"auth": authFields["remote_ip"], "anon": anonFields["remote_ip"]} {
		if strings.Contains(got, rightmost) {
			t.Errorf("%s log remote_ip=%q must not contain rightmost XFF entry %q", tag, got, rightmost)
		}
		if got == "127.0.0.1" || got == "::1" {
			t.Errorf("%s log remote_ip=%q is the loopback — XFF was not honored despite CHAT_TRUSTED_PROXY=1", tag, got)
		}
	}

	// Sanity: no XFF means the loopback wins even with the flag on.
	assertLoopbackIP(t, "no-xff", plainFields["remote_ip"])

	// Malformed leftmost: LeftmostForwardedFor returns "" → fallback to
	// the host portion of RemoteAddr (loopback). The valid second entry
	// must NOT be promoted.
	assertLoopbackIP(t, "malformed-xff", badFields["remote_ip"])
	if got := badFields["remote_ip"]; strings.Contains(got, "1.2.3.4") {
		t.Errorf("malformed-xff log remote_ip=%q must not promote the second entry %q when the leftmost is invalid", got, "1.2.3.4")
	}
}

// doAndReadRequestID issues req, asserts the status, and returns the
// X-Request-Id response header. It mirrors the inline pattern in
// access_log_xff_ignored_test.go but factored to keep the four-leg
// driver above readable.
func doAndReadRequestID(t *testing.T, req *http.Request, wantStatus int, label string) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s http.Do: %v", label, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s: status %d, want %d", label, resp.StatusCode, wantStatus)
	}
	id := resp.Header.Get("X-Request-Id")
	if id == "" {
		t.Fatalf("%s: missing X-Request-Id response header", label)
	}
	return id
}

// startServerWithTrustedProxy mirrors startServer (harness_test.go) but
// adds CHAT_TRUSTED_PROXY=1 to the child env. It is colocated here
// rather than added to the harness because harness_test.go is shared
// across this package's tests; a flag-free helper keeps the negative
// test (access_log_xff_ignored_test.go) and the field test
// (access_log_fields_test.go) reading the safe default.
func startServerWithTrustedProxy(t *testing.T) *runningServer {
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

	logs := &syncBuf{}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
		"CHAT_TRUSTED_PROXY=1",
	)
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
