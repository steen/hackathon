package goclientpackage_e2e_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

// TestAC3 exercises AC-3 verbatim from
// specs/plans/phase-2/10-feature-go-client-package.md:
//
// "HTTP requests authenticate with `Authorization: Bearer <jwt>`;
// WebSocket connections use the one-shot ticket flow — call `WsTicket`
// to mint a ticket, then redeem it on upgrade as `?ticket=<hex>` (see
// `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`).
// Bearer tokens are not sent on the WS upgrade."
//
// The AC bundles three observable contracts. Each gets one sub-test,
// driven against the real apps/server binary booted by harness_test.go::
// startServer:
//
//  1. Bearer-on-REST: every authenticated REST call carries
//     `Authorization: Bearer <token>`. A sniffing http.RoundTripper
//     wired in via goclient.WithHTTPClient captures every request the
//     client emits; the assertion is on the captured headers, not on
//     the response, so a regression that dropped the header but still
//     somehow got a 200 (e.g. by accident of cookie auth) would still
//     fail.
//
//  2. Ticket round-trip: WsTicket returns a non-empty hex ticket, and
//     Watch redeems it as `?ticket=<hex>` on the upgrade URL. The
//     sniffer captures the upgrade GET so the test sees the exact
//     query string rather than relying on the server to echo it.
//
//  3. No-bearer-on-WS: even when SetToken has been called before
//     Watch, the WS upgrade request carries no `Authorization` header.
//     This pins the SEC-12 invariant from feature-ws-hardening.md.
func TestAC3_GoClientAuthTransport(t *testing.T) {
	t.Parallel()

	const acStatement = "HTTP requests authenticate with `Authorization: Bearer <jwt>`; WebSocket connections use the one-shot ticket flow — call `WsTicket` to mint a ticket, then redeem it on upgrade as `?ticket=<hex>` (see `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`). Bearer tokens are not sent on the WS upgrade."

	srv := startServer(t)

	// One registered user reused across the three sub-tests. Each sub-
	// test instantiates its own *Client with its own sniffer so there
	// is no cross-talk on captured requests.
	bootstrap := goclient.New(srv.url)
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bootstrapCancel()
	username := randomUsername(t)
	password := randomSecret(t, 16)
	if _, err := bootstrap.Register(bootstrapCtx, username, password, srv.inviteCode); err != nil {
		t.Fatalf("bootstrap Register: %v", err)
	}

	t.Run(acStatement+"/rest-requests-carry-Authorization-Bearer", func(t *testing.T) {
		t.Parallel()

		sniffer := newRequestSniffer()
		client := goclient.New(srv.url, goclient.WithHTTPClient(&http.Client{
			Transport: sniffer,
			Timeout:   30 * time.Second,
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Login mints + stores a bearer token. The Login request itself
		// is unauthenticated — that's expected; what we pin here is that
		// every subsequent authenticated call carries the header.
		loginResp, err := client.Login(ctx, username, password)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if loginResp.Token == "" {
			t.Fatalf("Login: empty token")
		}

		// Drive an authenticated GET (Me) and an authenticated POST
		// (WsTicket) so both verbs are covered.
		if _, err := client.Me(ctx); err != nil {
			t.Fatalf("Me: %v", err)
		}
		if _, err := client.WsTicket(ctx); err != nil {
			t.Fatalf("WsTicket: %v", err)
		}

		meReq := sniffer.firstMatching(t, http.MethodGet, "/api/auth/me")
		if got := meReq.Header.Get("Authorization"); got != "Bearer "+loginResp.Token {
			t.Fatalf("GET /api/auth/me Authorization = %q, want %q", got, "Bearer "+loginResp.Token)
		}
		ticketReq := sniffer.firstMatching(t, http.MethodPost, "/api/auth/ws-ticket")
		if got := ticketReq.Header.Get("Authorization"); got != "Bearer "+loginResp.Token {
			t.Fatalf("POST /api/auth/ws-ticket Authorization = %q, want %q", got, "Bearer "+loginResp.Token)
		}

		// Sanity: a second client with no token set must NOT emit an
		// Authorization header. This rules out the regression where the
		// header is hardcoded into the transport rather than sourced
		// from Client.Token().
		nakedSniffer := newRequestSniffer()
		nakedClient := goclient.New(srv.url, goclient.WithHTTPClient(&http.Client{
			Transport: nakedSniffer,
			Timeout:   30 * time.Second,
		}))
		// /api/auth/login is the only route that succeeds without a
		// bearer in the request, so use a wrong-password Login as the
		// probe — we only care about the header on the wire, not the
		// response.
		_, _ = nakedClient.Login(ctx, username, "definitely-wrong")
		loginReq := nakedSniffer.firstMatching(t, http.MethodPost, "/api/auth/login")
		if got := loginReq.Header.Get("Authorization"); got != "" {
			t.Fatalf("POST /api/auth/login (no token) Authorization = %q, want empty", got)
		}
	})

	t.Run(acStatement+"/ws-upgrade-redeems-ticket-as-query-param", func(t *testing.T) {
		t.Parallel()

		sniffer := newRequestSniffer()
		client := goclient.New(srv.url, goclient.WithHTTPClient(&http.Client{
			Transport: sniffer,
			Timeout:   30 * time.Second,
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if _, err := client.Login(ctx, username, password); err != nil {
			t.Fatalf("Login: %v", err)
		}

		// Mint a ticket explicitly so the test sees the exported
		// WsTicket return value (AC-3's "call `WsTicket` to mint a
		// ticket"). The Ticket field must be non-empty hex.
		ticket, err := client.WsTicket(ctx)
		if err != nil {
			t.Fatalf("WsTicket: %v", err)
		}
		if ticket.Ticket == "" {
			t.Fatalf("WsTicket: empty ticket")
		}
		if !isHex(ticket.Ticket) {
			t.Fatalf("WsTicket.Ticket = %q, want hex", ticket.Ticket)
		}

		// Watch mints its own ticket internally and redeems it on the
		// upgrade. The sniffer captures the upgrade GET regardless of
		// which ticket value Watch uses, so the assertion below is on
		// the redemption shape — `?ticket=<hex>` — not on the literal
		// ticket the test minted above.
		watchCtx, watchCancel := context.WithCancel(ctx)
		defer watchCancel()
		events, err := client.Watch(watchCtx, goclient.WatchOptions{})
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
		// Tear the subscription down right away — we only care about
		// the upgrade-request shape, not the event stream.
		watchCancel()
		drainEvents(events)

		upgradeReq := sniffer.firstUpgradeRequest(t)
		gotTicket := upgradeReq.URL.Query().Get("ticket")
		if gotTicket == "" {
			t.Fatalf("WS upgrade URL = %q, no `ticket` query parameter", upgradeReq.URL.String())
		}
		if !isHex(gotTicket) {
			t.Fatalf("WS upgrade ticket = %q, want hex", gotTicket)
		}
	})

	t.Run(acStatement+"/ws-upgrade-omits-Authorization-header", func(t *testing.T) {
		t.Parallel()

		sniffer := newRequestSniffer()
		client := goclient.New(srv.url, goclient.WithHTTPClient(&http.Client{
			Transport: sniffer,
			Timeout:   30 * time.Second,
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Login stores a token on the client. Per AC-3, the client must
		// NOT forward this token on the WS upgrade — even though every
		// other call from this *Client carries it. The sniffer captures
		// all requests; we filter the upgrade GET below.
		if _, err := client.Login(ctx, username, password); err != nil {
			t.Fatalf("Login: %v", err)
		}
		if client.Token() == "" {
			t.Fatalf("Login did not store a token; AC-3 invariant is unobservable without one")
		}

		watchCtx, watchCancel := context.WithCancel(ctx)
		defer watchCancel()
		events, err := client.Watch(watchCtx, goclient.WatchOptions{})
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
		watchCancel()
		drainEvents(events)

		upgradeReq := sniffer.firstUpgradeRequest(t)
		if got := upgradeReq.Header.Get("Authorization"); got != "" {
			t.Fatalf("WS upgrade Authorization = %q, want empty (SEC-12 / AC-3)", got)
		}
	})
}

// requestSniffer is an http.RoundTripper that records every request it
// forwards. It is the AC-3 instrument: a black-box view of the headers
// and URL the client emits, decoupled from server-side behavior.
type requestSniffer struct {
	mu       sync.Mutex
	requests []*http.Request
	inner    http.RoundTripper
}

func newRequestSniffer() *requestSniffer {
	return &requestSniffer{inner: http.DefaultTransport}
}

func (s *requestSniffer) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone for inspection. The original *http.Request mutates as the
	// transport adds connection-level headers (User-Agent, etc.); a
	// snapshot avoids data races and surprise mutations.
	clone := req.Clone(req.Context())
	clone.Body = nil // body is a one-shot reader; not needed for header asserts
	s.mu.Lock()
	s.requests = append(s.requests, clone)
	s.mu.Unlock()
	return s.inner.RoundTrip(req)
}

func (s *requestSniffer) snapshot() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*http.Request, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *requestSniffer) firstMatching(t *testing.T, method, path string) *http.Request {
	t.Helper()
	for _, r := range s.snapshot() {
		if r.Method == method && r.URL.Path == path {
			return r
		}
	}
	t.Fatalf("no captured %s %s request; saw %s", method, path, summarize(s.snapshot()))
	return nil
}

// firstUpgradeRequest finds the WS-upgrade GET. The coder/websocket
// dialer issues a regular http.Request with Connection: Upgrade and
// Upgrade: websocket; that's the canonical filter. Path is /ws on the
// production server.
func (s *requestSniffer) firstUpgradeRequest(t *testing.T) *http.Request {
	t.Helper()
	for _, r := range s.snapshot() {
		if r.Method != http.MethodGet {
			continue
		}
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			continue
		}
		return r
	}
	t.Fatalf("no captured WS upgrade request; saw %s", summarize(s.snapshot()))
	return nil
}

func summarize(reqs []*http.Request) string {
	var b strings.Builder
	for i, r := range reqs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(r.Method)
		b.WriteByte(' ')
		b.WriteString(r.URL.Path)
	}
	if b.Len() == 0 {
		return "(none)"
	}
	return b.String()
}

// isHex reports whether s is non-empty and consists entirely of ASCII
// hex digits. The server mints ws-tickets via crypto/rand.Read +
// hex.EncodeToString (apps/server/internal/auth/tickets.go), so the
// wire shape is hex; AC-3 names the redemption query as `?ticket=<hex>`.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// drainEvents consumes any in-flight events so Watch's goroutine
// reaches its ctx-cancel branch and closes the channel. A 2s deadline
// keeps a stuck server from hanging the test forever.
func drainEvents(events <-chan goclient.Event) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline.C:
			return
		}
	}
}
