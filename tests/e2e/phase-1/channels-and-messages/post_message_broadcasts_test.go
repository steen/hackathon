package channels_and_messages_e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dialWSChannel opens a /ws connection bound to a specific channel id
// using a freshly minted single-use ws-ticket. Each call mints a new
// ticket because tickets are one-shot.
func dialWSChannel(t *testing.T, srv *runningServer, token, channelID string) *websocket.Conn {
	t.Helper()
	ticket := mintWSTicket(t, srv, token)
	q := url.Values{}
	q.Set("channel", channelID)
	q.Set("ticket", ticket)
	wsURL := srv.wsURL + "?" + q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws %s: %v", wsURL, err)
	}
	t.Cleanup(func() {
		_ = conn.Close(websocket.StatusNormalClosure, "test done")
	})
	return conn
}

// readWSMessageFrame reads frames until one with `"type":"message"` is
// observed, swallowing presence events the hub fires on subscribe.
// AC-4 and AC-5 are about the message broadcast envelope; presence
// frames are out of scope here.
func readWSMessageFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, raw, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &probe); err == nil && probe.Type == "message" {
			return raw
		}
	}
	t.Fatalf("ws read: no message-typed frame within %s", timeout)
	return nil
}

// AC-4: POST /api/channels/{id}/messages persists a message and
// broadcasts it to WS subscribers of that channel (US-5).
//
// Setup: create channel c1, register a sender + two observer users,
// open a WS connection per observer bound to c1, then POST a message.
// Both observers must receive a frame whose data carries the
// {channel_id, body, sender_user_id, id, created_at} fields. SQLite
// must hold exactly one matching row.
func TestAC4_PostMessagePersistsAndBroadcasts(t *testing.T) {
	srv := startServer(t)
	senderTok, senderID := register(t, srv, randomUsername(t), randomPassword(t))
	obs1Tok, _ := register(t, srv, randomUsername(t), randomPassword(t))
	obs2Tok, _ := register(t, srv, randomUsername(t), randomPassword(t))

	ch := createChannel(t, srv, senderTok, randomChannelName(t))

	conn1 := dialWSChannel(t, srv, obs1Tok, ch.ID)
	conn2 := dialWSChannel(t, srv, obs2Tok, ch.ID)

	// Give the hub a moment to register the subscribers before the
	// REST-driven broadcast goes out.
	deadline := time.Now().Add(2 * time.Second)
	subsURL := srv.httpURL + "/debug/subs?channel=" + url.QueryEscape(ch.ID)
	for time.Now().Before(deadline) {
		resp, err := http.Get(subsURL) //nolint:gosec,noctx // test helper, loopback URL
		if err == nil {
			var n int
			_, _ = fmt.Fscanf(resp.Body, "%d", &n)
			resp.Body.Close()
			if n >= 2 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	body := "hello world"
	msg := sendMessage(t, srv, senderTok, ch.ID, body)

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		raw := readWSMessageFrame(t, conn, 3*time.Second)
		var frame struct {
			Type string      `json:"type"`
			Data messageInfo `json:"data"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("AC-4: observer %d frame decode: %v\nraw=%s", i, err, raw)
		}
		if frame.Type != "message" {
			t.Fatalf("AC-4: observer %d frame.type=%q want message", i, frame.Type)
		}
		if frame.Data.Body != body {
			t.Fatalf("AC-4: observer %d body=%q want %q", i, frame.Data.Body, body)
		}
		if frame.Data.ChannelID != ch.ID {
			t.Fatalf("AC-4: observer %d channel_id=%q want %q", i, frame.Data.ChannelID, ch.ID)
		}
		if frame.Data.SenderUserID != senderID {
			t.Fatalf("AC-4: observer %d sender_user_id=%q want %q", i, frame.Data.SenderUserID, senderID)
		}
		if frame.Data.ID != msg.ID {
			t.Fatalf("AC-4: observer %d id=%q want %q (REST-issued ULID)", i, frame.Data.ID, msg.ID)
		}
		if frame.Data.CreatedAt.IsZero() {
			t.Fatalf("AC-4: observer %d created_at is zero in %s", i, raw)
		}
	}

	// Read-only DB inspection: exactly one row matching the message id
	// + body in the channel.
	db := openDBReadOnly(t, srv)
	var (
		gotID, gotChan, gotUser, gotBody string
	)
	row := db.QueryRow(
		"SELECT id, channel_id, user_id, body FROM messages WHERE channel_id = ?",
		ch.ID,
	)
	if err := row.Scan(&gotID, &gotChan, &gotUser, &gotBody); err != nil {
		t.Fatalf("AC-4: scan messages row: %v", err)
	}
	if gotID != msg.ID || gotChan != ch.ID || gotUser != senderID || gotBody != body {
		t.Fatalf("AC-4: row mismatch id=%q chan=%q user=%q body=%q vs sent (id=%s, chan=%s, user=%s, body=%q)",
			gotID, gotChan, gotUser, gotBody, msg.ID, ch.ID, senderID, body)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE channel_id = ?", ch.ID).Scan(&count); err != nil {
		t.Fatalf("AC-4: count: %v", err)
	}
	if count != 1 {
		t.Fatalf("AC-4: messages rows for channel = %d, want 1", count)
	}

}
