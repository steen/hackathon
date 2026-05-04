package access_log_fields_and_wiring_e2e_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestAC4_PanicCaughtByRecoverServerStaysUpEnvelope covers AC-4 of
// specs/plans/phase-1/feature-access-log-fields-and-wiring.md verbatim:
//
//	A panic raised inside any handler under the wired stack is caught by
//	Recover, never crashes the server process, and produces the user-safe
//	envelope already implemented.
//
// AC-4 has three observable claims against the running binary:
//
//  1. Hitting a handler that panics returns the user-safe envelope
//     (apps/server/internal/http/middleware.go:177-190 writes
//     500 + {"ok":false,"data":null,"error":{"code":"internal", ...}}
//     via WriteError + CodeInternal).
//  2. The server process is still alive afterwards (a follow-up GET
//     against an unrelated route returns its normal response).
//  3. The panic value never appears in the response body (envelope
//     hygiene per PRD §11 / SEC envelope; the panic value is only
//     written to the server log line "panic request_id=... value=...").
//
// All three require a route that deterministically panics inside the
// wired chain. The production binary intentionally has no such route
// (verified at this commit via `git grep -n panicprobe -- '*.go'` →
// zero hits, and apps/server/internal/wiring/ has no /debug/panic
// registration). The follow-up to add a `//go:build panicprobe`-gated
// /debug/panic handler is tracked at #306 (parent epic #63).
//
// Findings doc explicitly authorises the t.Skip route until the probe
// lands: specs/test-analysis/phase-1/access-log-fields-and-wiring.md
// AC-4 entry says "(a) gate this test behind a build tag ... (b)
// t.Skip("requires panic probe build tag") and document it." We take
// option (b) here; once #306 ships the panicprobe build tag and route,
// replace this skip with a real round-trip per the findings sketch:
//
//   - Build the server with `-tags=panicprobe`.
//   - GET /debug/panic → expect HTTP 500, envelope ok=false,
//     error.code="internal", and assert the response body does NOT
//     contain the documented panic-probe panic value.
//   - GET /debug/subs?channel=%23general → expect HTTP 200, proving
//     the server process survived.
func TestAC4_PanicCaughtByRecoverServerStaysUpEnvelope(t *testing.T) {
	t.Skip("AC-4 requires the panic-probe build tag/route from #306; skipping until that handler lands. " +
		"See specs/test-analysis/phase-1/access-log-fields-and-wiring.md AC-4 for the full assertion sketch.")

	// The body below documents the intended assertions so the
	// reviewer of #306 has a ready-to-restore implementation.
	srv := startServer(t)

	statusPanic, _, envPanic, rawPanic := getJSON(t, srv, "/debug/panic", "")
	if statusPanic != http.StatusInternalServerError {
		t.Fatalf("/debug/panic: status %d, want 500 (Recover writes WriteError(...,CodeInternal,...))", statusPanic)
	}
	if envPanic.OK {
		t.Errorf("/debug/panic envelope ok=true, want false")
	}
	if envPanic.Error == nil || envPanic.Error.Code != "internal" {
		t.Errorf("/debug/panic envelope error=%+v, want code=internal", envPanic.Error)
	}
	if envPanic.Data != nil {
		t.Errorf("/debug/panic envelope data=%s, want null per envelope hygiene", string(*envPanic.Data))
	}
	if strings.Contains(string(rawPanic), "panic-probe-secret") {
		t.Errorf("/debug/panic body leaked the documented panic value; envelope must not include it")
	}

	statusSubs, _, _ := getRaw(t, srv, "/debug/subs?channel=%23general", "")
	if statusSubs != http.StatusOK {
		t.Fatalf("/debug/subs after panic: status %d, want 200 (server process must survive Recover)", statusSubs)
	}
}
