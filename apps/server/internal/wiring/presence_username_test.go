package wiring

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/wsapi"
	"hackathon/migrations"
)

// presenceTestInvite is a fixture-only invite code; obviously fake
// per CLAUDE.md "No hardcoded secrets" — it is the value we configure
// the wired Build with, not a value cribbed from production.
const presenceTestInvite = "INVITE-PRESENCE-TEST"

// presenceTestSecret is the JWT signing key for this test only;
// long-form fake placeholder per CLAUDE.md.
var presenceTestSecret = []byte("test-signing-key-must-be-long-enough-aaaaaa")

// TestRegisterPresenceUsernameEndToEndCarriesUsername builds the full
// production handler via wiring.Build, registers a user through the
// wired auth API, dials the wired /ws endpoint with a real ws-ticket,
// and asserts the self-join presence frame carries the registered
// user's username — which only happens if registerPresenceUsername
// installed a working resolver against the SQLite users table.
func TestRegisterPresenceUsernameEndToEndCarriesUsername(t *testing.T) {
	wsapi.SetPresenceUsernameLookup(nil)
	t.Cleanup(func() { wsapi.SetPresenceUsernameLookup(nil) })

	deps := newTestDeps(t)
	srv := httptest.NewServer(Build(deps))
	t.Cleanup(srv.Close)

	username := "alice-presence"
	password := "correct-horse-battery-staple"
	token := registerThroughAPI(t, srv.URL, username, password, presenceTestInvite)
	ticket := wsTicketThroughAPI(t, srv.URL, token)

	// The seed plants a "general" channel before Build returns; look it
	// up so the WS dial below carries a real ULID. The handler now
	// requires ?channel= when ChannelLookup is wired.
	channelID := lookupGeneralChannelID(t, deps)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + ticket + "&channel=" + channelID
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.CloseNow()

	readCtx, cancelRead := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRead()
	_, raw, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("ws read self-join: %v", err)
	}

	var ev presenceFrame
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, string(raw))
	}
	if ev.Type != "presence" || ev.Data.Kind != "join" {
		t.Fatalf("frame: got %+v, want type=presence kind=join", ev)
	}
	if ev.Data.Username != username {
		t.Fatalf("frame username: got %q, want %q (raw=%s)", ev.Data.Username, username, string(raw))
	}
}

// lookupGeneralChannelID reads the seeded "general" channel id from
// the deps' Repo. The seed runs as part of Build above, so the row is
// present by the time this helper executes.
func lookupGeneralChannelID(t *testing.T, deps Deps) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	channels, err := deps.Repo.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	for _, c := range channels {
		if c.Name == "general" {
			return c.ID
		}
	}
	t.Fatalf("seeded 'general' channel not found in %v", channels)
	return ""
}

// presenceFrame mirrors the wsapi presence envelope for decoding
// without importing unexported types.
type presenceFrame struct {
	Type string `json:"type"`
	Data struct {
		Kind     string `json:"kind"`
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	} `json:"data"`
}

// newTestDeps builds a wiring.Deps backed by a fresh SQLite tempfile
// with migrations applied. The Hub is in-process; Repo + JWTSecret +
// InviteCode are populated so the wired auth surface accepts a
// register → login → ws-ticket flow.
func newTestDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := appdb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("appdb.Open: %v", err)
	}
	if err := appdb.ApplyFS(context.Background(), sqlDB, migrations.FS); err != nil {
		t.Fatalf("appdb.ApplyFS: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	r, err := repo.New(sqlDB)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	return Deps{
		Hub:        hub.New(),
		Repo:       r,
		JWTSecret:  presenceTestSecret,
		InviteCode: presenceTestInvite,
	}
}

// registerThroughAPI POSTs /api/auth/register against the wired
// handler and returns the JWT from the response envelope.
func registerThroughAPI(t *testing.T, baseURL, username, password, invite string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": invite,
	})
	resp, err := stdhttp.Post(baseURL+"/api/auth/register",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("register status: got %d want 201, body=%s", resp.StatusCode, string(raw))
	}
	return decodeToken(t, resp.Body)
}

// wsTicketThroughAPI POSTs /api/auth/ws-ticket with a Bearer JWT and
// returns the redeemable ticket string.
func wsTicketThroughAPI(t *testing.T, baseURL, token string) string {
	t.Helper()
	req, err := stdhttp.NewRequest(stdhttp.MethodPost, baseURL+"/api/auth/ws-ticket", nil)
	if err != nil {
		t.Fatalf("ws-ticket request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ws-ticket POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("ws-ticket status: got %d want 200, body=%s", resp.StatusCode, string(raw))
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("ws-ticket decode: %v", err)
	}
	if !env.OK || env.Data.Ticket == "" {
		t.Fatalf("ws-ticket envelope: %+v", env)
	}
	return env.Data.Ticket
}

func decodeToken(t *testing.T, body io.Reader) string {
	t.Helper()
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		t.Fatalf("decode token envelope: %v", err)
	}
	if !env.OK || env.Data.Token == "" {
		t.Fatalf("token envelope: %+v", env)
	}
	return env.Data.Token
}
