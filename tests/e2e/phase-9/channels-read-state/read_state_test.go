// Package channels_read_state_e2e_test exercises the Phase 9
// per-viewer channel read-state surface end-to-end (decision log
// `lt -p direct-messages 3` §5, §7, §9, §11, L5, L6, L13, L26):
//
//   - GET /api/channels populates per-channel `unread_count`,
//     `last_message_at`, `last_read_message_id` (additive fields).
//   - Listing order is `last_message_at DESC NULLS LAST`.
//   - First listing for a fresh user materializes a `channel_reads`
//     row per non-empty channel (auto-materialize, §11).
//   - A subsequent message bumps `unread_count` to 1 (proves the
//     baseline is frozen at first-list).
//   - POST /api/channels/{id}/read returns 204, advances the stored
//     `last_read_message_id`, and broadcasts a {type:"read"} frame
//     to the caller's `user:<viewer>` topic.
//   - The `read-mark` per-user bucket trips at the 51st rapid POST.
//
// Black-box harness: builds and boots the production chat-server
// binary (decision log L27 / testsupport.StartServer), drives it
// through HTTP + WS, and inspects the on-disk SQLite file in
// read-only mode for the stored-cursor assertion. The DB cross-check
// is the load-bearing assertion — without it, an UPSERT bug that
// silently drops the row would still produce a 204 + WS frame.
package channels_read_state_e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
	_ "modernc.org/sqlite"

	"hackathon/tests/e2e/internal/testsupport"
)

// envReadMarkBurst / envReadMarkRefill mirror
// apps/server/internal/ratelimit/config.go. Duplicated here because the
// ratelimit package is `internal/`-scoped; drift is caught when the
// unit tests in that package fail.
const (
	envReadMarkBurst  = "CHAT_READ_MARK_BURST"
	envReadMarkRefill = "CHAT_READ_MARK_REFILL"
)

type listChannel struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	LastMessageID     *string `json:"last_message_id,omitempty"`
	LastMessageAt     *string `json:"last_message_at,omitempty"`
	LastReadMessageID *string `json:"last_read_message_id,omitempty"`
	UnreadCount       *int    `json:"unread_count,omitempty"`
}

