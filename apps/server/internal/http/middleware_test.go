package http

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"hackathon/apps/server/internal/auth"
)

// SEC-11: token query parameter must not appear in access logs.
func TestAccessLogStripsTokenQueryParam_SEC11(t *testing.T) {
	logs := captureLog(t)

	h := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me?token=super-secret-jwt&foo=bar", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if strings.Contains(out, "super-secret-jwt") {
		t.Fatalf("access log leaked token value: %s", out)
	}
	if !strings.Contains(out, "token=REDACTED") {
		t.Fatalf("access log did not redact token key: %s", out)
	}
	if !strings.Contains(out, "foo=bar") {
		t.Fatalf("access log dropped non-sensitive query: %s", out)
	}
}

// SEC-11: ticket query parameter (WS upgrade) must not appear in access logs.
func TestAccessLogStripsTicketQueryParam_SEC11(t *testing.T) {
	logs := captureLog(t)

	h := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	req := httptest.NewRequest(http.MethodGet, "/ws?ticket=one-shot-ticket-value", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if strings.Contains(out, "one-shot-ticket-value") {
		t.Fatalf("access log leaked ticket value: %s", out)
	}
	if !strings.Contains(out, "ticket=REDACTED") {
		t.Fatalf("access log did not redact ticket key: %s", out)
	}
}

// Repeated keys (?token=a&token=b) and unusual encoding must not slip a raw
// secret through. Exercises proper URL parsing per the constraints.
func TestAccessLogRedactsRepeatedAndEncodedKeys(t *testing.T) {
	logs := captureLog(t)

	h := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x?token=alpha&token=beta&ticket=%2Fweird%3D", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	for _, leaked := range []string{"alpha", "beta", "weird"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("access log leaked %q: %s", leaked, out)
		}
	}
}

func TestAccessLogRecordsMethodPathStatusLatencyAndRequestID(t *testing.T) {
	logs := captureLog(t)

	chain := RequestIDMiddleware(AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})))
	req := httptest.NewRequest(http.MethodPost, "/api/foo", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	out := logs.String()
	for _, sub := range []string{"method=POST", "path=/api/foo", "status=418", "latency_ms=", "request_id=", "remote_ip=", "user_id="} {
		if !strings.Contains(out, sub) {
			t.Fatalf("access log missing %q: %s", sub, out)
		}
	}
	if strings.Contains(out, "request_id=\n") || strings.Contains(out, "request_id= ") {
		t.Fatalf("request_id was empty: %s", out)
	}
}

func TestAccessLogRecordsRemoteIPHostPortion(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "remote_ip=203.0.113.7") {
		t.Fatalf("access log missing remote_ip host: %s", out)
	}
	if strings.Contains(out, ":54321") {
		t.Fatalf("access log leaked client port: %s", out)
	}
}

// PRD §9 / §11: when CHAT_TRUSTED_PROXY is unset the access log must
// IGNORE X-Forwarded-For and record the host portion of RemoteAddr,
// even if the client supplied a forged XFF chain. The corresponding
// e2e test (PR #636) covers the same default through the full server;
// this unit test pins the AccessLog branch directly so a regression in
// the helper surfaces here first.
func TestAccessLogIgnoresXFFWhenTrustedProxyFalse(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "remote_ip=203.0.113.7") {
		t.Fatalf("access log dropped RemoteAddr fallback: %s", out)
	}
	if strings.Contains(out, "1.2.3.4") || strings.Contains(out, "5.6.7.8") {
		t.Fatalf("access log honored XFF with trustedProxy=false: %s", out)
	}
}

// PRD §9 / §11: when CHAT_TRUSTED_PROXY=1 the access log records the
// leftmost X-Forwarded-For entry, not the proxy's RemoteAddr.
func TestAccessLogHonorsXFFWhenTrustedProxyTrue(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.1:54321" // proxy
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "remote_ip=1.2.3.4") {
		t.Fatalf("access log did not honor leftmost XFF: %s", out)
	}
	if strings.Contains(out, "remote_ip=10.0.0.1") {
		t.Fatalf("access log fell back to RemoteAddr despite valid XFF: %s", out)
	}
}

// Defense-in-depth: with trustedProxy=true and a malformed leftmost XFF
// entry, the helper must reject the value and fall back to RemoteAddr
// rather than poison the log line with arbitrary client-supplied bytes.
func TestAccessLogFallsBackOnGarbageXFF(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "not-an-ip, 5.6.7.8")
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "remote_ip=10.0.0.1") {
		t.Fatalf("access log did not fall back to RemoteAddr on garbage XFF: %s", out)
	}
	if strings.Contains(out, "not-an-ip") {
		t.Fatalf("access log accepted garbage XFF leftmost: %s", out)
	}
}

func TestAccessLogRecordsUserIDFromContext(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-01HABC"))
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "user_id=user-01HABC") {
		t.Fatalf("access log missing user_id: %s", out)
	}
}

