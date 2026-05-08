package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ratelimit"
	"hackathon/apps/server/internal/repo"
)

// channelsFixture extends the auth fixture with a channels+messages
// handler pair wired against the same DB. We re-open the SQLite tempfile
// here so the channels test file is self-contained.
type channelsFixture struct {
	*fixture
	repo     *repo.Repo
	hub      *hub.Hub
	channels *ChannelsHandlers
	messages *MessagesHandlers
	mux      *stdhttp.ServeMux
}

func newChannelsFixture(t *testing.T) *channelsFixture {
	t.Helper()
	return newChannelsFixtureWithLimit(t, nil)
}

// newChannelsFixtureWithLimit lets a test wire the per-user channel-write
// limiter so the 429 path is exercised with the full middleware stack.
// Pass nil to disable the limiter (default).
func newChannelsFixtureWithLimit(t *testing.T, writeLimit func(stdhttp.Handler) stdhttp.Handler) *channelsFixture {
	t.Helper()
	f := newFixture(t)
	r, err := repo.New(f.db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	h := hub.New()
	ch := NewChannelsHandlers(ChannelsDeps{Repo: r, Hub: h, Now: time.Now})
	ms := NewMessagesHandlers(MessagesDeps{Repo: r, Hub: h, Now: time.Now})

	mux := stdhttp.NewServeMux()
	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            f.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	ch.Routes(mux, require, writeLimit, ms)
	return &channelsFixture{
		fixture:  f,
		repo:     r,
		hub:      h,
		channels: ch,
		messages: ms,
		mux:      mux,
	}
}

func (cf *channelsFixture) do(t *testing.T, method, path string, body interface{}, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	cf.mux.ServeHTTP(rr, req)
	return rr
}

// auth-only assertions: every endpoint must reject a missing bearer.
func TestChannelsEndpointsRequireAuth(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	cases := []struct {
		method, path string
	}{
		{stdhttp.MethodGet, "/api/channels"},
		{stdhttp.MethodPost, "/api/channels"},
		{stdhttp.MethodGet, "/api/channels/01HZZ00000000000000000ZZZZ/messages"},
		{stdhttp.MethodPost, "/api/channels/01HZZ00000000000000000ZZZZ/messages"},
	}
	for _, c := range cases {
		rr := cf.do(t, c.method, c.path, nil, "")
		if rr.Code != stdhttp.StatusUnauthorized {
			t.Errorf("%s %s: got %d want 401", c.method, c.path, rr.Code)
		}
	}
}

// US-3 — list returns the channels you've created.
func TestListChannelsReturnsCreatedChannels(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "general"}, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "random"}, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	rr := cf.do(t, stdhttp.MethodGet, "/api/channels", nil, tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("list: %d", rr.Code)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Channels []repo.Channel `json:"channels"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || len(env.Data.Channels) != 2 {
		t.Fatalf("got %+v want 2 channels", env)
	}
}

// US-4 — create persists and returns it.
func TestCreateChannelPersistsAndReturnsIt(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "general"}, tok)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("status: got %d want 201; body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		OK   bool         `json:"ok"`
		Data repo.Channel `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Name != "general" || env.Data.ID == "" {
		t.Fatalf("data: %+v", env.Data)
	}
}

// US-4 — duplicate name returns 409.
func TestCreateChannelRejectsDuplicateName(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	body := map[string]string{"name": "dup"}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels", body, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("first: %d", rr.Code)
	}
	rr := cf.do(t, stdhttp.MethodPost, "/api/channels", body, tok)
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("second: got %d want 409", rr.Code)
	}
}

// US-4 — invalid names rejected with 400.
func TestCreateChannelRejectsInvalidName(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	bad := []string{"", "UPPER", "with spaces", "-leading", "way-too-long-channel-name-blah-blah-blah-extra"}
	for _, name := range bad {
		rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
			map[string]string{"name": name}, tok)
		if rr.Code != stdhttp.StatusBadRequest {
			t.Errorf("name=%q: got %d want 400", name, rr.Code)
		}
	}
}