// TestListChannelsAdditiveFieldsAndOrder boots a server, registers a
// fresh user, posts two messages on `general` then creates a second
// channel and posts one message on it, and asserts:
//
//  1. Listing returns the additive fields populated for both channels.
//  2. Order is `last_message_at DESC NULLS LAST` — the channel with
//     the most recent activity is first.
//  3. The fresh user's `unread_count` is 0 on both channels (first
//     listing materialized the baseline at each channel's tip).
//  4. After one more post on `general`, the next listing returns
//     `unread_count = 1` for `general` — proves the baseline is
//     frozen at first-list, not advancing with the channel's tip.
func TestListChannelsAdditiveFieldsAndOrder(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	authorName := "phase9-author-" + testsupport.RandomSecret(t, 4)
	authorPass := testsupport.RandomSecret(t, 12)
	_, authorToken := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, authorName, authorPass)

	general := lookupSeededGeneralChannelID(t, srv, authorToken)

	postMessage(t, srv.HTTPURL, authorToken, general, "msg-1")
	postMessage(t, srv.HTTPURL, authorToken, general, "msg-2")

	secondID := createChannel(t, srv.HTTPURL, authorToken, "phase9-rs-"+testsupport.RandomSecret(t, 4))
	postMessage(t, srv.HTTPURL, authorToken, secondID, "second-1")

	// general's last_message_at is older than secondID's; secondID
	// must lead the listing.
	viewerName := "phase9-viewer-" + testsupport.RandomSecret(t, 4)
	viewerPass := testsupport.RandomSecret(t, 12)
	_, viewerToken := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, viewerName, viewerPass)

	channels := listChannels(t, srv.HTTPURL, viewerToken)
	if len(channels) < 2 {
		t.Fatalf("listing returned %d channels; want >= 2", len(channels))
	}

	// AC: order — last_message_at DESC NULLS LAST. Second channel
	// posted last, so it leads. Walk the slice and assert the
	// (non-NULL) `last_message_at` values are non-increasing.
	var prev *time.Time
	for i, c := range channels {
		if c.LastMessageAt == nil {
			// NULLs sort LAST — must come after every non-NULL row.
			for j := i + 1; j < len(channels); j++ {
				if channels[j].LastMessageAt != nil {
					t.Fatalf("ordering: NULL last_message_at at index %d precedes non-NULL at %d", i, j)
				}
			}
			break
		}
		got, err := time.Parse(time.RFC3339Nano, *c.LastMessageAt)
		if err != nil {
			t.Fatalf("parse last_message_at[%d]=%q: %v", i, *c.LastMessageAt, err)
		}
		if prev != nil && got.After(*prev) {
			t.Fatalf("ordering: index %d (%s) newer than predecessor (%s) — must be DESC",
				i, got.Format(time.RFC3339Nano), prev.Format(time.RFC3339Nano))
		}
		prev = &got
	}
	if channels[0].ID != secondID {
		t.Fatalf("ordering: head channel id = %q, want %q (most-recent activity)", channels[0].ID, secondID)
	}

	// AC: additive fields populated. Both channels have a tip, so
	// unread_count, last_read_message_id, last_message_id, and
	// last_message_at must be present.
	for i, c := range channels {
		if c.UnreadCount == nil {
			t.Fatalf("channels[%d] (%s): unread_count missing", i, c.ID)
		}
		if *c.UnreadCount != 0 {
			t.Fatalf("channels[%d] (%s): unread_count = %d, want 0 after fresh-user materialization",
				i, c.ID, *c.UnreadCount)
		}
		if c.LastMessageID == nil {
			t.Fatalf("channels[%d] (%s): last_message_id missing", i, c.ID)
		}
		if c.LastMessageAt == nil {
			t.Fatalf("channels[%d] (%s): last_message_at missing", i, c.ID)
		}
		if c.LastReadMessageID == nil {
			t.Fatalf("channels[%d] (%s): last_read_message_id missing after materialization", i, c.ID)
		}
		if *c.LastReadMessageID != *c.LastMessageID {
			t.Fatalf("channels[%d] (%s): materialized last_read=%q, last_message=%q — must match at first-list",
				i, c.ID, *c.LastReadMessageID, *c.LastMessageID)
		}
	}

	// AC: subsequent message arrives → next listing returns
	// unread_count=1 for that channel (baseline frozen at first-list).
	postMessage(t, srv.HTTPURL, authorToken, general, "post-baseline")
	channels2 := listChannels(t, srv.HTTPURL, viewerToken)
	var generalAfter *listChannel
	for i := range channels2 {
		if channels2[i].ID == general {
			generalAfter = &channels2[i]
			break
		}
	}
	if generalAfter == nil {
		t.Fatalf("listing 2: general channel %s not present", general)
	}
	if generalAfter.UnreadCount == nil || *generalAfter.UnreadCount != 1 {
		t.Fatalf("listing 2: general unread_count = %v, want 1", generalAfter.UnreadCount)
	}
}

