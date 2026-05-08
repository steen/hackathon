// Package dms_crud_e2e_test exercises the Phase 9 DM CRUD surface
// end-to-end: POST /api/dms (find-or-create), GET /api/dms (listing),
// POST /api/dms/{id}/messages (send + WS broadcast), GET
// /api/dms/{id}/messages (history). Decision-log anchors:
// §3 hide-empty, §4/§8 self-sufficient WS frame to BOTH user:<viewer>
// topics, §6 self-DM rejected, §9 last_message_at DESC, §11 sender
// dm_reads atomic, L2 canonical pair ordering, L8 404 on non-
// participation, L16 4096 body cap, L17 dm-write bucket, L18 201/200
// idempotent.
//
// Black-box harness: boots the production chat-server binary via
// testsupport.StartServer (decision log L27) and drives every assertion
// through the public HTTP + WS surface.
package dms_crud_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/tests/e2e/internal/testsupport"
)

const (
	envDMWriteBurst  = "CHAT_DM_WRITE_BURST"
	envDMWriteRefill = "CHAT_DM_WRITE_REFILL"
)

// userPair registers two users on srv and returns the (id, token)
// pairs. Used by every test below; pulled out so per-test naming is
// the only ceremony.
func userPair(t *testing.T, srv *testsupport.Server, label string) (aliceID, aliceTok, bobID, bobTok string) {
	t.Helper()
	a := "alice-" + label + "-" + testsupport.RandomSecret(t, 4)
	b := "bob-" + label + "-" + testsupport.RandomSecret(t, 4)
	pw := testsupport.RandomSecret(t, 12)
	aliceID, aliceTok = testsupport.Register(t, srv.HTTPURL, srv.InviteCode, a, pw)
	bobID, bobTok = testsupport.Register(t, srv.HTTPURL, srv.InviteCode, b, pw)
	return aliceID, aliceTok, bobID, bobTok
}

