// Package dms_read_state_e2e_test exercises POST /api/dms/{id}/read
// end-to-end (issue #871). Decision-log anchors:
//   - L5: advance-only — posting an older message_id is a silent no-op.
//   - L8: 404 on non-participation (peer-pair ACL).
//   - L17: read-mark token-bucket per user.
//   - §7: emits {type:"read", scope:"dm"} to caller's user:<viewer>
//     topic for cross-device sync (no peer fan-out — L10).
//
// Black-box harness: boots the production chat-server binary via
// testsupport.StartServer (decision-log L27) and drives every assertion
// through the public HTTP + WS surface.
package dms_read_state_e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/tests/e2e/internal/testsupport"
)

const (
	envReadMarkBurst  = "CHAT_READ_MARK_BURST"
	envReadMarkRefill = "CHAT_READ_MARK_REFILL"
)

// userPair registers two users on srv and returns the (id, token)
// pairs.
func userPair(t *testing.T, srv *testsupport.Server, label string) (aliceID, aliceTok, bobID, bobTok string) {
	t.Helper()
	a := "alice-" + label + "-" + testsupport.RandomSecret(t, 4)
	b := "bob-" + label + "-" + testsupport.RandomSecret(t, 4)
	pw := testsupport.RandomSecret(t, 12)
	aliceID, aliceTok = testsupport.Register(t, srv.HTTPURL, srv.InviteCode, a, pw)
	bobID, bobTok = testsupport.Register(t, srv.HTTPURL, srv.InviteCode, b, pw)
	return aliceID, aliceTok, bobID, bobTok
}

// TestPostDMReadReturns204AndAdvancesUnreadCount — primary AC: the
// recipient's POST /read returns 204 and the next GET /api/dms shows
// unread_count = 0 for the messages prior to the read pointer.
func TestPostDMReadReturns204AndAdvancesUnreadCount(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "advance")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	m1 := sendDM(t, srv.HTTPURL, aliceTok, conv, "one")
	m2 := sendDM(t, srv.HTTPURL, aliceTok, conv, "two")
	_ = m1

	// Pre-read sanity: bob sees unread_count = 2.
	if got := unreadCountFor(t, srv.HTTPURL, bobTok, conv); got != 2 {
		t.Fatalf("pre-read unread: got %d want 2", got)
	}

	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": m2})
	if status != http.StatusNoContent {
		t.Fatalf("POST /read: got %d want 204; body=%s", status, raw)
	}
	if len(raw) != 0 {
		t.Errorf("POST /read body: got %q want empty", raw)
	}

	// Post-read: bob's unread_count = 0.
	if got := unreadCountFor(t, srv.HTTPURL, bobTok, conv); got != 0 {
		t.Errorf("post-read unread: got %d want 0", got)
	}
}

// TestPostDMReadAdvanceOnly — L5: posting an older id after a newer
// id is a silent no-op (still 204; pointer does not regress).
func TestPostDMReadAdvanceOnly(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "advonly")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	older := sendDM(t, srv.HTTPURL, aliceTok, conv, "one")
	time.Sleep(5 * time.Millisecond) // widen ULID prefix gap so older < newer is unambiguous.
	newer := sendDM(t, srv.HTTPURL, aliceTok, conv, "two")

	// Bob marks read at newer.
	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": newer})
	if status != http.StatusNoContent {
		t.Fatalf("first POST /read: got %d want 204; body=%s", status, raw)
	}
	if got := unreadCountFor(t, srv.HTTPURL, bobTok, conv); got != 0 {
		t.Fatalf("after newer-read unread: got %d want 0", got)
	}

	// Bob (or a buggy client) re-posts the older id; pointer must NOT
	// regress. Server stays advance-only and still returns 204.
	status2, _, raw2 := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": older})
	if status2 != http.StatusNoContent {
		t.Fatalf("older POST /read: got %d want 204; body=%s", status2, raw2)
	}

	// Send one more peer message; bob's unread should be 1, NOT 2 — so
	// the pointer is still pinned at `newer`, not regressed to `older`.
	sendDM(t, srv.HTTPURL, aliceTok, conv, "three")
	if got := unreadCountFor(t, srv.HTTPURL, bobTok, conv); got != 1 {
		t.Errorf("post-regression-attempt unread: got %d want 1 (pointer must not regress)", got)
	}
}