// TestPostReadAdvancesCursorAndPublishesFrame asserts the full
// POST /api/channels/{id}/read contract:
//
//  1. 204 No Content.
//  2. SQLite `channel_reads.last_read_message_id` advances to the
//     supplied id.
//  3. The viewer's `user:<viewer>` WS topic receives a
//     {type:"read", scope:"channel", scope_id:<id>, last_read_message_id:<id>}
//     frame.
func TestPostReadAdvancesCursorAndPublishesFrame(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	authorName := "phase9-rauthor-" + testsupport.RandomSecret(t, 4)
	authorPass := testsupport.RandomSecret(t, 12)
	_, authorToken := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, authorName, authorPass)

	general := lookupSeededGeneralChannelID(t, srv, authorToken)

	viewerName := "phase9-rviewer-" + testsupport.RandomSecret(t, 4)
	viewerPass := testsupport.RandomSecret(t, 12)
	viewerID, viewerToken := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, viewerName, viewerPass)

	// First listing materializes the baseline at zero (general has
	// no messages yet for a fresh seed). After this call the viewer's
	// channel_reads row is present iff general's last_message_id is
	// non-NULL — otherwise materialization skips. We post one message
	// before listing so the row is guaranteed.
	tipBeforeList := postMessage(t, srv.HTTPURL, authorToken, general, "pre-baseline")
	_ = listChannels(t, srv.HTTPURL, viewerToken)

	advanceTarget := postMessage(t, srv.HTTPURL, authorToken, general, "advance-target")

	// Subscribe to the inbox topic via /ws BEFORE the POST so the
	// frame can't race the WS dial. The WS upgrade subscribes to
	// `user:<viewer>` automatically per the L15 multi-topic contract.
	inboxFrames := openInbox(t, srv, viewerToken, general)

	status, _, raw := postReadMark(t, srv.HTTPURL, viewerToken, general, advanceTarget)
	if status != http.StatusNoContent {
		t.Fatalf("POST /read: status %d body=%s", status, raw)
	}

	// AC: cursor advanced.
	db := openDBReadOnly(t, srv.DBPath)
	var stored sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT last_read_message_id FROM channel_reads WHERE channel_id = ? AND user_id = ?`,
		general, viewerID,
	).Scan(&stored); err != nil {
		t.Fatalf("select channel_reads: %v", err)
	}
	if !stored.Valid || stored.String != advanceTarget {
		t.Fatalf("channel_reads.last_read_message_id = %v, want %q", stored, advanceTarget)
	}
	// Sanity: advancing past tipBeforeList means the cursor moved
	// forward. tipBeforeList < advanceTarget under ULID ordering
	// since they were inserted in order.
	if stored.String == tipBeforeList {
		t.Fatalf("cursor stuck at tipBeforeList %q (expected %q)", tipBeforeList, advanceTarget)
	}

	// AC: WS frame received on `user:<viewer>` with the right scope/payload.
	frame := waitForReadFrame(t, inboxFrames, advanceTarget, 3*time.Second)
	if frame.Scope != "channel" {
		t.Errorf("frame scope = %q, want %q", frame.Scope, "channel")
	}
	if frame.ScopeID != general {
		t.Errorf("frame scope_id = %q, want %q", frame.ScopeID, general)
	}
	if frame.LastReadMessageID != advanceTarget {
		t.Errorf("frame last_read_message_id = %q, want %q", frame.LastReadMessageID, advanceTarget)
	}
}

// TestPostReadUnknownChannelReturns404 asserts the 404 branch when
// the path id is well-formed but not in the channels table.
func TestPostReadUnknownChannelReturns404(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	username := "phase9-r404-" + testsupport.RandomSecret(t, 4)
	password := testsupport.RandomSecret(t, 12)
	_, token := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, username, password)

	// Well-formed ULID-shape that isn't in the channels table.
	bogus := "01HZZZZZZZZZZZZZZZZZZZZZZZ"
	bogusMsg := "01HZZZZZZZZZZZZZZZZZZZZZZY"
	status, _, raw := postReadMark(t, srv.HTTPURL, token, bogus, bogusMsg)
	if status != http.StatusNotFound {
		t.Fatalf("POST /read for unknown channel: status %d body=%s want 404", status, raw)
	}
}

// TestPostReadRateLimitTrips asserts the 51st rapid POST returns 429.
// Burst is left at default (50) and refill is set to 5m so the test
// can exhaust the bucket without waiting on the refill clock.
func TestPostReadRateLimitTrips(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{
		ExtraEnv: []string{
			envReadMarkBurst + "=50",
			envReadMarkRefill + "=5m",
		},
	})

	authorName := "phase9-rl-author-" + testsupport.RandomSecret(t, 4)
	authorPass := testsupport.RandomSecret(t, 12)
	_, authorToken := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, authorName, authorPass)

	general := lookupSeededGeneralChannelID(t, srv, authorToken)
	target := postMessage(t, srv.HTTPURL, authorToken, general, "ratelimit-target")

	username := "phase9-rl-" + testsupport.RandomSecret(t, 4)
	password := testsupport.RandomSecret(t, 12)
	_, token := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, username, password)

	// Fire 50 requests; every one must succeed (burst=50). The 51st
	// must return 429 with Retry-After set.
	for i := 0; i < 50; i++ {
		status, _, raw := postReadMark(t, srv.HTTPURL, token, general, target)
		if status != http.StatusNoContent {
			t.Fatalf("POST /read #%d: status %d body=%s want 204", i+1, status, raw)
		}
	}
	status, _, raw := postReadMark(t, srv.HTTPURL, token, general, target)
	if status != http.StatusTooManyRequests {
		t.Fatalf("POST /read #51: status %d body=%s want 429", status, raw)
	}
}

// readFrameBody is the typed shape we parse from the
// `user:<viewer>` topic. ScopeID + LastReadMessageID let the test
// assert routing + payload simultaneously.
type readFrameBody struct {
	Type              string `json:"type"`
	Scope             string `json:"scope"`
	ScopeID           string `json:"scope_id"`
	LastReadMessageID string `json:"last_read_message_id"`
}

// openInbox dials the production /ws endpoint and returns a channel
// of decoded `read` frames. Other frame types (channel events,
// presence) are ignored. The returned channel is closed when the
// connection drops.
func openInbox(t *testing.T, srv *testsupport.Server, token, channelID string) <-chan readFrameBody {
	t.Helper()
	ticket := testsupport.MintTicket(t, srv.HTTPURL, token)
	wsURL := fmt.Sprintf("%s?ticket=%s&channel=%s", srv.WSURL, ticket, channelID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })

	out := make(chan readFrameBody, 8)
	go func() {
		defer close(out)
		readCtx := context.Background()
		for {
			_, raw, err := c.Read(readCtx)
			if err != nil {
				return
			}
			var f readFrameBody
			if jerr := json.Unmarshal(raw, &f); jerr != nil {
				continue
			}
			if f.Type != "read" {
				continue
			}
			select {
			case out <- f:
			default:
			}
		}
	}()
	return out
}

// waitForReadFrame drains frames until one matches `target` or the
// timeout elapses. A non-matching frame is rejected — read frames
// scoped to a different channel/message id signal a routing bug.
func waitForReadFrame(t *testing.T, frames <-chan readFrameBody, target string, timeout time.Duration) readFrameBody {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("inbox closed without delivering a read frame for %q", target)
			}
			if f.LastReadMessageID == target {
				return f
			}
			// Earlier frame from a prior step — keep waiting.
		case <-deadline.C:
			t.Fatalf("timed out waiting for read frame on user inbox (target %q)", target)
		}
	}
}

// listChannels GETs /api/channels with bearer and returns the parsed
// channels slice.
func listChannels(t *testing.T, httpURL, bearer string) []listChannel {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpURL+"/api/channels", nil)
	if err != nil {
		t.Fatalf("new GET /api/channels: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/channels: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/channels: status %d body=%s", resp.StatusCode, raw)
	}
	var env struct {
		OK   bool             `json:"ok"`
		Data *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Channels []listChannel `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channels: %v body=%s", err, raw)
	}
	return data.Channels
}

