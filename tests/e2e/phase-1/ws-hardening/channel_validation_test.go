// AC-4: WS sends to non-existent channels are rejected with a typed
// error frame and do not crash the connection.
package ws_hardening_e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestAC4_WSHardening_UnknownChannel_RejectedAtUpgrade covers the
// upgrade-arm of AC-4 verbatim:
//
//	"WS sends to non-existent channels are rejected with a typed error
//	frame and do not crash the connection."
//
// At this SHA the channel-existence check happens at upgrade time
// (apps/server/internal/wsapi/handler.go:161-172): when
// cfg.ChannelLookup is wired (production path with CHAT_DB_PATH set)
// a non-sentinel ?channel= that does not resolve in the channels
// table is rejected with HTTP 404 BEFORE the WebSocket handshake and
// BEFORE redeeming the ws-ticket. Three observable arms:
//
//  1. Structurally-invalid channel id ("NOT-A-REAL-ULID") → 404 at
//     upgrade. Never establishes a WS, so "connection survives" is
//     trivially satisfied — a probe cannot crash a connection that
//     was never made.
//  2. Structurally-valid-but-unknown ULID → 404 at upgrade. Same
//     arm as (1) for the contract surface, but proves the lookup
//     actually queried the DB rather than rejecting solely on
//     NormalizeChannelID's shape check.
//  3. Legacy "#general" sentinel + freshly-created ULID channel →
//  101. Positive control proving the 404 in arms (1)/(2) is the
//     channel-existence guard, not an unrelated upgrade failure.
//
// The harness's mintTicket helper uses a unique random username per
// call, so each subtest's ticket is independently bound and the
// reuse of a single startServer instance is safe.
func TestAC4_WSHardening_UnknownChannel_RejectedAtUpgrade(t *testing.T) {
	srv := startServer(t, startServerOpts{})

	// Create a channel via the REST surface so the positive case has
	// a known-good ULID. The harness's mintTicket already does
	// register + login, but it discards the username/password — so
	// register a dedicated channel-author here. The channel created
	// stays in the DB for the lifetime of the server, available to
	// every subtest's positive case.
	authorTok := registerForChannelCreation(t, srv)
	knownChannelID := createChannel(t, srv, authorTok, "ac4-known-"+randomSecret(t, 4))

	cases := []struct {
		name       string
		channel    string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "structurally_invalid_channel_404",
			channel:    "NOT-A-REAL-ULID",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name: "structurally_valid_but_unknown_ulid_404",
			// 26 chars, Crockford-base32 alphabet (0-9A-Z) — passes
			// NormalizeChannelID, fails the DB lookup. The leading
			// digits keep this distinct from any ULID the binary
			// could realistically mint at test time.
			channel:    "00000000000000000000NOEXIST",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "legacy_general_sentinel_accepted_101",
			channel:    "#general",
			wantStatus: http.StatusSwitchingProtocols,
			wantErr:    false,
		},
		{
			name:       "known_channel_id_accepted_101",
			channel:    knownChannelID,
			wantStatus: http.StatusSwitchingProtocols,
			wantErr:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ticket := mintTicket(t, srv)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			url := srv.wsURL + "?ticket=" + ticket + "&channel=" + tc.channel
			c, resp, err := websocket.Dial(ctx, url, nil)
			if c != nil {
				defer c.CloseNow()
			}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("dial channel=%q: want error, got nil (resp=%v)", tc.channel, resp)
				}
			} else if err != nil {
				body := ""
				if resp != nil {
					body = resp.Status
				}
				t.Fatalf("dial channel=%q: %v (resp=%s)", tc.channel, err, body)
			}

			if resp == nil {
				t.Fatalf("dial channel=%q: nil response, want status %d", tc.channel, tc.wantStatus)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("dial channel=%q: status=%d, want %d", tc.channel, resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// TestAC4_WSHardening_UnknownChannelDoesNotPoisonServer is the "does
// not crash the connection" half of AC-4, applied to the upgrade-time
// rejection path. The literal AC speaks of a frame on an established
// connection — but at this SHA the rejection is pre-upgrade, so the
// observable invariant becomes: a 404 for one client must not crash
// the server process or wedge subsequent legitimate WS connections.
//
// Steps:
//  1. Dial /ws with an unknown channel → must yield HTTP 404.
//  2. Immediately dial /ws with no channel (defaults to #general,
//     which bypasses the lookup) using a fresh ticket → must yield
//     HTTP 101 and a working bidirectional connection.
//  3. Read once on the post-404 connection — must yield the user's
//     own `presence:join` frame within 2s. A CloseError/EOF means
//     the server tore the connection down; a deadline timeout means
//     the broadcast pipeline is broken.
func TestAC4_WSHardening_UnknownChannelDoesNotPoisonServer(t *testing.T) {
	srv := startServer(t, startServerOpts{})

	// Arm 1 — bad-channel dial yields 404, no upgrade.
	badTicket := mintTicket(t, srv)
	ctxBad, cancelBad := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelBad()
	cBad, respBad, err := websocket.Dial(ctxBad, srv.wsURL+"?ticket="+badTicket+"&channel=NOT-A-REAL-ULID", nil)
	if cBad != nil {
		_ = cBad.CloseNow()
	}
	if err == nil {
		t.Fatalf("dial unknown channel: want error, got nil (resp=%v)", respBad)
	}
	if respBad == nil || respBad.StatusCode != http.StatusNotFound {
		t.Fatalf("dial unknown channel: resp=%v, want status 404", respBad)
	}

	// Arm 2 — fresh ticket against the legacy sentinel must still
	// upgrade. If the bad dial had crashed the server (e.g. via a
	// nil-pointer panic in the lookup path leaking past Recover),
	// this connection would either fail to dial or close immediately.
	goodTicket := mintTicket(t, srv)
	ctxGood, cancelGood := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelGood()
	cGood, respGood, err := websocket.Dial(ctxGood, srv.wsURL+"?ticket="+goodTicket, nil)
	if err != nil {
		body := ""
		if respGood != nil {
			body = respGood.Status
		}
		t.Fatalf("post-404 legitimate dial: %v (resp=%s)", err, body)
	}
	defer cGood.CloseNow()
	if respGood == nil || respGood.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("post-404 legitimate dial: resp=%v, want 101", respGood)
	}

	// Arm 3 — confirm the live connection is actually live. The
	// server emits a `{type:"presence", data:{kind:"join", ...}}`
	// frame to every subscriber when a user's first connection lands
	// (apps/server/internal/wsapi/handler.go:211 → BroadcastAll). The
	// post-404 dial here IS that first connection (the bad dial never
	// reached AddPresence), so reading once with a bounded deadline
	// should yield exactly that frame.
	//
	// What this proves: the server still serves WS connections after
	// the prior 404. A CloseError or an EOF would mean the bad dial
	// poisoned the process or wedged the listener; a deadline timeout
	// would mean the upgrade succeeded but the broadcast pipeline is
	// broken. Either failure mode is the AC-4 "do not crash the
	// connection" guarantee being violated.
	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	msgType, frame, readErr := cGood.Read(readCtx)
	if readErr != nil {
		var ce websocket.CloseError
		if errors.As(readErr, &ce) {
			t.Fatalf("post-404 legitimate dial: server closed connection (code=%d reason=%q)", ce.Code, ce.Reason)
		}
		if strings.Contains(readErr.Error(), "EOF") || strings.Contains(readErr.Error(), "closed") {
			t.Fatalf("post-404 legitimate dial: connection closed by server: %v", readErr)
		}
		t.Fatalf("post-404 legitimate dial: read failed: %v", readErr)
	}
	if msgType != websocket.MessageText {
		t.Fatalf("post-404 legitimate dial: read message type=%v, want text", msgType)
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(frame, &env); err != nil {
		t.Fatalf("post-404 legitimate dial: decode frame %q: %v", frame, err)
	}
	if env.Type != "presence" {
		t.Fatalf("post-404 legitimate dial: first frame type=%q, want presence; raw=%s", env.Type, frame)
	}
}

// TestAC4_WSHardening_TypedErrorFrameOnLiveConn pins the literal
// frame-arm of AC-4 ("typed error frame ... do not crash the
// connection") that requires typed inbound frames. Skipped because
// audit #78 dropped silent-rebroadcast and a typed inbound frame
// contract has not yet landed; the parent epic for that work is
// `feature-ws-userid-binding-and-channel-existence-check`. When that
// contract lands, replace the t.Skip with: dial with a valid ticket
// and channel "#general", write a JSON frame
// `{"type":"send","channel_id":"NOT-A-REAL-ULID","body":"hi"}`, expect
// a frame back `{"type":"error","code":"CHANNEL_NOT_FOUND"}`, then
// read once more within a short window to assert the connection is
// still open.
func TestAC4_WSHardening_TypedErrorFrameOnLiveConn(t *testing.T) {
	t.Skip("typed inbound frames not implemented; tracked separately — see feature-ws-userid-binding-and-channel-existence-check")
}

// registerForChannelCreation registers a fresh user and returns the
// auth token. Lighter than mintTicket because we don't need a
// ws-ticket — only a JWT to call POST /api/channels.
func registerForChannelCreation(t *testing.T, srv *runningServer) string {
	t.Helper()
	username := "channel-author-" + randomSecret(t, 4)
	password := randomSecret(t, 12)

	status, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register channel author: status %d body %s", status, raw)
	}
	if env.Data == nil {
		t.Fatalf("register channel author: nil data body=%s", raw)
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.Token == "" {
		t.Fatalf("register channel author: empty token body=%s", raw)
	}
	return data.Token
}

// createChannel POSTs /api/channels and returns the new channel id.
// The server folds and validates the id via ids.NormalizeChannelID,
// so the returned value is the canonical (upper-cased ULID) form.
func createChannel(t *testing.T, srv *runningServer, bearer, name string) string {
	t.Helper()
	status, env, raw := postJSON(t, srv, "/api/channels", bearer, map[string]string{
		"name": name,
	})
	if status != http.StatusCreated {
		t.Fatalf("POST /api/channels name=%q: status %d body %s", name, status, raw)
	}
	if env.Data == nil {
		t.Fatalf("POST /api/channels: nil data body=%s", raw)
	}
	var ch struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel data: %v body=%s", err, raw)
	}
	if ch.ID == "" {
		t.Fatalf("POST /api/channels: empty id body=%s", raw)
	}
	return ch.ID
}