// TestPostDMIsIdempotent201Then200 — L18: first POST returns 201, the
// repeated POST for the same pair returns 200 with the same id.
func TestPostDMIsIdempotent201Then200(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "idem")

	status1, env1, raw1 := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", aliceTok,
		map[string]string{"peer_user_id": bobID})
	if status1 != http.StatusCreated {
		t.Fatalf("first POST /api/dms: got status %d want 201; body=%s", status1, raw1)
	}
	var first struct {
		ID      string `json:"id"`
		UserAID string `json:"user_a_id"`
		UserBID string `json:"user_b_id"`
		Peer    struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"peer"`
	}
	if err := json.Unmarshal(*env1.Data, &first); err != nil {
		t.Fatalf("decode first body: %v", err)
	}
	if first.Peer.ID != bobID || first.Peer.Username == "" {
		t.Fatalf("peer summary on create: %+v", first.Peer)
	}
	// L2 canonical ordering: user_a_id < user_b_id, regardless of who called.
	if first.UserAID >= first.UserBID {
		t.Errorf("L2 violation: user_a_id=%q must be < user_b_id=%q", first.UserAID, first.UserBID)
	}

	status2, env2, raw2 := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", aliceTok,
		map[string]string{"peer_user_id": bobID})
	if status2 != http.StatusOK {
		t.Fatalf("second POST /api/dms: got status %d want 200; body=%s", status2, raw2)
	}
	var second struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env2.Data, &second); err != nil {
		t.Fatalf("decode second body: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("idempotency: second id %q != first id %q", second.ID, first.ID)
	}
}

// TestPostDMSelfPeerReturns400 — §6: peer_user_id == caller is rejected.
func TestPostDMSelfPeerReturns400(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	aliceID, aliceTok, _, _ := userPair(t, srv, "self")

	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", aliceTok,
		map[string]string{"peer_user_id": aliceID})
	if status != http.StatusBadRequest {
		t.Fatalf("self-DM: got status %d want 400; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "invalid_peer" {
		t.Errorf("self-DM error code: got %+v want invalid_peer", env.Error)
	}
}

// TestListDMsHidesEmptyAndOrdersByLastMessageDesc — §3 + §9.
func TestListDMsHidesEmptyAndOrdersByLastMessageDesc(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "list1")
	// Third user, will share an EMPTY conversation with alice — must NOT appear.
	carolName := "carol-list1-" + testsupport.RandomSecret(t, 4)
	carolID, _ := testsupport.Register(t, srv.HTTPURL, srv.InviteCode,
		carolName, testsupport.RandomSecret(t, 12))

	convAB := createDM(t, srv.HTTPURL, aliceTok, bobID)
	createDM(t, srv.HTTPURL, aliceTok, carolID) // empty — never sees a message.

	// Send one message in alice<->bob so it's the only conversation
	// eligible for the listing.
	sendDM(t, srv.HTTPURL, aliceTok, convAB, "first")

	// GET /api/dms via the same envelope helper.
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/dms", aliceTok)
	if status != http.StatusOK {
		t.Fatalf("GET /api/dms: status %d body=%s", status, raw)
	}
	var data struct {
		Conversations []struct {
			ID            string `json:"id"`
			LastMessageID string `json:"last_message_id"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	if len(data.Conversations) != 1 {
		t.Fatalf("listing length: got %d want 1 (empty conversations must be hidden); body=%s",
			len(data.Conversations), raw)
	}
	if data.Conversations[0].ID != convAB {
		t.Errorf("listing id: got %q want %q (alice<->bob)", data.Conversations[0].ID, convAB)
	}

	// Add a third user with a message AFTER alice<->bob's, then assert
	// the new conversation is first (last_message_at DESC).
	convAC := createDM(t, srv.HTTPURL, aliceTok, carolID)
	time.Sleep(15 * time.Millisecond) // ULID monotonic prefix advances by ms; widen the gap to make ordering stable.
	sendDM(t, srv.HTTPURL, aliceTok, convAC, "later")

	status2, env2, raw2 := getJSON(t, srv.HTTPURL, "/api/dms", aliceTok)
	if status2 != http.StatusOK {
		t.Fatalf("GET /api/dms 2: status %d body=%s", status2, raw2)
	}
	var data2 struct {
		Conversations []struct {
			ID string `json:"id"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env2.Data, &data2); err != nil {
		t.Fatalf("decode listing 2: %v", err)
	}
	if len(data2.Conversations) != 2 {
		t.Fatalf("listing length 2: got %d want 2; body=%s", len(data2.Conversations), raw2)
	}
	if data2.Conversations[0].ID != convAC {
		t.Errorf("ordering: first conv id = %q, want %q (newest last_message_at)",
			data2.Conversations[0].ID, convAC)
	}
}

// TestListDMsRecipientUnreadCountCountsAllPeerMessages — L6 DM rule:
// recipient with no dm_reads row sees unread_count = COUNT(peer messages).
func TestListDMsRecipientUnreadCountCountsAllPeerMessages(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "unread")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	sendDM(t, srv.HTTPURL, aliceTok, conv, "one")
	sendDM(t, srv.HTTPURL, aliceTok, conv, "two")
	sendDM(t, srv.HTTPURL, aliceTok, conv, "three")

	// Sender's listing: unread_count = 0 (sender's dm_reads row was
	// advanced atomically per §11).
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/dms", aliceTok)
	if status != http.StatusOK {
		t.Fatalf("alice listing: %d body=%s", status, raw)
	}
	var aliceData struct {
		Conversations []struct {
			ID          string `json:"id"`
			UnreadCount int    `json:"unread_count"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env.Data, &aliceData); err != nil {
		t.Fatalf("decode alice: %v", err)
	}
	if len(aliceData.Conversations) != 1 || aliceData.Conversations[0].UnreadCount != 0 {
		t.Errorf("sender unread_count: got %+v want 1 conv with unread_count=0",
			aliceData.Conversations)
	}

	// Recipient's listing: unread_count = 3 (no dm_reads row yet).
	status, env, raw = getJSON(t, srv.HTTPURL, "/api/dms", bobTok)
	if status != http.StatusOK {
		t.Fatalf("bob listing: %d body=%s", status, raw)
	}
	var bobData struct {
		Conversations []struct {
			ID          string `json:"id"`
			UnreadCount int    `json:"unread_count"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env.Data, &bobData); err != nil {
		t.Fatalf("decode bob: %v", err)
	}
	if len(bobData.Conversations) != 1 || bobData.Conversations[0].UnreadCount != 3 {
		t.Errorf("recipient unread_count: got %+v want 1 conv with unread_count=3",
			bobData.Conversations)
	}
}

// TestSendMessageBroadcastsToBothUserTopics — §4 / §8: the
// {type:"dm"} frame fans to BOTH user:<a> and user:<b> topics with
// a self-sufficient conversation block.
func TestSendMessageBroadcastsToBothUserTopics(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, bobTok := userPair(t, srv, "ws")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)

	aliceFrames := dialUserInbox(t, srv, aliceTok)
	bobFrames := dialUserInbox(t, srv, bobTok)

	body := "hello over ws"
	sendDM(t, srv.HTTPURL, aliceTok, conv, body)

	aliceFrame := waitForDMFrame(t, aliceFrames, conv, body)
	bobFrame := waitForDMFrame(t, bobFrames, conv, body)

	// Self-sufficient: both frames carry conversation + dm_message.
	if aliceFrame.Data.Conversation.ID != conv {
		t.Errorf("alice frame conversation id: got %q want %q", aliceFrame.Data.Conversation.ID, conv)
	}
	if bobFrame.Data.Conversation.ID != conv {
		t.Errorf("bob frame conversation id: got %q want %q", bobFrame.Data.Conversation.ID, conv)
	}
	if aliceFrame.Data.Conversation.Peer.ID != bobID {
		t.Errorf("alice frame peer.id: got %q want bob %q", aliceFrame.Data.Conversation.Peer.ID, bobID)
	}
	if bobFrame.Data.Conversation.Peer.ID == bobID {
		t.Errorf("bob frame peer.id: got bob's own id %q (expected alice's id)", bobFrame.Data.Conversation.Peer.ID)
	}
	if aliceFrame.Data.DMMessage.Body != body {
		t.Errorf("alice dm_message body: got %q want %q", aliceFrame.Data.DMMessage.Body, body)
	}
	if bobFrame.Data.DMMessage.Body != body {
		t.Errorf("bob dm_message body: got %q want %q", bobFrame.Data.DMMessage.Body, body)
	}
	if aliceFrame.Data.Conversation.UnreadCount != 0 {
		t.Errorf("alice (sender) unread_count: got %d want 0 (sender was advanced atomically)",
			aliceFrame.Data.Conversation.UnreadCount)
	}
	if bobFrame.Data.Conversation.UnreadCount != 1 {
		t.Errorf("bob (recipient) unread_count: got %d want 1 (NULL dm_reads → all peer messages count)",
			bobFrame.Data.Conversation.UnreadCount)
	}
}

// TestNonParticipantGetReturns404 — L8: non-participant on GET messages
// is 404 (not 403, no membership leak).
func TestNonParticipantGetReturns404(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "acl")

	carolName := "carol-acl-" + testsupport.RandomSecret(t, 4)
	_, carolTok := testsupport.Register(t, srv.HTTPURL, srv.InviteCode,
		carolName, testsupport.RandomSecret(t, 12))

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)

	status, env, raw := getJSON(t, srv.HTTPURL, "/api/dms/"+conv+"/messages", carolTok)
	if status != http.StatusNotFound {
		t.Fatalf("non-participant GET: got %d want 404; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Errorf("error code: got %+v want not_found", env.Error)
	}

	postStatus, postEnv, postRaw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/messages", carolTok,
		map[string]string{"body": "ghost"})
	if postStatus != http.StatusNotFound {
		t.Fatalf("non-participant POST: got %d want 404; body=%s", postStatus, postRaw)
	}
	if postEnv.Error == nil || postEnv.Error.Code != "not_found" {
		t.Errorf("error code post: got %+v want not_found", postEnv.Error)
	}
}

// TestPostMessageRejectsOversizedBody — L16: cap is 4096; 4097 returns
// 400 with the dedicated message_too_large code.
func TestPostMessageRejectsOversizedBody(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "cap")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)

	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/messages", aliceTok,
		map[string]string{"body": strings.Repeat("x", 4097)})
	if status != http.StatusBadRequest {
		t.Fatalf("oversized POST: got %d want 400; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "message_too_large" {
		t.Errorf("error code: got %+v want message_too_large", env.Error)
	}
}

// TestDMWriteBucketEnforces429 — L17: 11th rapid POST returns 429 with
// the dm-write bucket. Override the burst to a small value so the test
// is fast and deterministic.
func TestDMWriteBucketEnforces429(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{
		ExtraEnv: []string{
			envDMWriteBurst + "=3",
			envDMWriteRefill + "=1m",
		},
	})
	_, aliceTok, bobID, _ := userPair(t, srv, "rl")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)

	// First Burst=3 POSTs succeed.
	for i := 0; i < 3; i++ {
		status, _, raw := testsupport.PostJSON(t, srv.HTTPURL,
			"/api/dms/"+conv+"/messages", aliceTok,
			map[string]string{"body": fmt.Sprintf("msg %d", i)})
		if status != http.StatusCreated {
			t.Fatalf("send #%d: got %d want 201; body=%s", i, status, raw)
		}
	}
	// 4th must 429.
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/messages", aliceTok,
		map[string]string{"body": "drop me"})
	if status != http.StatusTooManyRequests {
		t.Fatalf("4th send: got %d want 429; body=%s", status, raw)
	}
	if env.Error == nil || env.Error.Code != "rate_limited" {
		t.Errorf("error code: got %+v want rate_limited", env.Error)
	}
}

// TestSenderDMReadsRowAdvancedInsideTransaction — §11: sender's
// dm_reads row is created/advanced atomically inside InsertDMMessageTx.
// Asserted indirectly through GET /api/dms unread_count = 0 for the
// sender after multiple sends. (DB-direct read would require another
// process opening the SQLite file; the listing is an equivalent proof
// because the unread_count formula reads dm_reads.)
func TestSenderDMReadsRowAdvancedInsideTransaction(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "senderread")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	for i := 0; i < 5; i++ {
		sendDM(t, srv.HTTPURL, aliceTok, conv, fmt.Sprintf("m%d", i))
	}

	status, env, raw := getJSON(t, srv.HTTPURL, "/api/dms", aliceTok)
	if status != http.StatusOK {
		t.Fatalf("listing: %d body=%s", status, raw)
	}
	var data struct {
		Conversations []struct {
			ID          string `json:"id"`
			UnreadCount int    `json:"unread_count"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(data.Conversations) != 1 || data.Conversations[0].ID != conv {
		t.Fatalf("expected one conversation with id %q; got %+v", conv, data.Conversations)
	}
	if data.Conversations[0].UnreadCount != 0 {
		t.Errorf("sender unread_count after 5 sends: got %d want 0 (dm_reads advanced atomically per §11)",
			data.Conversations[0].UnreadCount)
	}
}

// TestGetMessagesPaginationNewestFirst — confirms ULID-cursor newest-
// first paging shape, mirroring channel-message pagination.
func TestGetMessagesPaginationNewestFirst(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok, bobID, _ := userPair(t, srv, "pag")

	conv := createDM(t, srv.HTTPURL, aliceTok, bobID)
	var posted []string
	for i := 0; i < 5; i++ {
		posted = append(posted, sendDM(t, srv.HTTPURL, aliceTok, conv, fmt.Sprintf("m%d", i)))
	}

	status, env, raw := getJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/messages?limit=3", aliceTok)
	if status != http.StatusOK {
		t.Fatalf("page1: %d body=%s", status, raw)
	}
	var page1 struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*env.Data, &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Messages) != 3 {
		t.Fatalf("page1 length: got %d want 3", len(page1.Messages))
	}
	if page1.Messages[0].ID != posted[4] {
		t.Errorf("newest-first: page1[0]=%q want last posted %q", page1.Messages[0].ID, posted[4])
	}

	cursor := page1.Messages[2].ID
	status, env, raw = getJSON(t, srv.HTTPURL,
		"/api/dms/"+conv+"/messages?limit=10&before="+cursor, aliceTok)
	if status != http.StatusOK {
		t.Fatalf("page2: %d body=%s", status, raw)
	}
	var page2 struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*env.Data, &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Messages) != 2 {
		t.Fatalf("page2 length: got %d want 2", len(page2.Messages))
	}
	if page2.Messages[0].ID != posted[1] || page2.Messages[1].ID != posted[0] {
		t.Errorf("page2 ids: got %v want [%q, %q]",
			[]string{page2.Messages[0].ID, page2.Messages[1].ID}, posted[1], posted[0])
	}
}

// --- helpers below --------------------------------------------------

// dmFrame mirrors the wire shape of {type:"dm", data:{conversation, dm_message}}.
type dmFrame struct {
	Type string `json:"type"`
	Data struct {
		Conversation struct {
			ID   string `json:"id"`
			Peer struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"peer"`
			UnreadCount int `json:"unread_count"`
		} `json:"conversation"`
		DMMessage struct {
			ID             string `json:"id"`
			ConversationID string `json:"conversation_id"`
			SenderUserID   string `json:"sender_user_id"`
			Body           string `json:"body"`
		} `json:"dm_message"`
	} `json:"data"`
}

// createDM POSTs /api/dms and returns the new conversation id. Fails
// the test on a non-2xx.
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
	if data.ID == "" {
		t.Fatalf("POST /api/dms: empty id (body=%s)", raw)
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

// getJSON issues an authenticated GET and parses the envelope.
func getJSON(t *testing.T, httpURL, path, bearer string) (int, testsupport.Envelope, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpURL+path, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s body: %v", path, err)
	}
	var env testsupport.Envelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope from GET %s (status %d): %v body=%q",
				path, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode, env, raw
}

// dialUserInbox opens an authenticated /ws connection (no ?channel= so
// the L15 default-channel fallback applies and we still subscribe to
// `user:<viewer>`) and returns a buffered channel of received frames.
// The connection and goroutine are torn down via t.Cleanup.
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

// waitForDMFrame drains frames until it sees a {type:"dm"} envelope
// matching (conversationID, body) or hits a 5s timeout.
func waitForDMFrame(t *testing.T, frames chan []byte, conversationID, body string) dmFrame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw, ok := <-frames:
			if !ok {
				t.Fatalf("ws stream closed before {type:dm} frame for conv=%s body=%q",
					conversationID, body)
			}
			var probe struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				continue
			}
			if probe.Type != "dm" {
				continue
			}
			var f dmFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("decode dm frame: %v body=%s", err, raw)
			}
			if f.Data.Conversation.ID == conversationID && f.Data.DMMessage.Body == body {
				return f
			}
		case <-deadline:
			t.Fatalf("timed out waiting for {type:dm} frame for conv=%s body=%q",
				conversationID, body)
			return dmFrame{}
		}
	}
}

// statically reference bytes so the import survives if waitForDMFrame
// changes shape in a future edit.
var _ = bytes.NewReader
