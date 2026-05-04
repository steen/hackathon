package logging_and_error_envelope_e2e_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC1_AccessLogFieldsPresent covers AC-1 of
// specs/plans/phase-1/feature-logging-and-error-envelope.md verbatim:
//
//	Access-log middleware logs method, path, status, latency, IP, and
//	user ID (if known).
//
// Two requests are exercised:
//
//  1. Anonymous GET /debug/subs?channel=%23general — user_id should be
//     absent (the middleware emits "-" for an unauthenticated request).
//  2. Authenticated GET /api/auth/me — user_id should match the id
//     issued at registration.
//
// We correlate each request to its log line by the X-Request-Id header
// the server echoes in the response (RequestIDMiddleware). The
// middleware emits one log line per request with key=value fields:
//
//	method= path= status= latency_ms= request_id= remote_ip= user_id=
//
// We assert each field is present, has a non-empty value, and on the
// authenticated request that user_id matches the registered uid; on the
// anonymous request that user_id="-" (the documented absent-user
// convention in middleware.go's AccessLog).
func TestAC1_AccessLogFieldsPresent(t *testing.T) {
	srv := startServer(t)

	// Authenticated request first so the registration log lines don't
	// race with the assertion ordering — register itself produces an
	// access log line we don't inspect here.
	uid, tok := register(t, srv, "alice-ac1", "correct-horse-battery")

	// Authenticated GET /api/auth/me.
	statusMe, hdrMe, _, rawMe := getJSON(t, srv, "/api/auth/me", tok)
	if statusMe != http.StatusOK {
		t.Fatalf("/api/auth/me: status %d body %s", statusMe, rawMe)
	}
	reqIDMe := hdrMe.Get("X-Request-Id")
	if reqIDMe == "" {
		t.Fatalf("/api/auth/me: missing X-Request-Id response header")
	}

	// Anonymous GET /debug/subs?channel=%23general — this endpoint
	// returns text/plain, but the middleware still logs it.
	statusSubs, hdrSubs, _ := getRaw(t, srv, "/debug/subs?channel=%23general", "")
	if statusSubs != http.StatusOK {
		t.Fatalf("/debug/subs: status %d", statusSubs)
	}
	reqIDSubs := hdrSubs.Get("X-Request-Id")
	if reqIDSubs == "" {
		t.Fatalf("/debug/subs: missing X-Request-Id response header")
	}
	if reqIDSubs == reqIDMe {
		t.Fatalf("expected distinct request ids, got %q for both", reqIDMe)
	}

	// Find both log lines.
	authLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDMe}, 5*time.Second)
	anonLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDSubs}, 5*time.Second)

	// Required fields (AC-1: method, path, status, latency, IP, user ID).
	requiredKeys := []string{"method=", "path=", "status=", "latency_ms=", "remote_ip=", "user_id=", "request_id="}

	authFields := parseAccessLine(t, authLine, requiredKeys)
	anonFields := parseAccessLine(t, anonLine, requiredKeys)

	// Authenticated request — concrete value checks.
	if got := authFields["method"]; got != "GET" {
		t.Errorf("auth log method=%q, want GET", got)
	}
	if got := authFields["path"]; got != "/api/auth/me" {
		t.Errorf("auth log path=%q, want /api/auth/me", got)
	}
	if got := authFields["status"]; got != "200" {
		t.Errorf("auth log status=%q, want 200", got)
	}
	if got := authFields["user_id"]; got != uid {
		t.Errorf("auth log user_id=%q, want %q (registered uid)", got, uid)
	}
	if got := authFields["request_id"]; got != reqIDMe {
		t.Errorf("auth log request_id=%q, want %q", got, reqIDMe)
	}
	assertNonEmpty(t, "auth", "remote_ip", authFields["remote_ip"])
	assertNonEmpty(t, "auth", "latency_ms", authFields["latency_ms"])

	// Anonymous request — user_id should be the documented "-" placeholder.
	if got := anonFields["method"]; got != "GET" {
		t.Errorf("anon log method=%q, want GET", got)
	}
	if got := anonFields["status"]; got != "200" {
		t.Errorf("anon log status=%q, want 200", got)
	}
	// path must include the channel query (token/ticket-redaction is
	// AC-2's territory; we just verify the path field is populated and
	// names the requested resource).
	if path := anonFields["path"]; !strings.HasPrefix(path, "/debug/subs") {
		t.Errorf("anon log path=%q, want prefix /debug/subs", path)
	}
	if got := anonFields["user_id"]; got != "-" {
		t.Errorf("anon log user_id=%q, want %q (absent-user placeholder)", got, "-")
	}
	if got := anonFields["request_id"]; got != reqIDSubs {
		t.Errorf("anon log request_id=%q, want %q", got, reqIDSubs)
	}
	assertNonEmpty(t, "anon", "remote_ip", anonFields["remote_ip"])
	assertNonEmpty(t, "anon", "latency_ms", anonFields["latency_ms"])
}

// parseAccessLine splits a single access-log line into its key=value
// pairs. The middleware emits whitespace-separated tokens; values do
// not contain whitespace (path is URL-encoded; remote_ip/user_id are
// constrained alphabets per the gosec G706 comment in middleware.go).
// Fails the test if any required key is missing.
func parseAccessLine(t *testing.T, line string, required []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tok := range strings.Fields(line) {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			continue
		}
		key := tok[:eq]
		val := tok[eq+1:]
		out[key] = val
	}
	for _, want := range required {
		k := strings.TrimSuffix(want, "=")
		if _, ok := out[k]; !ok {
			t.Fatalf("access log line missing %q field; line=%q", want, line)
		}
	}
	return out
}

func assertNonEmpty(t *testing.T, tag, key, val string) {
	t.Helper()
	if val == "" {
		t.Errorf("%s log %s=<empty>", tag, key)
		return
	}
	// latency_ms should parse as a non-negative integer; remote_ip is
	// an IP literal (loopback), not exhaustively validated here. The
	// only thing AC-1 cares about is that the field is present and
	// has a value.
	if key == "latency_ms" {
		// Cheap sanity: digits only.
		for _, r := range val {
			if r < '0' || r > '9' {
				t.Errorf("%s log latency_ms=%q is not a non-negative integer", tag, val)
				return
			}
		}
	}
	_ = fmt.Sprint(val) // silence any future unused-variable refactor.
}
