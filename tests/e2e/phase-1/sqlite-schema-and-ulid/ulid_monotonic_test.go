package sqlite_schema_and_ulid_e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"
)

// AC-5: A package exposes a NewULID() helper (monotonic where possible).
//
// Black-box proxy for the unit-level invariant: the public REST surface
// mints message ids via apps/server/internal/ids.NewULID(). Driving the
// surface in two regimes — tight serial loop and concurrent fan-out —
// observes the helper's monotonic-where-possible guarantee through every
// production layer (handler → repo → sqlite write). Per
// specs/test-analysis/phase-1/sqlite-schema-and-ulid.md AC-5 sketch.
//
// Serial regime: 100 messages POSTed back-to-back. Every id must be a
// 26-char Crockford-base32 ULID, all 100 must be unique, and the
// response order must already be strictly lexicographically increasing.
// Same-millisecond posts are the interesting case here — that is where
// the monotonic entropy reader's increment is the only thing keeping
// ordering.
//
// Concurrent regime: 10 goroutines each POST 50 messages. Every id must
// be a 26-char Crockford-base32 ULID, all 500 must be unique, and the
// timestamp prefix must decode to within 5 seconds of the test's wall
// clock. Strict cross-goroutine ordering is *not* asserted (goroutine
// scheduling makes that brittle), but cross-goroutine uniqueness is —
// LockedMonotonicReader's job is to keep entropy mutation race-free.
func TestAC5_NewULIDMonotonicAcrossRapidCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-5 sends 600 messages end-to-end; skipped under -short")
	}

	srv := startServer(t)
	wantTime := time.Now()

	const username = "ulid-ac5"
	password := randomSecret(t, 12)
	token := registerUserAC5(t, srv, username, password)
	chID := createChannelAC5(t, srv, token, "ac5-monotonic")

	const serialCount = 100
	serialIDs := make([]string, 0, serialCount)
	for i := 0; i < serialCount; i++ {
		serialIDs = append(serialIDs,
			postMessageAC5(t, srv, token, chID, fmt.Sprintf("serial-%03d", i)))
	}

	if !sort.StringsAreSorted(serialIDs) {
		for i := 1; i < len(serialIDs); i++ {
			if serialIDs[i] <= serialIDs[i-1] {
				t.Errorf("serial ids not strictly increasing at index %d: %q !> %q",
					i, serialIDs[i], serialIDs[i-1])
			}
		}
	}
	for i := 1; i < len(serialIDs); i++ {
		if serialIDs[i] == serialIDs[i-1] {
			t.Errorf("duplicate serial id at index %d: %q", i, serialIDs[i])
		}
	}
	for i, id := range serialIDs {
		assertULIDAC5(t, fmt.Sprintf("serial[%d]", i), id, wantTime)
	}

	const goroutines = 10
	const perGoroutine = 50
	concurrentIDs := make([][]string, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		concurrentIDs[g] = make([]string, 0, perGoroutine)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				concurrentIDs[g] = append(concurrentIDs[g],
					postMessageAC5(t, srv, token, chID, fmt.Sprintf("g%d-%02d", g, i)))
			}
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine+serialCount)
	for _, id := range serialIDs {
		seen[id] = struct{}{}
	}
	for g, slice := range concurrentIDs {
		for i, id := range slice {
			if _, dup := seen[id]; dup {
				t.Errorf("duplicate id from concurrent goroutine %d index %d: %q",
					g, i, id)
			}
			seen[id] = struct{}{}
			assertULIDAC5(t, fmt.Sprintf("concurrent[g=%d,i=%d]", g, i), id, wantTime)
		}
	}
}

// crockfordULIDReAC5 pins the Crockford base32 alphabet ULID v2 uses:
// digits 0-9 and the letters A-Z minus I, L, O, U. Length is exactly 26
// (10 chars timestamp + 16 chars randomness). Suffixed -AC5 to avoid a
// collision with PR #348's identical regex if both land in this package.
var crockfordULIDReAC5 = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// crockfordIndexAC5 maps each Crockford base32 character to its 5-bit
// value; -1 for non-alphabet bytes.
var crockfordIndexAC5 = func() [256]int {
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

func assertULIDAC5(t *testing.T, label, id string, around time.Time) {
	t.Helper()
	if len(id) != 26 {
		t.Errorf("%s = %q has length %d, want 26 (ULID)", label, id, len(id))
		return
	}
	if !crockfordULIDReAC5.MatchString(id) {
		t.Errorf("%s = %q is not Crockford base32 (alphabet 0-9 A-Z minus I L O U)",
			label, id)
		return
	}
	ts := decodeULIDTimestampAC5(id)
	delta := ts.Sub(around)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("%s = %q timestamp %s is more than 5s from %s (delta=%s)",
			label, id, ts.Format(time.RFC3339Nano),
			around.Format(time.RFC3339Nano), delta)
	}
}

// decodeULIDTimestampAC5 decodes the 10-char Crockford-base32 timestamp
// prefix (48 bits ms-since-epoch, big-endian) per
// github.com/oklog/ulid/v2 layout.
func decodeULIDTimestampAC5(id string) time.Time {
	var ms uint64
	for i := 0; i < 10; i++ {
		v := crockfordIndexAC5[id[i]]
		if v < 0 {
			return time.Time{}
		}
		ms = ms<<5 | uint64(v)
	}
	return time.UnixMilli(int64(ms)) //nolint:gosec // G115: 48-bit ULID timestamp fits in int64.
}

// envelopeAC5 mirrors the {ok,data,error} response shape from PRD §10.
// Suffixed -AC5 to avoid colliding with PR #348's envelope type if both
// PRs land.
type envelopeAC5 struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func registerUserAC5(t *testing.T, srv *runningServer, username, password string) string {
	t.Helper()
	status, raw := postJSONRawAC5(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register: status %d body=%s", status, raw)
	}
	env := decodeEnvelopeAC5(t, raw)
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
	if data.Token == "" {
		t.Fatalf("register: empty token body=%s", raw)
	}
	return data.Token
}

func createChannelAC5(t *testing.T, srv *runningServer, token, name string) string {
	t.Helper()
	status, raw := postJSONRawAC5(t, srv, "/api/channels", token,
		map[string]string{"name": name})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create channel: status %d body=%s", status, raw)
	}
	env := decodeEnvelopeAC5(t, raw)
	if !env.OK || env.Data == nil {
		t.Fatalf("create channel envelope ok=%v error=%v body=%s",
			env.OK, env.Error, raw)
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

func postMessageAC5(t *testing.T, srv *runningServer, token, channelID, body string) string {
	t.Helper()
	status, raw := postJSONRawAC5(t, srv,
		"/api/channels/"+channelID+"/messages", token,
		map[string]string{"body": body})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("post message %q: status %d body=%s", body, status, raw)
	}
	env := decodeEnvelopeAC5(t, raw)
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
	return msg.ID
}

func decodeEnvelopeAC5(t *testing.T, raw []byte) envelopeAC5 {
	t.Helper()
	var env envelopeAC5
	if len(raw) == 0 {
		return env
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, raw)
	}
	return env
}

func postJSONRawAC5(t *testing.T, srv *runningServer, path, bearer string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s body: %v", path, err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, srv.httpURL+path, &buf) //nolint:noctx // test helper, loopback URL.
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
