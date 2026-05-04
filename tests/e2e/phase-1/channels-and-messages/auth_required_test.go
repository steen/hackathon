package channels_and_messages_e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// AC-6: All endpoints require authentication (bearer token via REST,
// ticket-redeemed JWT for WS).
//
// For each REST endpoint, exercises three failure modes (no token,
// garbage bearer, tampered bearer); each must return a 401 envelope
// whose error.code == "unauthorized". For /ws, exercises (no ticket,
// garbage ticket, redeemed-once ticket) — each must NOT yield an HTTP
// 101 Switching Protocols response.
func TestAC6_RESTEndpointsRequireAuth(t *testing.T) {
	srv := startServer(t)

	// We need a valid token to compute the "tampered" bearer and to
	// create a known channel id for the per-channel routes.
	tok, _ := register(t, srv, randomUsername(t), randomPassword(t))
	ch := createChannel(t, srv, tok, randomChannelName(t))

	// Tampered = flip the last char of a real token. Garbage = a literal
	// non-JWT string.
	tampered := tok[:len(tok)-1] + flipChar(tok[len(tok)-1])
	garbage := "not-a-real-jwt"

	cases := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/channels", nil},
		{http.MethodPost, "/api/channels", []byte(`{"name":"x"}`)},
		{http.MethodGet, fmt.Sprintf("/api/channels/%s/messages", ch.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/channels/%s/messages", ch.ID), []byte(`{"body":"hi"}`)},
	}

	for _, c := range cases {
		c := c
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			for _, mode := range []struct {
				label string
				token string
			}{
				{"no-auth", ""},
				{"garbage", garbage},
				{"tampered", tampered},
			} {
				resp := doRequest(t, srv, mode.token, c.method, c.path, c.body)
				raw := readBody(t, resp)
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("AC-6: %s %s mode=%s status=%d body=%s; want 401",
						c.method, c.path, mode.label, resp.StatusCode, raw)
				}
				env := decodeEnvelope(t, raw)
				if env.OK || env.Error == nil || env.Error.Code != "unauthorized" {
					t.Fatalf("AC-6: %s %s mode=%s envelope=%s; want code=unauthorized",
						c.method, c.path, mode.label, raw)
				}
			}
		})
	}
}

// AC-6 (gap): WS arm — /ws must reject upgrades without a valid,
// unredeemed ticket.
func TestAC6_WSRequiresTicket(t *testing.T) {
	srv := startServer(t)

	tok, _ := register(t, srv, randomUsername(t), randomPassword(t))
	ch := createChannel(t, srv, tok, randomChannelName(t))

	wsBase := srv.wsURL

	t.Run("no ticket", func(t *testing.T) {
		q := url.Values{}
		q.Set("channel", ch.ID)
		assertWSDialFails(t, wsBase+"?"+q.Encode())
	})

	t.Run("garbage ticket", func(t *testing.T) {
		q := url.Values{}
		q.Set("channel", ch.ID)
		q.Set("ticket", "not-a-real-ticket")
		assertWSDialFails(t, wsBase+"?"+q.Encode())
	})

	t.Run("ticket already redeemed", func(t *testing.T) {
		ticket := mintWSTicket(t, srv, tok)
		q := url.Values{}
		q.Set("channel", ch.ID)
		q.Set("ticket", ticket)
		// First dial succeeds; close it and reuse the same ticket.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, wsBase+"?"+q.Encode(), nil)
		if err != nil {
			t.Fatalf("AC-6: first dial with valid ticket failed: %v", err)
		}
		_ = conn.Close(websocket.StatusNormalClosure, "redeemed once")
		// Second dial with same ticket must fail.
		assertWSDialFails(t, wsBase+"?"+q.Encode())
	})
}

// assertWSDialFails attempts a websocket.Dial and fatals if the
// upgrade succeeds. It does NOT care which 4xx the server returns;
// AC-6 just requires "not 101".
func assertWSDialFails(t *testing.T, wsURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "should not have upgraded")
		t.Fatalf("AC-6: ws dial succeeded; want failure for %s", wsURL)
	}
	if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("AC-6: ws got 101 Switching Protocols; want 4xx")
	}
}

// doRequest is authedRequest's permissive twin: it does NOT check that
// token is non-empty (so callers can exercise the no-auth arm).
func doRequest(t *testing.T, srv *runningServer, token, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.httpURL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// flipChar flips the last byte to a different printable JWT char so
// the tampered token is well-formed (length-preserving) but signature-
// invalid.
func flipChar(b byte) string {
	if b == 'a' {
		return "b"
	}
	return "a"
}