// TestPostDMReadNonParticipantReturns404 — L8: a third user POSTing
// /read on a conversation they are not part of receives 404 (no
// membership leak), not 403.
func TestPostDMReadNonParticipantReturns404(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "acl")

	carol := "carol-acl-" + testsupport.RandomSecret(t, 4)
	_, carolTok := testsupport.Register(t, srv.HTTPURL, srv.InviteCode,
		carol, testsupport.RandomSecret(t, 12))

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	mid := sendDM(t, srv.HTTPURL, aliceTok, conv, "ghost-target")

	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", carolTok,
		map[string]string{"message_id": mid})
	if status != http.StatusNotFound {
		t.Fatalf("non-participant POST /read: got %d want 404; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Errorf("error code: got %+v want not_found", env.Error)
	}
}

// TestPostDMReadEmitsReadFrameToCaller — §7: a successful POST /read
// emits a {type:"read", scope:"dm"} frame to the caller's
// user:<viewer> topic. The peer must NOT receive it (L10).
func TestPostDMReadEmitsReadFrameToCaller(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "ws")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	sendDM(t, srv.HTTPURL, aliceTok, conv, "one")
	mid := sendDM(t, srv.HTTPURL, aliceTok, conv, "two")

	bobFrames := dialUserInbox(t, srv, bobTok)
	aliceFrames := dialUserInbox(t, srv, aliceTok)

	// Bob marks read.
	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": mid})
	if status != http.StatusNoContent {
		t.Fatalf("POST /read: got %d want 204; body=%s", status, raw)
	}

	// Bob's own connection should see the read frame.
	frame := waitForReadFrame(t, bobFrames, conv)
	if frame.Data.Scope != "dm" {
		t.Errorf("scope: got %q want dm", frame.Data.Scope)
	}
	if frame.Data.TargetID != conv {
		t.Errorf("target_id: got %q want %q", frame.Data.TargetID, conv)
	}
	if frame.Data.LastReadMessageID != mid {
		t.Errorf("last_read_message_id: got %q want %q", frame.Data.LastReadMessageID, mid)
	}
	if frame.Data.UnreadCount != 0 {
		t.Errorf("unread_count: got %d want 0 (just advanced past tip)", frame.Data.UnreadCount)
	}

	// Alice (peer) must NOT see it. Drain her queue briefly; any
	// {type:"read"} frame indicates a peer leak (L10).
	if leaked := drainForReadFrame(aliceFrames, 200*time.Millisecond); leaked {
		t.Errorf("peer fan-out leak: alice received a {type:read} frame for bob's read mark")
	}
}

// TestPostDMReadRateLimited429 — L17: read-mark bucket Burst=N enforces
// 429 on the (N+1)th rapid POST. Override the env burst to a small
// value so the test is fast.
func TestPostDMReadRateLimited429(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{
		ExtraEnv: []string{
			envReadMarkBurst + "=2",
			envReadMarkRefill + "=1m",
		},
	})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "rl")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	mid := sendDM(t, srv.HTTPURL, aliceTok, conv, "msg")

	for i := 0; i < 2; i++ {
		status, _, raw := testsupport.PostJSON(t, srv.HTTPURL,
			"/api/dms/"+conv+"/read", bobTok,
			map[string]string{"message_id": mid})
		if status != http.StatusNoContent {
			t.Fatalf("POST /read #%d: got %d want 204; body=%s", i, status, raw)
		}
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": mid})
	if status != http.StatusTooManyRequests {
		t.Fatalf("3rd POST /read: got %d want 429; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "rate_limited" {
		t.Errorf("error code: got %+v want rate_limited", env.Error)
	}
}

// TestPostDMReadRejectsMalformedMessageID — body validation: a
// non-ULID message_id returns 400, not 500.
func TestPostDMReadRejectsMalformedMessageID(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "valid")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	sendDM(t, srv.HTTPURL, aliceTok, conv, "one")

	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/read", bobTok,
		map[string]string{"message_id": "not-a-ulid"})
	if status != http.StatusBadRequest {
		t.Fatalf("malformed: got %d want 400; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "bad_request" {
		t.Errorf("error code: got %+v want bad_request", env.Error)
	}
}

