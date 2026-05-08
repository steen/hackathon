package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hackathon/apps/cli/internal/config"
)

// channelsRig stands up an httptest server that speaks just the channel
// surface the create/rename CLI commands touch (GET/POST /api/channels,
// PATCH /api/channels/{id}). It avoids extending the shared fakeServer
// in testserver_test.go (which the current footprint does not own).
type channelsRig struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	chans    []chRow
	nextID   int
	rename   func(id, name string) (status int, body any)
	create   func(name string) (status int, body any)
	postSeen atomic.Int32
}

type chRow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

const channelsTestToken = "test-token-aaaaaaaaaaaaaaaa" //nolint:gosec // obvious fake placeholder per CLAUDE.md
const channelsTestUserID = "u-test"

func newChannelsRig(t *testing.T) *channelsRig {
	t.Helper()
	r := &channelsRig{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/channels", r.handleChannels)
	mux.HandleFunc("/api/channels/", r.handleChannelByID)
	r.server = httptest.NewServer(mux)
	t.Cleanup(r.server.Close)
	r.addChannel("general")
	return r
}

func (r *channelsRig) URL() string { return r.server.URL }

func (r *channelsRig) addChannel(name string) chRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	c := chRow{
		ID:        fmt.Sprintf("01ABCDEFGHJKMNPQRSTVWXYZ%02d", r.nextID),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	r.chans = append(r.chans, c)
	return c
}

func (r *channelsRig) writeOK(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func (r *channelsRig) writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": msg},
	})
}

func (r *channelsRig) authed(req *http.Request) bool {
	return req.Header.Get("Authorization") == "Bearer "+channelsTestToken
}

func (r *channelsRig) handleChannels(w http.ResponseWriter, req *http.Request) {
	if !r.authed(req) {
		r.writeErr(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	switch req.Method {
	case http.MethodGet:
		r.mu.Lock()
		out := append([]chRow(nil), r.chans...)
		r.mu.Unlock()
		r.writeOK(w, http.StatusOK, map[string]any{"channels": out})
	case http.MethodPost:
		r.postSeen.Add(1)
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			r.writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if r.create != nil {
			status, payload := r.create(body.Name)
			if status >= 400 {
				if m, ok := payload.(map[string]string); ok {
					r.writeErr(w, status, m["code"], m["message"])
					return
				}
			}
			r.writeOK(w, status, payload)
			return
		}
		c := r.addChannel(body.Name)
		r.writeOK(w, http.StatusCreated, c)
	default:
		r.writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (r *channelsRig) handleChannelByID(w http.ResponseWriter, req *http.Request) {
	if !r.authed(req) {
		r.writeErr(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, "/api/channels/")
	id := strings.SplitN(rest, "/", 2)[0]
	if req.Method != http.MethodPatch {
		r.writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if r.rename != nil {
		status, payload := r.rename(id, body.Name)
		if status >= 400 {
			if m, ok := payload.(map[string]string); ok {
				r.writeErr(w, status, m["code"], m["message"])
				return
			}
		}
		r.writeOK(w, status, payload)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.chans {
		if r.chans[i].ID == id {
			r.chans[i].Name = body.Name
			r.writeOK(w, http.StatusOK, r.chans[i])
			return
		}
	}
	r.writeErr(w, http.StatusNotFound, "not_found", "channel not found")
}

func newChannelsEnv(t *testing.T, rig *channelsRig) (*Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	if err := config.Save(dir, &config.File{
		Server: rig.URL(),
		Token:  channelsTestToken,
		User:   &config.User{ID: channelsTestUserID, Username: "tester"},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &Env{
		Stdin:     strings.NewReader(""),
		Stdout:    stdout,
		Stderr:    stderr,
		ConfigDir: dir,
		Server:    rig.URL(),
	}, stdout, stderr
}

func mustChannelsCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func TestChannelsCreatePrintsIDAndName(t *testing.T) {
	rig := newChannelsRig(t)
	env, stdout, stderr := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	if err := Channels(ctx, env, []string{"create", "books"}); err != nil {
		t.Fatalf("Channels create: %v", err)
	}
	out := stdout.String()
	parts := strings.SplitN(strings.TrimRight(out, "\n"), "\t", 2)
	if len(parts) != 2 || parts[1] != "books" {
		t.Errorf("stdout = %q, want <id>\\tbooks", out)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want empty on success", got)
	}
}

func TestChannelsCreateRejectsInvalidNameLocally(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"create", "Bad Name"})
	if err == nil {
		t.Fatal("expected error on invalid name; got nil")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("err = %q, want local validation message", err)
	}
	if rig.postSeen.Load() != 0 {
		t.Errorf("server saw %d POSTs, want 0 (local validation should short-circuit)", rig.postSeen.Load())
	}
}

func TestChannelsCreateUsageError(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"create"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Errorf("err = %v, want usage error", err)
	}
}

func TestChannelsCreateConflict(t *testing.T) {
	rig := newChannelsRig(t)
	rig.create = func(string) (int, any) {
		return http.StatusConflict, map[string]string{
			"code":    "conflict",
			"message": "channel name already taken",
		}
	}
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"create", "books"})
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "conflict") || !strings.Contains(err.Error(), "channel name already taken") {
		t.Errorf("err = %q, want conflict + server message", err)
	}
}

func TestChannelsRenameHappyPath(t *testing.T) {
	rig := newChannelsRig(t)
	original := rig.addChannel("books")
	env, stdout, stderr := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	if err := Channels(ctx, env, []string{"rename", "books", "reading"}); err != nil {
		t.Fatalf("Channels rename: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := original.ID + "\treading"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if s := stderr.String(); s != "" {
		t.Errorf("stderr = %q, want empty on success", s)
	}
}

func TestChannelsRenameUnknownChannel(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "does-not-exist", "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no channel named") || !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("err = %q, want \"no channel named ...does-not-exist...\"", err)
	}
}

func TestChannelsRenameForbiddenOnGeneral(t *testing.T) {
	rig := newChannelsRig(t)
	rig.rename = func(string, string) (int, any) {
		return http.StatusForbidden, map[string]string{
			"code":    "forbidden",
			"message": "the general channel cannot be renamed",
		}
	}
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "general", "lobby"})
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}
	if !strings.Contains(err.Error(), "forbidden") ||
		!strings.Contains(err.Error(), "general channel cannot be renamed") {
		t.Errorf("err = %q, want forbidden + server message", err)
	}
}

