// AC-2: WS connections must present a valid one-shot ticket from
// `POST /api/auth/ws-ticket`; tickets expire after 30 seconds and
// are single-use.
package ws_hardening_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestAC2_WSHardening_TicketRequired_SingleUse covers the single-use
// half of AC-2 verbatim:
//
//	"WS connections must present a valid one-shot ticket from
//	POST /api/auth/ws-ticket; ... and are single-use."
//
// Steps:
//  1. Mint a ticket; redeem it once at /ws → assert HTTP 101.
//  2. Re-dial /ws with the same ticket → assert non-101 (expect 401
//     per the spec's "Implementation notes": pre-upgrade ticket
//     failures return HTTP 401).
func TestAC2_WSHardening_TicketRequired_SingleUse(t *testing.T) {
	srv := startServer(t, startServerOpts{})

	ticket := mintTicket(t, srv)

	// First redemption: must succeed (101).
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	c1, resp1, err := websocket.Dial(ctx1, srv.wsURL+"?ticket="+ticket, nil)
	if err != nil {
		body := ""
		if resp1 != nil {
			body = resp1.Status
		}
		t.Fatalf("first dial with valid ticket: %v (resp=%s)", err, body)
	}
	defer c1.CloseNow()
	if resp1 == nil || resp1.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("first dial: status=%v, want 101", resp1)
	}

	// Close the first connection before re-redeeming so the rejection
	// path is exclusively about ticket reuse, not a per-user concurrent
	// connection guard if one ever lands.
	_ = c1.CloseNow()

	// Second redemption with the same ticket: must NOT yield 101.
	// coder/websocket.Dial returns a non-nil *http.Response with the
	// server's 401 body when the upgrade is refused.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	c2, resp2, err := websocket.Dial(ctx2, srv.wsURL+"?ticket="+ticket, nil)
	if err == nil {
		c2.CloseNow()
		t.Fatalf("second dial with reused ticket succeeded; want failure")
	}
	if resp2 == nil || resp2.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("second dial with reused ticket: resp=%v, want non-101", resp2)
	}
	if resp2.StatusCode != http.StatusUnauthorized {
		// Not a hard fail — the AC only requires "not 101" — but log
		// the drift so the next change to the ticket guard surfaces.
		t.Logf("note: reused ticket got status %d (expected 401)", resp2.StatusCode)
	}
}

// TestAC2_WSHardening_TicketExpiresAfter30Seconds covers the TTL
// half of AC-2 verbatim:
//
//	"... tickets expire after 30 seconds ..."
//
// TicketTTL is fixed at 30s in the production code
// (apps/server/internal/auth/tickets.go: `const TicketTTL =
// 30 * time.Second`) with no env override exposed, so this test must
// wait wall-clock 31s. Gated on `!testing.Short()` per the
// test-analysis findings.
func TestAC2_WSHardening_TicketExpiresAfter30Seconds(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-2 TTL arm sleeps 31s for ws-ticket expiry; skipped under -short")
	}

	srv := startServer(t, startServerOpts{})

	ticket := mintTicket(t, srv)

	time.Sleep(31 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, srv.wsURL+"?ticket="+ticket, nil)
	if err == nil {
		c.CloseNow()
		t.Fatalf("dial with expired ticket succeeded; want failure (resp=%v)", resp)
	}
	if resp == nil || resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("dial with expired ticket: resp=%v, want non-101", resp)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Logf("note: expired ticket got status %d (expected 401)", resp.StatusCode)
	}
}