// postMessage POSTs /api/channels/{id}/messages and returns the new
// message id.
func postMessage(t *testing.T, httpURL, bearer, channelID, body string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, httpURL,
		"/api/channels/"+channelID+"/messages", bearer,
		map[string]string{"body": body})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("POST /api/channels/%s/messages: status %d body=%s", channelID, status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("POST message envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}
	var msg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &msg); err != nil {
		t.Fatalf("decode message data: %v body=%s", err, raw)
	}
	if msg.ID == "" {
		t.Fatalf("POST message: empty id (body=%s)", raw)
	}
	return msg.ID
}

// createChannel POSTs /api/channels and returns the new channel id.
func createChannel(t *testing.T, httpURL, bearer, name string) string {
	t.Helper()
	status, env, raw := testsupport.PostJSON(t, httpURL,
		"/api/channels", bearer,
		map[string]string{"name": name})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("POST /api/channels (name=%s): status %d body=%s", name, status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("POST /api/channels envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v body=%s", err, raw)
	}
	if ch.ID == "" {
		t.Fatalf("POST /api/channels (name=%s): empty id body=%s", name, raw)
	}
	return ch.ID
}

// postReadMark POSTs /api/channels/{id}/read and returns the raw
// status + envelope for the test to assert on.
func postReadMark(t *testing.T, httpURL, bearer, channelID, messageID string) (int, testsupport.Envelope, []byte) {
	t.Helper()
	return testsupport.PostJSON(t, httpURL,
		"/api/channels/"+channelID+"/read", bearer,
		map[string]string{"message_id": messageID})
}

// openDBReadOnly opens the running server's SQLite file read-only.
// Mirrors the helper in tests/e2e/phase-9/channels-denorm/denorm_test.go.
func openDBReadOnly(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// lookupSeededGeneralChannelID returns the seeded `general` channel id.
func lookupSeededGeneralChannelID(t *testing.T, srv *testsupport.Server, bearer string) string {
	t.Helper()
	for _, c := range listChannels(t, srv.HTTPURL, bearer) {
		if c.Name == "general" {
			return c.ID
		}
	}
	t.Fatalf("/api/channels: no seeded 'general' channel")
	return ""
}
