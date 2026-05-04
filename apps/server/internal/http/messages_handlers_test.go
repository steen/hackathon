package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"sync"
	"testing"

	"hackathon/apps/server/internal/repo"
)

// recorder is a minimal hub.Subscriber used to verify broadcast wiring
// inside a single-process test.
type recorder struct {
	mu  sync.Mutex
	got [][]byte
}

func (r *recorder) Send(msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	r.got = append(r.got, cp)
}

func (r *recorder) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.got))
	copy(out, r.got)
	return out
}

func createChannelOK(t *testing.T, cf *channelsFixture, tok, name string) string {
	t.Helper()
	rr := cf.do(t, stdhttp.MethodPost, "/api/channels",
		map[string]string{"name": name}, tok)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create channel: %d body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data repo.Channel `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data.ID
}

// US-5 — POST /messages persists and broadcasts to hub subscribers of
// that channel.
func TestPostMessagePersistsAndBroadcasts(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "general")

	rec := &recorder{}
	cf.hub.Subscribe(chID, rec)
	defer cf.hub.Unsubscribe(chID, rec)

	rr := cf.do(t, stdhttp.MethodPost, "/api/channels/"+chID+"/messages",
		map[string]string{"body": "hello world"}, tok)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("post: %d body=%s", rr.Code, rr.Body.String())
	}

	var env struct {
		OK   bool         `json:"ok"`
		Data repo.Message `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Body != "hello world" || env.Data.ChannelID != chID {
		t.Fatalf("data: %+v", env.Data)
	}

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("broadcast count: got %d want 1", len(got))
	}
	var frame struct {
		Type string       `json:"type"`
		Data repo.Message `json:"data"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("unmarshal frame: %v body=%s", err, string(got[0]))
	}
	if frame.Type != WSEventMessage || frame.Data.Body != "hello world" {
		t.Fatalf("frame: %+v", frame)
	}
	if frame.Data.ID != env.Data.ID {
		t.Fatalf("frame id %q != response id %q", frame.Data.ID, env.Data.ID)
	}
}

// US-5 — sending to a non-existent channel returns 404, no broadcast.
func TestPostMessageRejectsUnknownChannel(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rr := cf.do(t, stdhttp.MethodPost,
		"/api/channels/01HZZ00000000000000000ZZZZ/messages",
		map[string]string{"body": "ghost"}, tok)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("status: got %d want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// US-5 — empty/oversized bodies rejected. Oversize must surface the
// dedicated `message_too_large` code so clients can distinguish a body-
// cap reject from a generic bad_request (issue #419, see #137/#147).
func TestPostMessageRejectsBadBodies(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "general")

	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"empty", "", CodeBadRequest},
		{"whitespace", "   ", CodeBadRequest},
		{"oversized", strings.Repeat("x", MaxMessageBodyBytes+1), "message_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := cf.do(t, stdhttp.MethodPost, "/api/channels/"+chID+"/messages",
				map[string]string{"body": c.body}, tok)
			if rr.Code != stdhttp.StatusBadRequest {
				t.Fatalf("got %d want 400", rr.Code)
			}
			var env Envelope
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v body=%s", err, rr.Body.String())
			}
			if env.OK || env.Error == nil {
				t.Fatalf("envelope: %+v", env)
			}
			if env.Error.Code != c.wantCode {
				t.Fatalf("code: got %q want %q", env.Error.Code, c.wantCode)
			}
		})
	}
}

// US-6 — history is newest-first and respects `before` + `limit`.
func TestGetMessagesReturnsPriorMessagesPaginated(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "general")

	var posted []string
	for i := 0; i < 5; i++ {
		rr := cf.do(t, stdhttp.MethodPost, "/api/channels/"+chID+"/messages",
			map[string]string{"body": "m"}, tok)
		if rr.Code != stdhttp.StatusCreated {
			t.Fatalf("post[%d]: %d", i, rr.Code)
		}
		var env struct {
			Data repo.Message `json:"data"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&env)
		posted = append(posted, env.Data.ID)
	}

	rr := cf.do(t, stdhttp.MethodGet,
		"/api/channels/"+chID+"/messages?limit=3", nil, tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("list: %d", rr.Code)
	}
	var env struct {
		Data struct {
			Messages []repo.Message `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Messages) != 3 {
		t.Fatalf("page1 len: %d", len(env.Data.Messages))
	}
	if env.Data.Messages[0].ID != posted[4] {
		t.Fatalf("newest-first: got %s want %s", env.Data.Messages[0].ID, posted[4])
	}

	cursor := env.Data.Messages[2].ID
	rr2 := cf.do(t, stdhttp.MethodGet,
		"/api/channels/"+chID+"/messages?limit=10&before="+cursor, nil, tok)
	if rr2.Code != stdhttp.StatusOK {
		t.Fatalf("list2: %d", rr2.Code)
	}
	var env2 struct {
		Data struct {
			Messages []repo.Message `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&env2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(env2.Data.Messages) != 2 {
		t.Fatalf("page2 len: %d", len(env2.Data.Messages))
	}
	if env2.Data.Messages[0].ID != posted[1] || env2.Data.Messages[1].ID != posted[0] {
		t.Fatalf("page2 ids: got %v,%v want %v,%v",
			env2.Data.Messages[0].ID, env2.Data.Messages[1].ID, posted[1], posted[0])
	}
}

// US-6 — history of an unknown channel returns 404.
func TestGetMessagesUnknownChannelReturns404(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")

	rr := cf.do(t, stdhttp.MethodGet,
		"/api/channels/01HZZ00000000000000000ZZZZ/messages", nil, tok)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("got %d want 404", rr.Code)
	}
}
