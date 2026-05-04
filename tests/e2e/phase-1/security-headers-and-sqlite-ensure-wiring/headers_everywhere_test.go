package security_headers_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-1: "The HTTP server's `Handler` is built so every response —
// including those written by `Recover` after a panic and those
// produced by the `/ws` upgrade path — carries the four SEC-10 headers
// (`Content-Security-Policy`, `X-Content-Type-Options`,
// `Referrer-Policy`, `X-Frame-Options`)."
//
// This test boots the real server binary and exercises representative
// response paths, asserting all four headers appear with the documented
// values on each. The panic arm requires a panic-probe endpoint that
// does not yet exist in the server — see the panic sub-test for a
// scoped skip and the follow-up issue filed alongside this PR.
func TestAC1_FourSecurityHeadersOnEveryResponsePath(t *testing.T) {
	srv := startServer(t)

	t.Run("normal_http_response", func(t *testing.T) {
		// /debug/subs is loopback-allowed, requires no auth, and is
		// served through the same outermost SecurityHeaders wrap as
		// every other route. A 200 here is the closest thing to a
		// "normal HTTP response" available in the black-box surface
		// without first registering a user.
		req, err := http.NewRequest(http.MethodGet, srv.httpURL+"/debug/subs?channel=%23general", nil)
		if err != nil {
			t.Fatalf("new GET /debug/subs: %v", err)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /debug/subs: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /debug/subs status = %d, want 200", resp.StatusCode)
		}
		requireSecHeaders(t, "GET /debug/subs (200)", resp.Header)
	})

	t.Run("ws_upgrade_response", func(t *testing.T) {
		// /ws produces a 101 upgrade response when given a valid
		// ticket. The headers must ride the upgrade response too — a
		// hijack-aware ResponseWriter that drops them would silently
		// regress this AC.
		username := "alice"
		password := randomSecret(t, 12)
		bearer := register(t, srv, username, password)
		ticket := mintTicket(t, srv, bearer)

		dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(dialCtx, srv.wsURL+"?ticket="+ticket, nil)
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("dial /ws: %v (resp=%v)", err, resp)
		}
		defer c.CloseNow()
		if resp == nil {
			t.Fatalf("dial /ws: nil response")
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("dial /ws status = %d, want 101", resp.StatusCode)
		}
		requireSecHeaders(t, "WS /ws (101 upgrade)", resp.Header)
	})

	t.Run("panic_recovered_response", func(t *testing.T) {
		// AC-1 explicitly names "responses written by `Recover` after
		// a panic". The server has no production-built panic-probe
		// endpoint we can hit black-box; without one we cannot trigger
		// the Recover middleware's 500 response from a test. The
		// findings doc anticipates this case and marks it test.skip
		// pending a build-tagged probe (see the follow-up sub-issue).
		t.Skip("AC-1 panic arm: no panic-probe endpoint reachable from the production binary; tracked as a follow-up to add a build-tagged /debug/panic route so this assertion can run")
	})
}
