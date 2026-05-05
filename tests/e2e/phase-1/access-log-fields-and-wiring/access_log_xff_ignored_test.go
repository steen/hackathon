package access_log_fields_and_wiring_e2e_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC1_AccessLog_XFF_IgnoredWithoutTrustedProxy locks in the safe
// default for the X-Forwarded-For branch of AC-1 in
// specs/plans/phase-1/feature-access-log-fields-and-wiring.md:
//
//	The access log line emitted by AccessLog includes remote_ip=<ip>
//	(host portion of r.RemoteAddr, leftmost X-Forwarded-For when
//	CHAT_TRUSTED_PROXY=1) ...
//
// This test exercises the unset-flag branch: with no CHAT_TRUSTED_PROXY
// configured, an XFF header from the client must be IGNORED and the
// access log must record the loopback host portion of r.RemoteAddr.
//
// The trusted-proxy parser is not yet wired (see EnvTrustedProxy in
// apps/server/internal/config/config.go and the docstrings on
// remoteIP/clientIP in apps/server/internal/http/middleware.go and
// apps/server/internal/http/auth_handlers.go). The positive branch —
// asserting the leftmost XFF entry is honored when CHAT_TRUSTED_PROXY
// is set — is tracked as a separate follow-up that lands with the
// parser.
func TestAC1_AccessLog_XFF_IgnoredWithoutTrustedProxy(t *testing.T) {
	srv := startServer(t)

	const xff = "1.2.3.4, 5.6.7.8"

	req, err := http.NewRequest(http.MethodGet, srv.httpURL+"/debug/subs?channel=%23general", nil)
	if err != nil {
		t.Fatalf("new GET: %v", err)
	}
	req.Header.Set("X-Forwarded-For", xff)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/debug/subs: status %d", resp.StatusCode)
	}
	reqID := resp.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Fatalf("/debug/subs: missing X-Request-Id response header")
	}

	line := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqID}, 5*time.Second)
	fields := parseAccessLine(t, line, []string{"remote_ip=", "request_id="})

	got := fields["remote_ip"]
	assertLoopbackIP(t, "xff-ignored", got)

	// Defense in depth: even if a future regression made remote_ip a
	// substring of the XFF chain, the explicit checks here ensure the
	// header value never leaks through.
	if strings.Contains(got, "1.2.3.4") || strings.Contains(got, "5.6.7.8") {
		t.Errorf("remote_ip=%q must not contain X-Forwarded-For entries (%q) when CHAT_TRUSTED_PROXY is unset", got, xff)
	}
}