// --- helpers --------------------------------------------------------

// readFrame mirrors the wire shape of {type:"read", data:{scope, target_id,
// last_read_message_id, unread_count}}.
type readFrame struct {
	Type string `json:"type"`
	Data struct {
		Scope             string `json:"scope"`
		TargetID          string `json:"target_id"`
		LastReadMessageID string `json:"last_read_message_id"`
		UnreadCount       int    `json:"unread_count"`
	} `json:"data"`
}

// createDM POSTs /api/dms and returns the new conversation id.
func createDM(t *testing.T, httpURL, bearer, peerID string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, httpURL, "/api/dms", bearer,
		map[string]string{"peer_user_id": peerID})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("POST /api/dms: status %d body=%s", status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/dms: %v body=%s", err, raw)
	}
	return data.ID
}

// sendDM POSTs one DM and returns the persisted message id.
func sendDM(t *testing.T, httpURL, bearer, conversationID, body string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, httpURL,
		"/api/dms/"+conversationID+"/messages", bearer,
		map[string]string{"body": body})
	if status != http.StatusCreated {
		t.Fatalf("POST /api/dms/%s/messages: status %d body=%s", conversationID, status, raw)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /api/dms/.../messages: %v body=%s", err, raw)
	}
	return data.ID
}

// unreadCountFor reads GET /api/dms and returns the unread_count for the
// given conversation. Fails the test if the conversation isn't present.
func unreadCountFor(t *testing.T, httpURL, bearer, conversationID string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpURL+"/api/dms", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/dms: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/dms body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/dms: status %d body=%s", resp.StatusCode, raw)
	}
	var env testsupport.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, raw)
	}
	var data struct {
		Conversations []struct {
			ID          string `json:"id"`
			UnreadCount int    `json:"unread_count"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode listing: %v body=%s", err, raw)
	}
	for _, c := range data.Conversations {
		if c.ID == conversationID {
			return c.UnreadCount
		}
	}
	t.Fatalf("conversation %q not in listing; body=%s", conversationID, raw)
	return -1
}

// dialUserInbox opens an authenticated /ws connection (default channel
// fallback per L15 also subscribes to user:<viewer>) and returns a
// buffered channel of received frames.
func dialUserInbox(t *testing.T, srv *testsupport.Server, bearer string) chan []byte {
	t.Helper()
	ticket := testsupport.MintTicket(t, srv.HTTPURL, bearer)
	wsURL := fmt.Sprintf("%s?ticket=%s", srv.WSURL, ticket)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, resp, err := websocket.Dial(dialCtx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("ws status: %v want 101", resp)
	}

	frames := make(chan []byte, 32)
	readCtx, readCancel := context.WithCancel(context.Background())
	go func() {
		defer close(frames)
		for {
			_, raw, err := c.Read(readCtx)
			if err != nil {
				return
			}
			cp := make([]byte, len(raw))
			copy(cp, raw)
			select {
			case frames <- cp:
			case <-readCtx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		readCancel()
		_ = c.Close(websocket.StatusNormalClosure, "")
	})
	return frames
}

// waitForReadFrame drains frames until it sees a {type:"read"} envelope
// for the given conversation id, or hits a 5s timeout.
func waitForReadFrame(t *testing.T, frames chan []byte, conversationID string) readFrame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw, ok := <-frames:
			if !ok {
				t.Fatalf("ws stream closed before {type:read} frame for conv=%s", conversationID)
			}
			var probe struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				continue
			}
			if probe.Type != "read" {
				continue
			}
			var f readFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("decode read frame: %v body=%s", err, raw)
			}
			if f.Data.TargetID == conversationID {
				return f
			}
		case <-deadline:
			t.Fatalf("timed out waiting for {type:read} frame for conv=%s", conversationID)
			return readFrame{}
		}
	}
}

// drainForReadFrame polls frames for `budget` and returns true if any
// {type:"read"} frame is observed. Used to assert the negative case
// (peer must not receive a read mark — L10).
func drainForReadFrame(frames chan []byte, budget time.Duration) bool {
	deadline := time.After(budget)
	for {
		select {
		case raw, ok := <-frames:
			if !ok {
				return false
			}
			var probe struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &probe); err == nil && probe.Type == "read" {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
