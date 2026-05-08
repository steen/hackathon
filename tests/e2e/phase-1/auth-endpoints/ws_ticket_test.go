package auth_endpoints_e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-5: POST /api/auth/ws-ticket issues a one-shot, 30-second ticket
// bound to the user, redeemable once at WS upgrade.
func TestAC5_WSTicket_OneShot_BoundToUser(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	register(t, srv, username, password)
	tok := login(t, srv, username, password)
	channelID := seededGeneralChannelID(t, srv, tok)

	// /ws-ticket → 200 envelope with non-empty ticket.
	ticket := mintTicket(t, srv, tok)
	if ticket == "" {
		t.Fatalf("ws-ticket envelope returned empty ticket")
	}

	// Dial /ws with the ticket → 101.
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(dialCtx, srv.wsURL+"?ticket="+ticket+"&channel="+channelID, nil)
	if err != nil {
		body := ""
		if resp != nil {
			body = fmt.Sprintf(" status=%d", resp.StatusCode)
		}
		t.Fatalf("dial /ws with valid ticket: %v%s", err, body)
	}
	defer c.CloseNow()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial /ws status = %v, want 101", resp)
	}

	// Re-using the same ticket must NOT yield 101. coder/websocket.Dial
	// returns a non-nil *http.Response with the server's 401 body when
	// the upgrade is refused.
	dialCtx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	c2, resp2, err := websocket.Dial(dialCtx2, srv.wsURL+"?ticket="+ticket+"&channel="+channelID, nil)
	if err == nil {
		c2.CloseNow()
		t.Fatalf("dial /ws with reused ticket succeeded; want failure")
	}
	if resp2 == nil || resp2.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("dial /ws with reused ticket: resp=%v, want non-101", resp2)
	}
	if resp2.StatusCode != http.StatusUnauthorized {
		// Not a hard fail — the spec just requires "not 101" — but
		// surface it so the next change to the ticket guard catches the
		// drift.
		t.Logf("note: reused ticket got status %d (expected 401)", resp2.StatusCode)
	}

	// Per-user binding: tickets for two different users must be
	// distinct strings, and each user's ticket can be redeemed at /ws
	// (proves they aren't the same opaque cookie).
	const otherUser = "bob"
	otherPassword := randomSecret(t, 12)
	register(t, srv, otherUser, otherPassword)
	otherTok := login(t, srv, otherUser, otherPassword)
	otherTicket := mintTicket(t, srv, otherTok)
	if otherTicket == ticket {
		t.Fatalf("two users got byte-identical tickets")
	}

	dialCtx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	c3, resp3, err := websocket.Dial(dialCtx3, srv.wsURL+"?ticket="+otherTicket+"&channel="+channelID, nil)
	if err != nil {
		t.Fatalf("dial /ws with bob's ticket: %v (resp=%v)", err, resp3)
	}
	c3.CloseNow()
	if resp3.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial with bob's ticket: status %d, want 101", resp3.StatusCode)
	}
}

// AC-5 (gap): the 30s TTL is asserted by a long-running test that
// sleeps 31s between issuing and redeeming the ticket. Skipped under
// `go test -short` so the default test loop stays fast.
func TestAC5_WSTicket_ExpiresAfter30Seconds(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-5 (gap): TTL test sleeps 31s; skipped under -short")
	}
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	register(t, srv, username, password)
	tok := login(t, srv, username, password)
	channelID := seededGeneralChannelID(t, srv, tok)
	ticket := mintTicket(t, srv, tok)

	// TicketTTL is 30s in the production code (apps/server/internal/auth/tickets.go).
	time.Sleep(31 * time.Second)

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(dialCtx, srv.wsURL+"?ticket="+ticket+"&channel="+channelID, nil)
	if err == nil {
		c.CloseNow()
		t.Fatalf("dial /ws with expired ticket succeeded; want failure (resp=%v)", resp)
	}
	if resp == nil || resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("dial /ws with expired ticket: resp=%v, want non-101", resp)
	}
}

// mintTicket POSTs /api/auth/ws-ticket with the given bearer and
// returns data.ticket from the envelope. Helper for AC-5 tests.
func mintTicket(t *testing.T, srv *runningServer, bearer string) string {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/auth/ws-ticket", bearer, nil)
	if status != http.StatusOK {
		t.Fatalf("/ws-ticket: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/ws-ticket envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /ws-ticket data: %v body=%s", err, raw)
	}
	return data.Ticket
}
