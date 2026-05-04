package logging_and_error_envelope_e2e_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC4_InternalDetailNotInClientLoggedWithRequestID covers AC-4 of
// specs/plans/phase-1/feature-logging-and-error-envelope.md verbatim:
//
//	Internal error details (stack, raw DB error) are not exposed to
//	clients but are logged on the server side with a request ID.
//
// AC-4 has two observable claims against the running binary:
//
//  1. The response body for an internal-error path carries the
//     user-safe envelope only — no stack frames, file paths, raw DB
//     error strings, or panic values.
//  2. The same request is correlated to a server-side log line that
//     carries the request id (X-Request-Id response header → log
//     substring `request_id=<id>`).
//
// The deterministic half — POSTing a malformed JSON body to a
// production handler — exercises the WriteError path inside
// (*AuthHandlers).Register and (*AuthHandlers).Login in
// apps/server/internal/http/auth_handlers.go. Those handlers swallow
// the underlying json.Decoder error and emit `{code: "bad_request",
// message: "invalid JSON body"}`, while AccessLog in
// apps/server/internal/http/middleware.go writes one log line per
// request including `request_id=<uuid>` and `status=400`. That pair
// satisfies the AC: the client body shows no internal detail; the
// server log carries a request-id-correlated record of the failed
// request.
//
// The panic half (stack-trace + panic value emitted by Recover in
// apps/server/internal/http/middleware.go) requires a build-tag-gated
// /debug/panic route that does not exist on `main` today; that route
// is tracked at #306. The panic_logs_stack_with_request_id sub-test
// below is `t.Skip`'d until that probe lands and documents the full
// assertion sketch the future round-trip should restore.
func TestAC4_InternalDetailNotInClientLoggedWithRequestID(t *testing.T) {
	srv := startServer(t)

	// internalLeakNeedles are substrings that would only appear in a
	// response body if the server leaked internals through the
	// envelope. The list is intentionally narrow to avoid false
	// positives on the legitimate `message: "invalid JSON body"`
	// text — that string is generic and safe to surface.
	internalLeakNeedles := []string{
		"goroutine ",            // Go stack header.
		"panic:",                // panic value prefix from runtime.
		".go:",                  // file:line citation in any stack frame.
		"apps/server/internal/", // any internal package path.
		"SELECT ",               // raw SQL fragment (driver error formatting).
		"INSERT ",               // raw SQL fragment.
		"sqlite3:",              // sqlite driver error prefix.
		// json.Decoder error fragments — we want the handler's
		// generic message, not the decoder's verbatim text.
		"invalid character",
		"unexpected end of JSON input",
		"cannot unmarshal",
	}

	t.Run("malformed_json_body_no_internals_in_response", func(t *testing.T) {
		// Two malformed bodies, two handlers — both should yield a
		// generic envelope and never echo decoder details.
		cases := []struct {
			name string
			path string
			body []byte
		}{
			{
				name: "register_malformed",
				path: "/api/auth/register",
				body: []byte("{not valid json"),
			},
			{
				name: "login_malformed",
				path: "/api/auth/login",
				body: []byte(`{"username":`),
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				req, err := http.NewRequest(http.MethodPost, srv.httpURL+c.path, bytes.NewReader(c.body))
				if err != nil {
					t.Fatalf("new request %s: %v", c.path, err)
				}
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("http.Do %s: %v", c.path, err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("%s: status=%d, want 400", c.path, resp.StatusCode)
				}

				var raw bytes.Buffer
				if _, err := raw.ReadFrom(resp.Body); err != nil {
					t.Fatalf("read body %s: %v", c.path, err)
				}
				body := raw.String()

				// Envelope shape sanity (full invariant lives in
				// AC-3's TestAC3_EnvelopeShapeIsConsistent — here we
				// only need the "ok=false, generic message" arm).
				if !strings.Contains(body, `"ok":false`) {
					t.Errorf("%s: response missing ok=false; body=%q", c.path, body)
				}
				if !strings.Contains(body, `"code":"bad_request"`) {
					t.Errorf("%s: response missing code=bad_request; body=%q", c.path, body)
				}

				for _, needle := range internalLeakNeedles {
					if strings.Contains(body, needle) {
						t.Errorf("%s: response body leaked internal detail %q\nbody=%q",
							c.path, needle, body)
					}
				}

				reqID := resp.Header.Get("X-Request-Id")
				if reqID == "" {
					t.Fatalf("%s: missing X-Request-Id response header", c.path)
				}

				// The "logged with a request ID" half of AC-4: the
				// AccessLog middleware emits one line per request
				// carrying `request_id=<id>` and `status=400`. That
				// line is the server-side correlated record AC-4
				// requires; downstream operators find it by request
				// id when a client reports the bad_request envelope.
				line := awaitLogLine(t, srv,
					[]string{"access ", "request_id=" + reqID, "status=400"},
					5*time.Second)

				// And — defence in depth — the access log line
				// itself must not echo any internal-detail substring
				// either. (The AccessLog middleware is well-defined
				// here; this guard catches a future regression that
				// e.g. concatenates the decoder error into the log.)
				for _, needle := range internalLeakNeedles {
					if strings.Contains(line, needle) {
						t.Errorf("%s: access log line leaked internal detail %q\nline=%q",
							c.path, needle, line)
					}
				}
			})
		}
	})

	t.Run("panic_logs_stack_with_request_id", func(t *testing.T) {
		// AC-4's panic half: trigger a deterministic panic on the
		// wired stack, assert (a) the response body is the generic
		// internal envelope without the panic value or stack, and
		// (b) the server log contains a `panic request_id=<id>` line
		// (emitted by the Recover middleware in middleware.go) that
		// includes a stack-trace marker and the same request id the
		// client received.
		//
		// This requires a build-tag-gated /debug/panic route on the
		// production binary. None exists at this commit (verified
		// via `git grep -n panicprobe -- '*.go'` → zero hits). The
		// follow-up to add a `//go:build panicprobe`-gated handler
		// is tracked at #306 (parent epic #63). Once #306 ships,
		// drop the t.Skip and restore the round-trip below.
		t.Skip("AC-4 panic half requires the panic-probe build tag/route from #306; " +
			"skipping until that handler lands. The assertion sketch below stays " +
			"in compileable form so the harness API matches when the probe arrives.")

		// Build the server with `-tags=panicprobe` (startServer
		// would need a probe-aware variant once #306 lands).
		statusPanic, hdrPanic, _, rawPanic := getJSON(t, srv, "/debug/panic", "")
		if statusPanic != http.StatusInternalServerError {
			t.Fatalf("/debug/panic: status %d, want 500", statusPanic)
		}
		reqIDPanic := hdrPanic.Get("X-Request-Id")
		if reqIDPanic == "" {
			t.Fatalf("/debug/panic: missing X-Request-Id response header")
		}

		// Client body must be the generic internal envelope only.
		bodyPanic := string(rawPanic)
		if !strings.Contains(bodyPanic, `"code":"internal"`) {
			t.Errorf("/debug/panic body missing code=internal: %q", bodyPanic)
		}
		for _, needle := range []string{"goroutine ", "panic:", ".go:", "apps/server/"} {
			if strings.Contains(bodyPanic, needle) {
				t.Errorf("/debug/panic body leaked %q: %q", needle, bodyPanic)
			}
		}

		// Server log must carry the panic line with the request id
		// AND a stack-trace marker (`goroutine ` is emitted by
		// runtime/debug.Stack — see the Recover middleware in
		// middleware.go).
		panicLine := awaitLogLine(t, srv,
			[]string{"panic ", "request_id=" + reqIDPanic},
			5*time.Second)
		if !strings.Contains(panicLine, "goroutine ") {
			t.Errorf("panic log line missing stack marker `goroutine `; line=%q", panicLine)
		}
	})
}
