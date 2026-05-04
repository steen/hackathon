package sqlite_schema_and_ulid_e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// AC-4: ULIDs are used as primary keys for users, channels, and messages.
//
// Black-box: boot the real apps/server binary, exercise the public REST
// endpoints to create one row in each of the three tables (users via
// /api/auth/register, channels via POST /api/channels, messages via
// POST /api/channels/{id}/messages), then read each id back from the
// on-disk SQLite file and assert it satisfies the ULID shape: 26 chars,
// Crockford base32 alphabet, and a timestamp prefix that decodes to a
// time within a few seconds of the test's wall clock. As a secondary
// check, send three messages back-to-back and assert their ids are
// strictly lexicographically increasing — the sort property a ULID
// guarantees and an INTEGER autoincrement does not (so this test would
// fail under either a non-ULID id scheme or a clock-skew regression
// strong enough to break ordering).
func TestAC4_ULIDsAsPrimaryKeysForUsersChannelsMessages(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	const channelName = "general"

	wantTime := time.Now()

	uid, token := registerUser(t, srv, username, password)
	chID := createChannel(t, srv, token, channelName)
	msgIDs := postMessages(t, srv, token, chID, []string{"hi", "there", "again"})

	db := openDBReadOnly(t, srv)

	var dbUserID string
	if err := db.QueryRow(
		`SELECT id FROM users WHERE username = ?`, username,
	).Scan(&dbUserID); err != nil {
		t.Fatalf("select user id: %v", err)
	}
	if dbUserID != uid {
		t.Errorf("users.id from DB = %q, register response said %q", dbUserID, uid)
	}
	assertULID(t, "users.id", dbUserID, wantTime)

	var dbChannelID string
	if err := db.QueryRow(
		`SELECT id FROM channels WHERE name = ?`, channelName,
	).Scan(&dbChannelID); err != nil {
		t.Fatalf("select channel id: %v", err)
	}
	if dbChannelID != chID {
		t.Errorf("channels.id from DB = %q, create response said %q", dbChannelID, chID)
	}
	assertULID(t, "channels.id", dbChannelID, wantTime)

	rows, err := db.Query(
		`SELECT id FROM messages WHERE channel_id = ? ORDER BY id ASC`, chID,
	)
	if err != nil {
		t.Fatalf("select message ids: %v", err)
	}
	defer rows.Close()
	var dbMsgIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan message id: %v", err)
		}
		dbMsgIDs = append(dbMsgIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iter message ids: %v", err)
	}
	if len(dbMsgIDs) != len(msgIDs) {
		t.Fatalf("messages row count = %d, want %d", len(dbMsgIDs), len(msgIDs))
	}
	for i, id := range dbMsgIDs {
		assertULID(t, fmt.Sprintf("messages.id[%d]", i), id, wantTime)
	}
	for i := 1; i < len(dbMsgIDs); i++ {
		if dbMsgIDs[i] <= dbMsgIDs[i-1] {
			t.Errorf("messages.id not strictly increasing at index %d: %q !> %q",
				i, dbMsgIDs[i], dbMsgIDs[i-1])
		}
	}
	for i, want := range msgIDs {
		if dbMsgIDs[i] != want {
			t.Errorf("messages.id[%d] from DB = %q, POST response said %q",
				i, dbMsgIDs[i], want)
		}
	}
}

