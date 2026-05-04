// AC-3 (500 sub-case): the four SEC-10 headers ride 5xx responses, not
// only 2xx and 4xx. Source:
// specs/plans/phase-1/feature-file-perms-and-headers.md.
//
// Closes the matrix gap called out in headers_on_errors_test.go's header:
// the 500 path was previously asserted only by inspection because there
// was no way to trigger a panic from outside the binary without editing
// production code. apps/server/internal/wiring/panicprobe.go (gated by
// `//go:build panicprobe`) registers GET /debug/panic, a handler that
// always panics. This test compiles the server with `-tags=panicprobe`
// and exercises Recover to confirm SecurityHeaders wraps it (the SEC
// headers must be set on the wire even when the inner handler crashed).
//
// Default chat-server binaries do not include /debug/panic — the
// panicprobe_off.go stub registers nothing — so this test is the only
// place the route is reachable.
package file_perms_and_headers_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestAC3_SecurityHeadersPresentOn500PanicResponse triggers the
// build-tag-gated /debug/panic handler and asserts the response is 500
// with the four SEC-10 headers set, proving SecurityHeaders is the
// outermost wrap (above Recover).
func TestAC3_SecurityHeadersPresentOn500PanicResponse(t *testing.T) {
	srv := startServer(t, startServerOpts{buildTags: "panicprobe"})

	client := &http.Client{Timeout: 5 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.httpURL+"/debug/panic", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do GET /debug/panic: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d; want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	requireSecHeaders(t, "panic_500_debug_panic", resp.Header)
}
