package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
)

// presenceFixture wires the auth fixture (for register/login + DB) plus
// a fresh hub and the presence handler behind RequireJWT, all on one
// mux. The handler is the unit under test; the hub is filled via its
// public API to mimic what the WS handler would do at runtime.
type presenceFixture struct {
	*fixture
	hub *hub.Hub
	mux *stdhttp.ServeMux
}

func newPresenceFixture(t *testing.T) *presenceFixture {
	t.Helper()
	f := newFixture(t)
	h := hub.New()
	ph := NewPresenceHandlers(PresenceDeps{Hub: h, DB: f.db})

	mux := stdhttp.NewServeMux()
	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            f.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	mux.Handle("GET /api/presence", require(stdhttp.HandlerFunc(ph.List)))
	return &presenceFixture{fixture: f, hub: h, mux: mux}
}

func (pf *presenceFixture) get(t *testing.T, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	pf.mux.ServeHTTP(rr, req)
	return rr
}

func TestPresenceRequiresAuth(t *testing.T) {
	pf := newPresenceFixture(t)
	defer pf.close()

	rr := pf.get(t, "/api/presence", "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestPresenceReturnsEmptyWhenHubEmpty(t *testing.T) {
	pf := newPresenceFixture(t)
	defer pf.close()
	tok := registerOK(t, pf.fixture, "alice", "correct-horse-battery")

	rr := pf.get(t, "/api/presence", tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	users := decodePresenceUsers(t, rr.Body.Bytes())
	if len(users) != 0 {
		t.Fatalf("got %+v, want empty list", users)
	}
}

func TestPresenceListsHubUsersWithUsernames(t *testing.T) {
	pf := newPresenceFixture(t)
	defer pf.close()

	// Register two users so they exist in the DB; capture their IDs by
	// peeking at the auth fixture's response token to derive ID is
	// indirect, so go through the DB directly here.
	_ = registerOK(t, pf.fixture, "alice", "correct-horse-battery")
	_ = registerOK(t, pf.fixture, "bob", "correct-horse-battery")
	requesterTok := registerOK(t, pf.fixture, "charlie", "correct-horse-battery")

	aliceID := lookupUserIDByUsername(t, pf.fixture, "alice")
	bobID := lookupUserIDByUsername(t, pf.fixture, "bob")

	pf.hub.AddPresence(aliceID)
	pf.hub.AddPresence(bobID)

	rr := pf.get(t, "/api/presence", requesterTok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	users := decodePresenceUsers(t, rr.Body.Bytes())
	if len(users) != 2 {
		t.Fatalf("got %+v, want 2 users", users)
	}
	// Sorted by username — alice before bob.
	if users[0].Username != "alice" || users[1].Username != "bob" {
		t.Fatalf("order: %+v, want alice then bob", users)
	}
	if users[0].ID != aliceID || users[1].ID != bobID {
		t.Fatalf("ids: %+v, want %s then %s", users, aliceID, bobID)
	}
}

func TestPresenceTolerateMissingUserRow(t *testing.T) {
	pf := newPresenceFixture(t)
	defer pf.close()
	requesterTok := registerOK(t, pf.fixture, "alice", "correct-horse-battery")

	// A user whose row was deleted between hub snapshot and DB read.
	// The handler should keep the entry with an empty username so the
	// caller can still render the id.
	pf.hub.AddPresence("01HZZZZZZZZZZZZZZZZZZZZZZZ")

	rr := pf.get(t, "/api/presence", requesterTok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	users := decodePresenceUsers(t, rr.Body.Bytes())
	if len(users) != 1 || users[0].ID != "01HZZZZZZZZZZZZZZZZZZZZZZZ" || users[0].Username != "" {
		t.Fatalf("got %+v, want a single ghost user", users)
	}
}

func decodePresenceUsers(t *testing.T, raw []byte) []PresenceUser {
	t.Helper()
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Users []PresenceUser `json:"users"`
		} `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&env); err != nil {
		t.Fatalf("decode: %v; body=%s", err, raw)
	}
	if !env.OK {
		t.Fatalf("envelope ok=false; body=%s", raw)
	}
	return env.Data.Users
}

func lookupUserIDByUsername(t *testing.T, f *fixture, username string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		t.Fatalf("lookup id for %s: %v", username, err)
	}
	return id
}
