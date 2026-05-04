package logging_and_error_envelope_e2e_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC2_SensitiveQueryParamsRedacted covers AC-2 of
// specs/plans/phase-1/feature-logging-and-error-envelope.md verbatim:
//
//	Sensitive query parameters (`token`, `ticket`) are stripped/redacted
//	from logged URLs.
//
// Three requests exercise the redaction surface:
//
//  1. GET /api/auth/me?token=secret-token-AAA&ticket=secret-ticket-BBB
//     (authenticated). Both keys are redacted; their literal values must
//     not appear in the captured log line; the redaction placeholder
//     (token=REDACTED, ticket=REDACTED) must.
//  2. GET /debug/subs?channel=%23general&token=secret-token-CCC
//     (anonymous). Confirms a non-sensitive sibling param (channel) is
//     preserved while token is redacted — the middleware must not drop
//     unrelated query keys.
//  3. GET /debug/subs?channel=%23general (anonymous, no sensitive
//     params). The recorded path must keep channel verbatim and contain
//     neither REDACTED nor any decoy value — proves redaction is gated
//     on the key, not always-on.
//
// Each request is correlated to its log line via the X-Request-Id
// response header (RequestIDMiddleware) → request_id=<id> in the
// access-log line emitted by middleware.AccessLog.
func TestAC2_SensitiveQueryParamsRedacted(t *testing.T) {
	srv := startServer(t)

	_, tok := register(t, srv, "alice-ac2", "correct-horse-battery")

	const (
		decoyToken1  = "secret-token-AAA"
		decoyTicket1 = "secret-ticket-BBB"
		decoyToken2  = "secret-token-CCC"
	)

	// 1. Authenticated /api/auth/me with both decoy params.
	statusMe, hdrMe, _, rawMe := getJSON(t, srv, "/api/auth/me?token="+decoyToken1+"&ticket="+decoyTicket1, tok)
	if statusMe != http.StatusOK {
		t.Fatalf("/api/auth/me: status %d body %s", statusMe, rawMe)
	}
	reqIDMe := hdrMe.Get("X-Request-Id")
	if reqIDMe == "" {
		t.Fatal("/api/auth/me: missing X-Request-Id response header")
	}

	// 2. Anonymous /debug/subs with channel + decoy token.
	statusSubsMixed, hdrSubsMixed, _ := getRaw(t, srv, "/debug/subs?channel=%23general&token="+decoyToken2, "")
	if statusSubsMixed != http.StatusOK {
		t.Fatalf("/debug/subs (mixed): status %d", statusSubsMixed)
	}
	reqIDSubsMixed := hdrSubsMixed.Get("X-Request-Id")
	if reqIDSubsMixed == "" {
		t.Fatal("/debug/subs (mixed): missing X-Request-Id response header")
	}

	// 3. Anonymous /debug/subs with only a non-sensitive param.
	statusSubsClean, hdrSubsClean, _ := getRaw(t, srv, "/debug/subs?channel=%23general", "")
	if statusSubsClean != http.StatusOK {
		t.Fatalf("/debug/subs (clean): status %d", statusSubsClean)
	}
	reqIDSubsClean := hdrSubsClean.Get("X-Request-Id")
	if reqIDSubsClean == "" {
		t.Fatal("/debug/subs (clean): missing X-Request-Id response header")
	}

	// Distinct ids prove we don't accidentally read the same line three times.
	if reqIDMe == reqIDSubsMixed || reqIDMe == reqIDSubsClean || reqIDSubsMixed == reqIDSubsClean {
		t.Fatalf("expected three distinct request ids, got %q %q %q", reqIDMe, reqIDSubsMixed, reqIDSubsClean)
	}

	meLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDMe}, 5*time.Second)
	mixedLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDSubsMixed}, 5*time.Second)
	cleanLine := awaitLogLine(t, srv, []string{"access ", "request_id=" + reqIDSubsClean}, 5*time.Second)

	// 1. Both decoy values must be absent; both redaction placeholders present.
	if strings.Contains(meLine, decoyToken1) {
		t.Errorf("auth me log line leaked token value %q\nline=%q", decoyToken1, meLine)
	}
	if strings.Contains(meLine, decoyTicket1) {
		t.Errorf("auth me log line leaked ticket value %q\nline=%q", decoyTicket1, meLine)
	}
	if !strings.Contains(meLine, "token=REDACTED") {
		t.Errorf("auth me log line missing token=REDACTED placeholder\nline=%q", meLine)
	}
	if !strings.Contains(meLine, "ticket=REDACTED") {
		t.Errorf("auth me log line missing ticket=REDACTED placeholder\nline=%q", meLine)
	}

	// 2. Mixed line — token redacted, channel preserved.
	if strings.Contains(mixedLine, decoyToken2) {
		t.Errorf("mixed log line leaked token value %q\nline=%q", decoyToken2, mixedLine)
	}
	if !strings.Contains(mixedLine, "token=REDACTED") {
		t.Errorf("mixed log line missing token=REDACTED placeholder\nline=%q", mixedLine)
	}
	// channel value is %23general on the wire; net/url's q.Encode() emits
	// it the same way (the # byte must stay percent-encoded). Accept either
	// %23general or the decoded #general so this test survives a future
	// non-rewriting branch in redactURL (which keeps RawQuery untouched
	// when no key was redacted — but here a key WAS redacted, so the
	// encoded form is what's emitted).
	if !strings.Contains(mixedLine, "channel=%23general") && !strings.Contains(mixedLine, "channel=#general") {
		t.Errorf("mixed log line dropped unrelated channel param\nline=%q", mixedLine)
	}

	// 3. Clean line — channel preserved, no REDACTED noise, no decoy leakage
	// from cross-request contamination of the buffer match.
	if !strings.Contains(cleanLine, "channel=%23general") && !strings.Contains(cleanLine, "channel=#general") {
		t.Errorf("clean log line dropped channel param\nline=%q", cleanLine)
	}
	if strings.Contains(cleanLine, "REDACTED") {
		t.Errorf("clean log line contains unexpected REDACTED placeholder\nline=%q", cleanLine)
	}
	for _, decoy := range []string{decoyToken1, decoyTicket1, decoyToken2} {
		if strings.Contains(cleanLine, decoy) {
			t.Errorf("clean log line leaked decoy value %q\nline=%q", decoy, cleanLine)
		}
	}
}