// Phase 8 — POST /api/channels success emits a `channel:create` frame to
// every connected WS client. Subscribed to a sentinel channel on the
// hub so BroadcastAll has a target.
func TestCreateChannelBroadcastsCreateEvent(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "general"}, tok)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("broadcast count: got %d want 1", len(got))
	}
	var frame struct {
		Type string `json:"type"`
		Data struct {
			Kind    string       `json:"kind"`
			Channel repo.Channel `json:"channel"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(got[0]))
	}
	if frame.Type != WSEventChannel || frame.Data.Kind != "create" || frame.Data.Channel.Name != "general" {
		t.Fatalf("frame: %+v", frame)
	}
}

// Phase 8 — failed create paths (invalid name, conflict) emit zero frames.
func TestCreateChannelDoesNotBroadcastOnFailure(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "BAD NAME"}, tok); rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("invalid name: got %d want 400", rr.Code)
	}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "dup"}, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("seed dup: %d", rr.Code)
	}
	rec2 := &recorder{}
	cf.hub.Subscribe("watcher", rec2)
	defer cf.hub.Unsubscribe("watcher", rec2)
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "dup"}, tok); rr.Code != stdhttp.StatusConflict {
		t.Fatalf("conflict: got %d want 409", rr.Code)
	}
	if got := rec2.snapshot(); len(got) != 0 {
		t.Fatalf("broadcasts on conflict: got %d want 0", len(got))
	}
}

// Phase 8 — PATCH /api/channels/{id} renames and returns the updated row.
func TestRenameChannelPersistsAndReturnsUpdated(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "old")

	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/"+chID,
		map[string]string{"name": "new"}, tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("patch: %d body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		OK   bool         `json:"ok"`
		Data repo.Channel `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || env.Data.ID != chID || env.Data.Name != "new" {
		t.Fatalf("envelope: %+v", env)
	}
}

// Phase 8 — rename emits a `channel:rename` frame on success.
func TestRenameChannelBroadcastsRenameEvent(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "old")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	if rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/"+chID,
		map[string]string{"name": "renamed"}, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("patch: %d body=%s", rr.Code, rr.Body.String())
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("broadcast count: got %d want 1", len(got))
	}
	var frame struct {
		Type string `json:"type"`
		Data struct {
			Kind    string       `json:"kind"`
			Channel repo.Channel `json:"channel"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(got[0]))
	}
	if frame.Type != WSEventChannel || frame.Data.Kind != "rename" || frame.Data.Channel.Name != "renamed" || frame.Data.Channel.ID != chID {
		t.Fatalf("frame: %+v", frame)
	}
}

// Phase 8 — `general` is not renamable; 403 with the standard envelope.
// No broadcast on the failure path.
func TestRenameGeneralChannelRejectedWith403(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "general")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/"+chID,
		map[string]string{"name": "renamed"}, tok)
	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("status: got %d want 403; body=%s", rr.Code, rr.Body.String())
	}
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != CodeForbidden {
		t.Fatalf("envelope: %+v", env)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("broadcasts on 403: got %d want 0", len(got))
	}
}

// Phase 8 — rename on missing id returns 404; no broadcast.
func TestRenameUnknownChannelReturns404(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/01HZZ00000000000000000ZZZZ",
		map[string]string{"name": "anything"}, tok)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("status: got %d want 404; body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("broadcasts on 404: got %d want 0", len(got))
	}
}

// Phase 8 — rename to a name held by another channel returns 409; no
// broadcast.
func TestRenameChannelDuplicateNameReturns409(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	a := createChannelOK(t, cf, tok, "alpha")
	createChannelOK(t, cf, tok, "beta")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/"+a,
		map[string]string{"name": "beta"}, tok)
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("status: got %d want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("broadcasts on 409: got %d want 0", len(got))
	}
}

// Phase 8 — POST + PATCH share one per-user bucket; the (Burst+1)th
// channel-write op from one user returns 429 with the standard envelope.
// Mirrors the SEC-5 pattern from auth_handlers_test.go.
func TestChannelWriteRateLimitTrips429AfterBurst(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 2, Refill: time.Hour})
	writeLimit := UserRateLimit(limiter, time.Minute, nil, false)
	cf := newChannelsFixtureWithLimit(t, writeLimit)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	// 2 successful POSTs use the bucket.
	for i, name := range []string{"first", "second"} {
		rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
			map[string]string{"name": name}, tok)
		if rr.Code != stdhttp.StatusCreated {
			t.Fatalf("POST %d: got %d want 201; body=%s", i, rr.Code, rr.Body.String())
		}
	}
	// 3rd write (a PATCH) trips the shared bucket.
	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/01HZZ00000000000000000ZZZZ",
		map[string]string{"name": "third"}, tok)
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("3rd write (PATCH): got %d want 429; body=%s", rr.Code, rr.Body.String())
	}
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != CodeRateLimited {
		t.Fatalf("envelope: %+v", env)
	}
}

// Phase 8 (#898) — the Retry-After header on the per-user channel-write
// 429 response must reflect the retryAfter argument the caller passes
// to UserRateLimit (not a hardcoded constant). registerChannels feeds
// this from CHAT_CHANNEL_WRITE_REFILL, so a header that disagrees with
// the configured refill mis-leads well-behaved clients backing off.
//
// We exercise two non-default values to pin the contract:
//   - 30s rounds to exactly 30 seconds.
//   - 90s rounds to exactly 90 seconds.
//
// Both prove the header tracks the argument; neither matches the old
// hardcoded 60s, so the regression would fail on either case.
func TestChannelWriteRetryAfterMatchesRefill(t *testing.T) {
	cases := []struct {
		name       string
		retryAfter time.Duration
		wantSecs   int
	}{
		{"30s", 30 * time.Second, 30},
		{"90s", 90 * time.Second, 90},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
			writeLimit := UserRateLimit(limiter, tc.retryAfter, nil, false)
			cf := newChannelsFixtureWithLimit(t, writeLimit)
			defer cf.close()
			tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

			if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
				map[string]string{"name": "first"}, tok); rr.Code != stdhttp.StatusCreated {
				t.Fatalf("1st POST: got %d want 201; body=%s", rr.Code, rr.Body.String())
			}
			rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
				map[string]string{"name": "second"}, tok)
			if rr.Code != stdhttp.StatusTooManyRequests {
				t.Fatalf("2nd POST: got %d want 429; body=%s", rr.Code, rr.Body.String())
			}
			got := rr.Header().Get("Retry-After")
			if got == "" {
				t.Fatalf("Retry-After header missing on 429")
			}
			secs, err := strconv.Atoi(got)
			if err != nil {
				t.Fatalf("Retry-After not an integer: %q (%v)", got, err)
			}
			if secs != tc.wantSecs {
				t.Fatalf("Retry-After: got %d want %d (retryAfter=%s)",
					secs, tc.wantSecs, tc.retryAfter)
			}
		})
	}
}

// Phase 8 — GET /api/channels is NOT rate-limited by the channel-write
// bucket. After exhausting the bucket on POSTs, GET still returns 200.
func TestChannelWriteRateLimitDoesNotAffectList(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	writeLimit := UserRateLimit(limiter, time.Minute, nil, false)
	cf := newChannelsFixtureWithLimit(t, writeLimit)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "first"}, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("POST: %d", rr.Code)
	}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "second"}, tok); rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("2nd POST should 429: %d", rr.Code)
	}
	if rr := cf.do(t, stdhttp.MethodGet, "/api/channels", nil, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("GET /api/channels under exhausted write bucket: got %d want 200", rr.Code)
	}
}

// Phase 8 — two distinct users sharing one IP each have independent
// per-user buckets; one user's 429 does not block the other.
func TestChannelWriteRateLimitIsolatesUsers(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	writeLimit := UserRateLimit(limiter, time.Minute, nil, false)
	cf := newChannelsFixtureWithLimit(t, writeLimit)
	defer cf.close()
	alice := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	bob := registerOK(t, cf.fixture, "bob", "correct-horse-battery")

	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "alpha"}, alice); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("alice POST: %d", rr.Code)
	}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "bravo"}, alice); rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("alice 2nd POST should 429: %d", rr.Code)
	}
	// Bob's bucket is untouched.
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "charlie"}, bob); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("bob POST under alice's exhausted bucket: %d body=%s", rr.Code, rr.Body.String())
	}
}

// Phase 8 (#883) — per-user channel-write 429s land in auth_events with
// the rejected user_id set, mirroring IPRateLimit's audit story. Without
// this, per-user rate-limit rejections are silent in the audit log.
func TestChannelWriteRateLimitLogsAuthEvent(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	cf := newChannelsFixture(t)
	defer cf.close()
	sink := NewRateLimitAuditSink(cf.db)
	writeLimit := UserRateLimit(limiter, time.Minute, sink, false)
	// Re-wire with the audited limiter. The default fixture has no
	// limiter; we rebuild Routes against the same handlers + mux state.
	mux := stdhttp.NewServeMux()
	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            cf.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	cf.channels.Routes(mux, require, writeLimit, cf.messages)
	cf.mux = mux

	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	// Burst=1: first POST succeeds, second trips 429 + audit row.
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "first"}, tok); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("POST: got %d want 201; body=%s", rr.Code, rr.Body.String())
	}
	if rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": "second"}, tok); rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("2nd POST: got %d want 429", rr.Code)
	}

	// Find alice's user id so we can assert the audit row carries it
	// (per-user 429s share AuthEventRateLimited with per-IP 429s; the
	// user_id column distinguishes them).
	var aliceID string
	if err := cf.fixture.db.QueryRow(
		`SELECT id FROM users WHERE username = ?`, "alice").Scan(&aliceID); err != nil {
		t.Fatalf("lookup alice id: %v", err)
	}
	var n int
	if err := cf.fixture.db.QueryRow(
		`SELECT COUNT(*) FROM auth_events WHERE kind = ? AND user_id = ?`,
		AuthEventRateLimited, aliceID).Scan(&n); err != nil {
		t.Fatalf("query auth_events: %v", err)
	}
	if n < 1 {
		t.Fatalf("auth_events rate_limited rows for user_id=%q: got %d want >=1", aliceID, n)
	}
}

// Phase 8 — rename with an invalid body returns 400; no broadcast.
func TestRenameChannelInvalidNameReturns400(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "old")

	rec := &recorder{}
	cf.hub.Subscribe("watcher", rec)
	defer cf.hub.Unsubscribe("watcher", rec)

	rr := cf.do(t, stdhttp.MethodPatch, "/api/channels/"+chID,
		map[string]string{"name": "BAD NAME"}, tok)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("broadcasts on 400: got %d want 0", len(got))
	}
}
