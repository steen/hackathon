package channels_and_messages_e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// AC-5: WS clients receive new-message events in real time, with author
// + timestamp + body (US-5).
//
// Two distinct senders post into the same channel; a single subscriber
// must receive both frames in send order, each with the correct
// sender_user_id, a parseable RFC 3339 timestamp within ±5s of the
// test's wall clock, and the exact body that was POSTed.
func TestAC5_WSNewMessageFrameCarriesAuthorAndTimestamp(t *testing.T) {
	srv := startServer(t)

	subTok, _ := register(t, srv, randomUsername(t), randomPassword(t))
	senderATok, senderAID := register(t, srv, randomUsername(t), randomPassword(t))
	senderBTok, senderBID := register(t, srv, randomUsername(t), randomPassword(t))

	ch := createChannel(t, srv, senderATok, randomChannelName(t))

	conn := dialWSChannel(t, srv, subTok, ch.ID)

	// Wait for the subscriber to land in the hub before posting.
	deadline := time.Now().Add(2 * time.Second)
	subsURL := srv.httpURL + "/debug/subs?channel=" + url.QueryEscape(ch.ID)
	for time.Now().Before(deadline) {
		resp, err := http.Get(subsURL) //nolint:gosec,noctx // test helper, loopback URL
		if err == nil {
			var n int
			_, _ = fmt.Fscanf(resp.Body, "%d", &n)
			resp.Body.Close()
			if n >= 1 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	posts := []struct {
		token  string
		userID string
		body   string
	}{
		{senderATok, senderAID, "first from A"},
		{senderBTok, senderBID, "second from B"},
	}

	postedAt := time.Now()
	for _, p := range posts {
		_ = sendMessage(t, srv, p.token, ch.ID, p.body)
	}

	for i, p := range posts {
		raw := readWSMessageFrame(t, conn, 3*time.Second)
		var frame struct {
			Type string `json:"type"`
			Data struct {
				ID           string `json:"id"`
				ChannelID    string `json:"channel_id"`
				SenderUserID string `json:"sender_user_id"`
				Body         string `json:"body"`
				CreatedAt    string `json:"created_at"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("AC-5: frame %d decode: %v\nraw=%s", i, err, raw)
		}
		if frame.Type != "message" {
			t.Fatalf("AC-5: frame %d type=%q want message", i, frame.Type)
		}
		if frame.Data.Body != p.body {
			t.Fatalf("AC-5: frame %d body=%q want %q", i, frame.Data.Body, p.body)
		}
		if frame.Data.SenderUserID != p.userID {
			t.Fatalf("AC-5: frame %d sender_user_id=%q want %q", i, frame.Data.SenderUserID, p.userID)
		}
		if got := len(frame.Data.SenderUserID); got != 26 {
			t.Fatalf("AC-5: frame %d sender_user_id length=%d, want 26 (ULID)", i, got)
		}
		if got := len(frame.Data.ID); got != 26 {
			t.Fatalf("AC-5: frame %d id length=%d, want 26 (ULID)", i, got)
		}
		if frame.Data.ChannelID != ch.ID {
			t.Fatalf("AC-5: frame %d channel_id=%q want %q", i, frame.Data.ChannelID, ch.ID)
		}
		ts, err := time.Parse(time.RFC3339Nano, frame.Data.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, frame.Data.CreatedAt)
		}
		if err != nil {
			t.Fatalf("AC-5: frame %d created_at=%q is not RFC3339(Nano): %v", i, frame.Data.CreatedAt, err)
		}
		if delta := time.Since(postedAt); ts.Before(postedAt.Add(-5*time.Second)) || ts.After(postedAt.Add(5*time.Second+delta)) {
			t.Fatalf("AC-5: frame %d created_at=%v outside ±5s of post wall clock %v", i, ts, postedAt)
		}
	}
}
