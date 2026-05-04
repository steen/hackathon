package rate_limits_e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// AC-4: "Limits return HTTP 429 with the user-safe error envelope (see
// `feature-logging-and-error-envelope.md`)."
//
// AC-1's existing test already asserts the 429 status and that the
// envelope's ok=false and error.code is non-empty. AC-4 is specifically
// about the *envelope shape and user-safety* of the 429 body: the wire
// contract from PRD §10 + the "no SQL, stack frames, or file paths"
// rule on ErrorBody.Message in apps/server/internal/http/errors.go.
//
// This test trips both per-IP limiters (login burst=10, register
// burst=5; thresholds pinned from
// apps/server/internal/ratelimit/iplimit.go) and asserts the 429
// response:
//
//   - decodes to {"ok":false, "data":null, "error":{"code","message"}};
//   - error.code is the stable constant "rate_limited"
//     (apps/server/internal/http/middleware_ratelimit.go:CodeRateLimited);
//   - error.message is non-empty and user-safe (no obvious leakage of
//     internals — SQL keywords, stack-frame markers, absolute paths,
//     file extensions, package paths);
//   - Content-Type is application/json (charset is allowed but not
//     required by the contract);
//   - the four SecurityHeaders from apps/server/internal/http/headers_middleware.go
//     are present, proving the outer middleware stack still applies
//     when an inner handler short-circuits with 429.
func TestAC4_EnvelopeShapeOn429(t *testing.T) {
	srv := startServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("login 429 carries the user-safe envelope", func(t *testing.T) {
		// Vary the username so per-username backoff doesn't fire
		// before the per-IP bucket (matches AC-1's pattern).
		for i := 1; i <= 10; i++ {
			user := fmt.Sprintf("ac4-login-%03d", i)
			status, _, raw := loginRaw(t, client, srv, user, "wrong-password")
			if status == http.StatusTooManyRequests {
				t.Fatalf("login attempt %d/10 got 429 before reaching burst+1; LoginIPConfig burst=10 should permit the first 10 (body=%s)", i, raw)
			}
		}
		resp := postLoginRaw(t, client, srv, "ac4-login-011", "wrong-password")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusTooManyRequests {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("11th login attempt must be 429 to trip the IP limiter; got %d body=%s", resp.StatusCode, b)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read 429 body: %v", err)
		}
		assertRateLimitedEnvelope(t, resp, body, "login")
	})

	t.Run("register 429 carries the user-safe envelope", func(t *testing.T) {
		const wrongInvite = "definitely-not-the-invite"
		for i := 1; i <= 5; i++ {
			status, _, raw := registerRaw(t, client, srv, "ac4-reg", "correct-horse-battery", wrongInvite)
			if status == http.StatusTooManyRequests {
				t.Fatalf("register attempt %d/5 got 429 before reaching burst+1; RegisterIPConfig burst=5 should permit the first 5 (body=%s)", i, raw)
			}
		}
		resp := postRegisterRaw(t, client, srv, "ac4-reg", "correct-horse-battery", wrongInvite)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusTooManyRequests {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("6th register attempt must be 429 to trip the IP limiter; got %d body=%s", resp.StatusCode, b)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read 429 body: %v", err)
		}
		assertRateLimitedEnvelope(t, resp, body, "register")
	})
}

