package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
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
	ch.Routes(mux, require, ms)
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
