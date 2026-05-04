package access_log_fields_and_wiring_e2e_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC1_AccessLogRemoteIPAndUserID covers AC-1 of
// specs/plans/phase-1/feature-access-log-fields-and-wiring.md verbatim:
//
//	The access log line emitted by AccessLog includes remote_ip=<ip>
//	(host portion of r.RemoteAddr, leftmost X-Forwarded-For when
//	CHAT_TRUSTED_PROXY=1) and user_id=<id> (set by an authenticated
//	handler via context helper; empty when unauthenticated).
//
// The verifiable surface here is:
//
//   - remote_ip is the host portion of r.RemoteAddr (loopback 127.0.0.1
//     or ::1 for an IPv6 dial) — verified on both an authenticated and
//     an anonymous request.
//   - user_id matches the registered user id when the request carries a
//     valid bearer token to a RequireJWT-wrapped route (/api/auth/me).
//   - user_id is the documented absent-user placeholder ("-" per
//     middleware.go) on an anonymous request to a non-auth route
//     (/debug/subs?channel=%23general).
//
// The X-Forwarded-For + CHAT_TRUSTED_PROXY branch of the AC is not
// exercised here because that wiring is deferred (see
// apps/server/internal/http/middleware.go remoteIP docstring on
// origin/main and apps/server/internal/config/config.go EnvTrustedProxy
// constant — the env var name is anchored but no parser reads it). A
// follow-up sub-issue tracks adding a dedicated test once the parser
// lands; see SKIPPED note in the PR body.
func TestAC1_AccessLogRemoteIPAndUserID(t *testing.T) {
	srv := startServer(t)

	// Authenticated leg: register, then GET /api/auth/me with the bearer.
	uid, tok := register(t, srv, "alice-ac1", "correct-horse-battery")

	statusMe, hdrMe, _, rawMe := getJSON(t, srv, "/api/auth/me", tok)
	if statusMe != http.StatusOK {
		t.Fatalf("/api/auth/me: status %d body %s", statusMe, rawMe)
	}
	reqIDMe := hdrMe.Get("X-Request-Id")
	if reqIDMe == "" {
		t.Fatalf("/api/auth/me: missing X-Request-Id response header")
	}

	// Anonymous leg: GET /debug/subs (text/plain, but middleware logs it).
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

	authLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDMe}, 5*time.Second)
	anonLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDSubs}, 5*time.Second)

	required := []string{"method=", "path=", "status=", "remote_ip=", "user_id=", "request_id="}
	authFields := parseAccessLine(t, authLine, required)
	anonFields := parseAccessLine(t, anonLine, required)

	// Authenticated request: user_id is the registered id; remote_ip is
	// the loopback host (127.0.0.1 for IPv4, ::1 for IPv6 dial).
	if got := authFields["path"]; got != "/api/auth/me" {
		t.Errorf("auth log path=%q, want /api/auth/me", got)
	}
	if got := authFields["status"]; got != "200" {
		t.Errorf("auth log status=%q, want 200", got)
	}
	if got := authFields["user_id"]; got != uid {
		t.Errorf("auth log user_id=%q, want %q (registered uid via context helper)", got, uid)
	}
	assertLoopbackIP(t, "auth", authFields["remote_ip"])

	// Anonymous request: user_id is the absent-user placeholder per the
	// middleware ("empty when unauthenticated" in AC-1, encoded as "-").
	if path := anonFields["path"]; !strings.HasPrefix(path, "/debug/subs") {
		t.Errorf("anon log path=%q, want prefix /debug/subs", path)
	}
	if got := anonFields["status"]; got != "200" {
		t.Errorf("anon log status=%q, want 200", got)
	}
	if got := anonFields["user_id"]; got != "-" {
		t.Errorf("anon log user_id=%q, want %q (absent-user placeholder for unauthenticated request)", got, "-")
	}
	assertLoopbackIP(t, "anon", anonFields["remote_ip"])
}

// assertLoopbackIP fails the test unless val is the host portion of an
// IPv4 or IPv6 loopback address. The Go http client may dial either
// family depending on the resolver; both are valid for a 127.0.0.1
// listen address since the OS aliases vary.
func assertLoopbackIP(t *testing.T, tag, val string) {
	t.Helper()
	if val == "" {
		t.Errorf("%s log remote_ip=<empty>", tag)
		return
	}
	if strings.Contains(val, ":") && !strings.HasPrefix(val, "::") {
		// SplitHostPort failure case in middleware.remoteIP would leave
		// host:port joined; that's a regression we want to catch.
		t.Errorf("%s log remote_ip=%q contains a port — middleware should strip it via net.SplitHostPort", tag, val)
		return
	}
	switch val {
	case "127.0.0.1", "::1":
		return
	}
	t.Errorf("%s log remote_ip=%q, want loopback (127.0.0.1 or ::1)", tag, val)
}
