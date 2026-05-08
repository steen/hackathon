// AC-1: WS upgrade enforces a same-origin check; cross-origin
// upgrades are rejected with a 403.
package ws_hardening_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestAC1_WSHardening_CrossOriginUpgradeRejectedWith403 covers the
// AC-1 statement verbatim:
//
//	"WS upgrade enforces a same-origin check; cross-origin upgrades
//	are rejected with a 403."
//
// The test boots the production binary with CHAT_ALLOWED_ORIGINS set
// to a single allowed origin (http://localhost:3000), then issues
// three /ws upgrade attempts. Each dial uses a freshly-minted
// one-shot ws-ticket (the production wiring runs ticket redemption
// before websocket.Accept, so an unauthenticated probe would 401
// before the origin check ever runs).
//
//  1. Cross-origin (Origin: http://evil.example.com) → must be
//     rejected with HTTP 403.
//  2. Allowed origin (Origin: http://localhost:3000, matched via
//     CHAT_ALLOWED_ORIGINS) → must succeed with HTTP 101 (positive
//     control proves the 403 isn't coming from an unrelated
//     misconfiguration).
//  3. No Origin header (loopback dev path) → must succeed with
//     HTTP 101 (the spec's "allow loopback during dev" carve-out;
//     coder/websocket's authenticateOrigin returns nil for an empty
//     Origin header).
func TestAC1_WSHardening_CrossOriginUpgradeRejectedWith403(t *testing.T) {
	srv := startServer(t, startServerOpts{
		allowedOrigins: "http://localhost:3000",
	})
	channelID := seededChannelID(t, srv)

	cases := []struct {
		name       string
		origin     string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "cross_origin_rejected_403",
			origin:     "http://evil.example.com",
			wantStatus: http.StatusForbidden,
			wantErr:    true,
		},
		{
			name:       "allowed_origin_accepted_101",
			origin:     "http://localhost:3000",
			wantStatus: http.StatusSwitchingProtocols,
			wantErr:    false,
		},
		{
			name:       "no_origin_loopback_accepted_101",
			origin:     "",
			wantStatus: http.StatusSwitchingProtocols,
			wantErr:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each subtest mints its own ticket — tickets are
			// single-use, so reuse across subtests would 401 before
			// the origin check.
			ticket := mintTicket(t, srv)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			c, resp, err := websocket.Dial(ctx, srv.wsURL+"?ticket="+ticket+"&channel="+channelID, &websocket.DialOptions{
				HTTPHeader: originHeader(tc.origin),
			})
			if c != nil {
				defer c.CloseNow()
			}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("dial Origin=%q: want error, got nil (resp=%v)", tc.origin, resp)
				}
			} else if err != nil {
				body := ""
				if resp != nil {
					body = resp.Status
				}
				t.Fatalf("dial Origin=%q: %v (resp=%s)", tc.origin, err, body)
			}

			if resp == nil {
				t.Fatalf("dial Origin=%q: nil response, want status %d", tc.origin, tc.wantStatus)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("dial Origin=%q: status=%d, want %d", tc.origin, resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