// crockfordULIDRe pins the Crockford base32 alphabet ULID v2 uses: the
// digits 0-9 and the letters A-Z minus I, L, O, U. Length is exactly
// 26 (10 chars of timestamp + 16 chars of randomness).
var crockfordULIDRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// crockfordIndex maps each Crockford base32 character to its 5-bit
// value. Index = -1 for non-alphabet characters.
var crockfordIndex = func() [256]int {
	var idx [256]int
	for i := range idx {
		idx[i] = -1
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for i := 0; i < len(alphabet); i++ {
		idx[alphabet[i]] = i
	}
	return idx
}()

func assertULID(t *testing.T, label, id string, around time.Time) {
	t.Helper()
	if len(id) != 26 {
		t.Errorf("%s = %q has length %d, want 26 (ULID)", label, id, len(id))
		return
	}
	if !crockfordULIDRe.MatchString(id) {
		t.Errorf("%s = %q is not Crockford base32 (alphabet 0-9 A-Z minus I L O U)",
			label, id)
		return
	}
	// Reject pure-numeric ids: an INTEGER autoincrement formatted as text
	// would still pass the alphabet check but lack any letters.
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		t.Errorf("%s = %q parses as decimal — not a ULID-shaped id", label, id)
	}
	ts := decodeULIDTimestamp(id)
	delta := ts.Sub(around)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("%s = %q timestamp %s is more than 5s from %s (delta=%s)",
			label, id, ts.Format(time.RFC3339Nano), around.Format(time.RFC3339Nano), delta)
	}
}

// decodeULIDTimestamp decodes the 10-char Crockford-base32 timestamp
// prefix to milliseconds since the Unix epoch. The 26-char ULID layout
// is documented at github.com/oklog/ulid: 48 bits of ms-since-epoch
// followed by 80 bits of randomness, packed big-endian.
func decodeULIDTimestamp(id string) time.Time {
	var ms uint64
	for i := 0; i < 10; i++ {
		v := crockfordIndex[id[i]]
		if v < 0 {
			return time.Time{}
		}
		ms = ms<<5 | uint64(v)
	}
	return time.UnixMilli(int64(ms))
}

func registerUser(t *testing.T, srv *runningServer, username, password string) (userID, token string) {
	t.Helper()
	status, raw := postJSONRaw(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register: status %d body=%s", status, raw)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK || env.Data == nil {
		t.Fatalf("register envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}
	var data struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.User.ID == "" {
		t.Fatalf("register: empty user.id body=%s", raw)
	}
	if data.Token == "" {
		t.Fatalf("register: empty token body=%s", raw)
	}
	return data.User.ID, data.Token
}

func createChannel(t *testing.T, srv *runningServer, token, name string) string {
	t.Helper()
	status, raw := postJSONRaw(t, srv, "/api/channels", token, map[string]string{
		"name": name,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create channel: status %d body=%s", status, raw)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK || env.Data == nil {
		t.Fatalf("create channel envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel data: %v body=%s", err, raw)
	}
	if ch.ID == "" {
		t.Fatalf("create channel: empty id body=%s", raw)
	}
	return ch.ID
}

func postMessages(t *testing.T, srv *runningServer, token, channelID string, bodies []string) []string {
	t.Helper()
	out := make([]string, 0, len(bodies))
	for _, body := range bodies {
		status, raw := postJSONRaw(t, srv, "/api/channels/"+channelID+"/messages", token,
			map[string]string{"body": body})
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("post message %q: status %d body=%s", body, status, raw)
		}
		env := decodeEnvelope(t, raw)
		if !env.OK || env.Data == nil {
			t.Fatalf("post message envelope ok=%v error=%v body=%s",
				env.OK, env.Error, raw)
		}
		var msg struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(*env.Data, &msg); err != nil {
			t.Fatalf("decode message data: %v body=%s", err, raw)
		}
		if msg.ID == "" {
			t.Fatalf("post message: empty id body=%s", raw)
		}
		out = append(out, msg.ID)
	}
	return out
}

// envelopeAC4 mirrors the response envelope used across the API. Named
// to avoid colliding with helpers that may land in this package later.
type envelopeAC4 struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, raw []byte) envelopeAC4 {
	t.Helper()
	var env envelopeAC4
	if len(raw) == 0 {
		return env
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, raw)
	}
	return env
}

func postJSONRaw(t *testing.T, srv *runningServer, path, bearer string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s body: %v", path, err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+path, &buf)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, raw
}