func TestChannelsRenameRateLimited(t *testing.T) {
	rig := newChannelsRig(t)
	rig.addChannel("books")
	rig.rename = func(string, string) (int, any) {
		return http.StatusTooManyRequests, map[string]string{
			"code":    "rate_limited",
			"message": "too many requests, please try again later",
		}
	}
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "books", "reading"})
	if err == nil {
		t.Fatal("expected rate-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "rate_limited") ||
		!strings.Contains(err.Error(), "too many requests") {
		t.Errorf("err = %q, want rate-limit message", err)
	}
}

func TestChannelsRenameInvalidNewNameLocally(t *testing.T) {
	rig := newChannelsRig(t)
	rig.addChannel("books")
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "books", "Bad Name"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("err = %q, want local validation message", err)
	}
}

func TestChannelsRenameUsageError(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "only-one"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Errorf("err = %v, want usage error", err)
	}
}

func TestChannelsCreateNotLoggedIn(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)
	// Drop the cached token so newClient(requireToken=true) returns
	// ErrNotLoggedIn before any HTTP call.
	if err := config.Save(env.ConfigDir, &config.File{Server: rig.URL()}); err != nil {
		t.Fatalf("clear token: %v", err)
	}

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"create", "books"})
	if err == nil {
		t.Fatal("expected ErrNotLoggedIn, got nil")
	}
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want errors.Is ErrNotLoggedIn", err)
	}
	if !strings.HasPrefix(err.Error(), "channels create: ") {
		t.Errorf("err = %q, want prefix %q", err.Error(), "channels create: ")
	}
}

func TestChannelsRenameNotLoggedIn(t *testing.T) {
	rig := newChannelsRig(t)
	env, _, _ := newChannelsEnv(t, rig)
	if err := config.Save(env.ConfigDir, &config.File{Server: rig.URL()}); err != nil {
		t.Fatalf("clear token: %v", err)
	}

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	err := Channels(ctx, env, []string{"rename", "books", "reading"})
	if err == nil {
		t.Fatal("expected ErrNotLoggedIn, got nil")
	}
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want errors.Is ErrNotLoggedIn", err)
	}
	if !strings.HasPrefix(err.Error(), "channels rename: ") {
		t.Errorf("err = %q, want prefix %q", err.Error(), "channels rename: ")
	}
}

func TestChannelsListStillWorksWithoutSubcommand(t *testing.T) {
	rig := newChannelsRig(t)
	rig.addChannel("books")
	env, stdout, _ := newChannelsEnv(t, rig)

	ctx, cancel := mustChannelsCtx()
	defer cancel()
	if err := Channels(ctx, env, nil); err != nil {
		t.Fatalf("Channels list: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "\tgeneral\n") || !strings.Contains(out, "\tbooks\n") {
		t.Errorf("stdout = %q, want both seeded channel names", out)
	}
}