// assertRateLimitedEnvelope checks every shape requirement AC-4 puts on
// the 429 response. Defined in this file rather than in harness_test.go
// because the dispatch told this PR not to modify existing files and
// no other test currently needs these assertions.
func assertRateLimitedEnvelope(t *testing.T, resp *http.Response, body []byte, label string) {
	t.Helper()

	// Content-Type must announce JSON; charset is allowed.
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("%s 429: Content-Type=%q, want prefix application/json", label, ct)
	}

	// SecurityHeaders middleware wraps the entire stack; a short-circuit
	// 429 from an inner middleware must still get the four headers.
	wantHeaders := map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
	}
	for h, wantPrefix := range wantHeaders {
		got := resp.Header.Get(h)
		if got == "" {
			t.Errorf("%s 429: missing %s header (SecurityHeaders middleware should still apply on rate-limit short-circuit)", label, h)
			continue
		}
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("%s 429: %s=%q, want prefix %q", label, h, got, wantPrefix)
		}
	}

	// Decode generically so we can check the keys are exactly the
	// envelope contract: ok, data, error — and `data` is JSON null,
	// not `"null"` or absent.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("%s 429: decode envelope: %v\nbody=%s", label, err, body)
	}
	for _, k := range []string{"ok", "data", "error"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("%s 429: envelope missing %q key; got keys=%v body=%s", label, k, keysOf(raw), body)
		}
	}
	if !bytes.Equal(bytes.TrimSpace(raw["data"]), []byte("null")) {
		t.Errorf("%s 429: envelope.data must be JSON null on the error arm; got %s", label, raw["data"])
	}

	var ok bool
	if err := json.Unmarshal(raw["ok"], &ok); err != nil {
		t.Fatalf("%s 429: decode ok: %v", label, err)
	}
	if ok {
		t.Errorf("%s 429: envelope.ok must be false on a rate-limit rejection; got true", label)
	}

	var errBody struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw["error"], &errBody); err != nil {
		t.Fatalf("%s 429: decode error body: %v\nraw error=%s", label, err, raw["error"])
	}
	// Code is the stable wire constant clients branch on.
	if errBody.Code != "rate_limited" {
		t.Errorf("%s 429: envelope.error.code=%q, want %q (CodeRateLimited)", label, errBody.Code, "rate_limited")
	}
	// Message must be a non-empty user-safe string. The contract from
	// errors.go's ErrorBody comment forbids SQL text, stack frames,
	// and file paths.
	if strings.TrimSpace(errBody.Message) == "" {
		t.Errorf("%s 429: envelope.error.message is empty; the user-safe envelope requires a non-empty human-readable message", label)
	}
	assertUserSafeMessage(t, errBody.Message, label)
}

// assertUserSafeMessage flags message text that looks like internal
// leakage. It's a coarse heuristic — the contract is "no SQL, stack
// frames, or file paths" (errors.go), so we look for tokens that
// commonly appear in such leaks. False positives are unlikely for the
// short user-facing string the rate-limit middleware emits today
// ("too many requests, please try again later"); a future regression
// that swaps in a raw error string would trip these.
func assertUserSafeMessage(t *testing.T, msg, label string) {
	t.Helper()

	lower := strings.ToLower(msg)
	bannedSubstrings := []string{
		// SQL fragments — exact-token matches via word boundaries
		// would be safer but the message is short enough that
		// substring is fine; collisions with "select" inside
		// "selected" are acceptable here because no rate-limit
		// message would say that.
		"select ", "insert ", "update ", "delete from", "where ",
		// Stack/runtime markers
		"goroutine", "runtime.", "panic:", "0x",
		// Package paths and file extensions that point at sources
		"hackathon/", ".go:", "internal/",
	}
	for _, sub := range bannedSubstrings {
		if strings.Contains(lower, sub) {
			t.Errorf("%s 429: envelope.error.message contains %q which suggests internal leakage; got %q", label, sub, msg)
		}
	}
	// Absolute filesystem paths.
	if strings.HasPrefix(msg, "/") || regexp.MustCompile(`(?i)\b[a-z]:\\`).MatchString(msg) {
		t.Errorf("%s 429: envelope.error.message looks like a filesystem path; got %q", label, msg)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// postLoginRaw is a thin variant of harness_test.go's loginRaw that
// returns the *http.Response directly so the caller can inspect
// headers (Content-Type, SecurityHeaders). The harness's loginRaw
// closes the body and only returns (status, envelope, raw bytes), so
// it can't surface response headers — hence this local helper.
func postLoginRaw(t *testing.T, client *http.Client, srv *runningServer, username, password string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do POST /api/auth/login: %v", err)
	}
	return resp
}

func postRegisterRaw(t *testing.T, client *http.Client, srv *runningServer, username, password, invite string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": invite,
	})
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+"/api/auth/register", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new register request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do POST /api/auth/register: %v", err)
	}
	return resp
}