func TestAccessLogUserIDRendersDashWhenUnset(t *testing.T) {
	logs := captureLog(t)

	chain := AccessLog(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	chain.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	// "-" is the standard access-log convention for an absent value
	// (Apache combined log). Empty string would split as a zero-length
	// field for naive whitespace tokenizers.
	if !strings.Contains(out, "user_id=-") {
		t.Fatalf("access log user_id should be \"-\" when unset: %s", out)
	}
}

// test_panic_recovery_returns_generic_envelope_and_logs_internally
func TestPanicRecoveryReturnsGenericEnvelopeAndLogsInternally(t *testing.T) {
	logs := captureLog(t)

	const internalSecret = "secret-stack-trace-marker-do-not-leak"
	chain := RequestIDMiddleware(Recover(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(internalSecret)
	})))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}
	for _, k := range []string{"ok", "data", "error"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("envelope missing key %q: %v", k, body)
		}
	}
	if body["ok"] != false {
		t.Fatalf("ok = %v, want false", body["ok"])
	}
	if body["data"] != nil {
		t.Fatalf("data = %v, want nil", body["data"])
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %v, want object", body["error"])
	}
	if strings.Contains(errObj["message"].(string), internalSecret) {
		t.Fatalf("client-facing message leaked panic value: %v", errObj["message"])
	}
	if strings.Contains(rec.Body.String(), internalSecret) {
		t.Fatalf("response body leaked panic value: %s", rec.Body.String())
	}
	if errObj["code"] == "" {
		t.Fatalf("error.code is empty")
	}

	out := logs.String()
	if !strings.Contains(out, internalSecret) {
		t.Fatalf("server log did not include panic value: %s", out)
	}
	if !strings.Contains(out, "request_id=") {
		t.Fatalf("server log missing request_id: %s", out)
	}
}

func TestRequestIDMiddlewarePlumbsContextAndHeader(t *testing.T) {
	var seen string
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Fatalf("handler saw empty request ID")
	}
	if got := rec.Header().Get("X-Request-Id"); got != seen {
		t.Fatalf("X-Request-Id header = %q, ctx = %q", got, seen)
	}
}

func TestRequestIDsAreUniquePerRequest(t *testing.T) {
	seen := make(map[string]struct{}, 32)
	h := RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen[RequestID(r.Context())] = struct{}{}
	}))
	for i := 0; i < 32; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}
	if len(seen) != 32 {
		t.Fatalf("expected 32 distinct request IDs, got %d", len(seen))
	}
}

// Regression test for the audit finding "Access log loses every authenticated
// user_id". Before the fix the auth middleware wrote user.ID under its private
// ctxKey while the access log read via http.UserID, which keys on a different
// type — so user_id was always "-" in production. This test runs an
// authenticated request through the real chain (SecurityHeaders →
// RequestIDMiddleware → AccessLog → RequireJWT(handler)) plus an unauth
// request and asserts the log line shows the ULID for the auth case and "-"
// for the unauth case.
func TestAccessLogRecordsAuthenticatedUserIDThroughRealChain(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	tok := registerOK(t, f, "alice", "correct-horse-battery")

	var sawUserID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			sawUserID = uid
		}
		w.WriteHeader(http.StatusOK)
	})

	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            f.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	chain := SecurityHeaders(RequestIDMiddleware(AccessLog(false, require(handler))))

	logs := captureLog(t)

	// Authenticated request: log line must contain the user's ULID.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed status: got %d want 200", rec.Code)
	}
	if sawUserID == "" {
		t.Fatalf("handler never observed an authenticated user id; auth chain misconfigured")
	}
	out := logs.String()
	wantField := "user_id=" + sawUserID
	if !strings.Contains(out, wantField) {
		t.Fatalf("access log missing %q for authenticated request:\n%s", wantField, out)
	}
	if strings.Contains(out, "user_id=-") {
		t.Fatalf("access log rendered \"-\" for an authenticated request:\n%s", out)
	}
	// Defensive: the value must be a non-empty 26-char ULID, not the literal
	// string "<nil>" or some accidental fmt %v of a context value.
	ulidRE := regexp.MustCompile(`user_id=[0-9A-HJKMNP-TV-Z]{26}\b`)
	if !ulidRE.MatchString(out) {
		t.Fatalf("access log user_id is not a ULID-shaped value:\n%s", out)
	}

	// Unauthenticated request through the same chain: must render "-".
	logs.Reset()
	reqAnon := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	recAnon := httptest.NewRecorder()
	chain.ServeHTTP(recAnon, reqAnon)
	if recAnon.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status: got %d want 401", recAnon.Code)
	}
	outAnon := logs.String()
	if !strings.Contains(outAnon, "user_id=-") {
		t.Fatalf("access log missing user_id=- for unauthenticated request:\n%s", outAnon)
	}
}

// captureLog redirects the standard logger AND the default slog logger
// to a buffer for the duration of the test and restores both on cleanup.
// AccessLog still writes via stdlib log.Printf (issue #715 keeps the
// raw access line on stderr); Recover writes via slog (issue #734). Tests
// that assert on either path read out of the same buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	prevSlog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
		slog.SetDefault(prevSlog)
	})
	return &buf
}
