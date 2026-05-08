// Package channels_denorm_e2e_test asserts the InsertMessageTx
// transaction populates `channels.last_message_id` and
// `channels.last_message_at` atomically with each channel-message
// insert (issue #864 / decision log L11 + L21; cold-pass SC3).
//
// Black-box harness: boot the production chat-server binary via the
// shared testsupport.StartServer helper (decision log L27), POST one
// message through /api/channels/{id}/messages, then read the on-disk
// SQLite file in read-only mode and SELECT the two denormalized
// columns. SC3 tightens the AC from "tests pass" to a concrete SQL
// assertion: last_message_id equals the just-inserted message id and
// last_message_at is non-NULL within 5s of the wall clock at POST.
package channels_denorm_e2e_test

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

	_ "modernc.org/sqlite"

	"hackathon/tests/e2e/internal/testsupport"
)

// TestInsertMessageTxUpdatesChannelDenorm POSTs one message and
// asserts the channel row's denormalized columns reflect it. The
// 5-second window is deliberate slack for slow CI boxes (decision log
// SC3 picks 5s as a generous bound while still catching a stuck
// clock or a missing UPDATE).
func TestInsertMessageTxUpdatesChannelDenorm(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	username := "phase9-denorm-" + testsupport.RandomSecret(t, 4)
	password := testsupport.RandomSecret(t, 12)
	_, token := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, username, password)

	channelID := lookupSeededGeneralChannelID(t, srv, token)

	beforePOST := time.Now()
	msgID := postMessage(t, srv.HTTPURL, token, channelID, "hi from denorm e2e")
	afterPOST := time.Now()

	db := openDBReadOnly(t, srv.DBPath)
	ctx := context.Background()

	var (
		dbLastMsgID sql.NullString
		dbLastMsgAt sql.NullTime
	)
	if err := db.QueryRowContext(ctx,
		`SELECT last_message_id, last_message_at FROM channels WHERE id = ?`,
		channelID,
	).Scan(&dbLastMsgID, &dbLastMsgAt); err != nil {
		t.Fatalf("select channels.last_message_*: %v", err)
	}

	if !dbLastMsgID.Valid {
		t.Fatalf("channels.last_message_id is NULL after POST; want %q", msgID)
	}
	if dbLastMsgID.String != msgID {
		t.Errorf("channels.last_message_id = %q, want %q (returned by POST)",
			dbLastMsgID.String, msgID)
	}

	if !dbLastMsgAt.Valid {
		t.Fatalf("channels.last_message_at is NULL after POST")
	}
	// Window: [beforePOST - 5s, afterPOST + 5s]. 5s on either side
	// follows SC3's "within 5s of now"; clock-skew between the test
	// process and the spawned server is the same wall clock here, so
	// the slack only needs to absorb test-host scheduler jitter.
	lower := beforePOST.Add(-5 * time.Second)
	upper := afterPOST.Add(5 * time.Second)
	got := dbLastMsgAt.Time
	if got.Before(lower) || got.After(upper) {
		t.Errorf("channels.last_message_at = %s, want within [%s, %s] (5s window around POST)",
			got.Format(time.RFC3339Nano),
			lower.Format(time.RFC3339Nano),
			upper.Format(time.RFC3339Nano))
	}
}

// postMessage POSTs /api/channels/{id}/messages with the given body
// and returns the persisted message id from the success envelope.
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

// openDBReadOnly opens the running server's SQLite file read-only so
// SELECTs do not contend with the server's writes. mode=ro errors out
// rather than creating the file if the path is wrong. Mirrors the
// pattern in tests/e2e/phase-9/migration/migration_test.go.
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

// lookupSeededGeneralChannelID lists channels with the given bearer
// token and returns the seeded "general" channel id. Phase 3 seeds
// this row on first boot so every running server has it.
func lookupSeededGeneralChannelID(t *testing.T, srv *testsupport.Server, bearer string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.HTTPURL+"/api/channels", nil)
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
		t.Fatalf("GET /api/channels: status %d body %s", resp.StatusCode, raw)
	}
	var env struct {
		OK   bool             `json:"ok"`
		Data *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&env); err != nil {
		t.Fatalf("decode /api/channels: %v body %s", err, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/api/channels envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode channels list: %v body %s", err, raw)
	}
	for _, ch := range data.Channels {
		if ch.Name == "general" {
			return ch.ID
		}
	}
	t.Fatalf("/api/channels: no seeded 'general' in %s", raw)
	return ""
}
