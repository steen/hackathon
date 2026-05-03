package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeServer is a minimal stand-in for apps/server. It speaks the
// envelope contract but only the slice of routes the CLI tests need.
// Tests that need a 401 path swap the token store to empty.
type fakeServer struct {
	t  *testing.T
	mu sync.Mutex

	server *httptest.Server

	// inviteCode mirrors AuthDeps.InviteCode. Tests set this to a
	// fake placeholder; never a value that could be mistaken for real.
	inviteCode string

	// users keyed by username -> {id, password, token}
	users map[string]*fakeUser
	// tokens keyed by token -> userID
	tokens map[string]string
	// channels in insert order
	channels []fakeChannel
	// messages keyed by channel id
	messages map[string][]fakeMsg

	// broadcast topic
	subsMu sync.Mutex
	subs   map[string][]chan fakeMsg

	// nextID is a monotonic id generator
	nextID int
}

type fakeUser struct {
	ID       string
	Username string
	Password string
	Token    string
}

type fakeChannel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type fakeMsg struct {
	ID           string `json:"id"`
	ChannelID    string `json:"channel_id"`
	SenderUserID string `json:"sender_user_id"`
	Body         string `json:"body"`
	CreatedAt    string `json:"created_at"`
}

type envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{
		t:          t,
		inviteCode: "test-invite-code",
		users:      map[string]*fakeUser{},
		tokens:     map[string]string{},
		messages:   map[string][]fakeMsg{},
		subs:       map[string][]chan fakeMsg{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/register", f.handleRegister)
	mux.HandleFunc("/api/auth/login", f.handleLogin)
	mux.HandleFunc("/api/auth/me", f.handleMe)
	mux.HandleFunc("/api/auth/logout", f.handleLogout)
	mux.HandleFunc("/api/auth/ws-ticket", f.handleWSTicket)
	mux.HandleFunc("/api/channels", f.handleChannels)
	mux.HandleFunc("/api/channels/", f.handleChannelMessages)
	mux.HandleFunc("/ws", f.handleWS)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	// seed a default channel so most tests do not have to create one
	f.addChannel("general")
	return f
}

func (f *fakeServer) URL() string { return f.server.URL }

func (f *fakeServer) writeOK(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{OK: true, Data: data})
}

func (f *fakeServer) writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := envelope{}
	body.Error = &struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: msg}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeServer) authedUserID(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(auth, p) {
		return "", false
	}
	tok := strings.TrimPrefix(auth, p)
	f.mu.Lock()
	defer f.mu.Unlock()
	uid, ok := f.tokens[tok]
	return uid, ok
}

func (f *fakeServer) newToken() string {
	f.nextID++
	return fmt.Sprintf("tok-%d-%d", time.Now().UnixNano(), f.nextID)
}

func (f *fakeServer) addChannel(name string) fakeChannel {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	c := fakeChannel{
		ID:        fmt.Sprintf("ch-%d", f.nextID),
		Name:      name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	f.channels = append(f.channels, c)
	return c
}

func (f *fakeServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.writeErr(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if req.InviteCode != f.inviteCode {
		f.writeErr(w, 403, "forbidden", "invalid invite code")
		return
	}
	f.mu.Lock()
	if _, exists := f.users[req.Username]; exists {
		f.mu.Unlock()
		f.writeErr(w, 409, "conflict", "username already taken")
		return
	}
	f.nextID++
	id := fmt.Sprintf("u-%d", f.nextID)
	tok := f.newToken()
	f.users[req.Username] = &fakeUser{ID: id, Username: req.Username, Password: req.Password, Token: tok}
	f.tokens[tok] = id
	f.mu.Unlock()
	f.writeOK(w, 201, map[string]interface{}{
		"token": tok,
		"user":  map[string]string{"id": id, "username": req.Username},
	})
}

func (f *fakeServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.writeErr(w, 400, "bad_request", "invalid JSON body")
		return
	}
	f.mu.Lock()
	u, ok := f.users[req.Username]
	if !ok || u.Password != req.Password {
		f.mu.Unlock()
		f.writeErr(w, 401, "unauthorized", "invalid credentials")
		return
	}
	tok := f.newToken()
	u.Token = tok
	f.tokens[tok] = u.ID
	f.mu.Unlock()
	f.writeOK(w, 200, map[string]interface{}{
		"token": tok,
		"user":  map[string]string{"id": u.ID, "username": u.Username},
	})
}

func (f *fakeServer) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	uid, ok := f.authedUserID(r)
	if !ok {
		f.writeErr(w, 401, "unauthorized", "unauthorized")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == uid {
			f.writeOK(w, 200, map[string]interface{}{
				"user": map[string]string{"id": u.ID, "username": u.Username},
			})
			return
		}
	}
	f.writeErr(w, 401, "unauthorized", "unauthorized")
}

func (f *fakeServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	uid, ok := f.authedUserID(r)
	if !ok {
		f.writeErr(w, 401, "unauthorized", "unauthorized")
		return
	}
	f.mu.Lock()
	for tok, u := range f.tokens {
		if u == uid {
			delete(f.tokens, tok)
		}
	}
	f.mu.Unlock()
	f.writeOK(w, 200, map[string]interface{}{"ok": true})
}

func (f *fakeServer) handleWSTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	if _, ok := f.authedUserID(r); !ok {
		f.writeErr(w, 401, "unauthorized", "unauthorized")
		return
	}
	tok := f.newToken()
	f.writeOK(w, 200, map[string]interface{}{
		"ticket":     tok,
		"expires_at": time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339Nano),
	})
}

func (f *fakeServer) handleChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.authedUserID(r); !ok {
		f.writeErr(w, 401, "unauthorized", "unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.mu.Lock()
		copyOut := append([]fakeChannel(nil), f.channels...)
		f.mu.Unlock()
		f.writeOK(w, 200, map[string]interface{}{"channels": copyOut})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.writeErr(w, 400, "bad_request", "invalid JSON body")
			return
		}
		c := f.addChannel(req.Name)
		f.writeOK(w, 201, c)
	default:
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
	}
}

func (f *fakeServer) handleChannelMessages(w http.ResponseWriter, r *http.Request) {
	uid, ok := f.authedUserID(r)
	if !ok {
		f.writeErr(w, 401, "unauthorized", "unauthorized")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "messages" {
		f.writeErr(w, 404, "not_found", "not found")
		return
	}
	channelID := parts[0]
	switch r.Method {
	case http.MethodGet:
		f.mu.Lock()
		out := append([]fakeMsg(nil), f.messages[channelID]...)
		f.mu.Unlock()
		f.writeOK(w, 200, map[string]interface{}{"messages": out})
	case http.MethodPost:
		var req struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.writeErr(w, 400, "bad_request", "invalid JSON body")
			return
		}
		f.mu.Lock()
		f.nextID++
		m := fakeMsg{
			ID:           fmt.Sprintf("m-%d", f.nextID),
			ChannelID:    channelID,
			SenderUserID: uid,
			Body:         req.Body,
			CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		}
		f.messages[channelID] = append(f.messages[channelID], m)
		f.mu.Unlock()
		f.broadcast(channelID, m)
		f.writeOK(w, 201, m)
	default:
		f.writeErr(w, 405, "method_not_allowed", "method not allowed")
	}
}

func (f *fakeServer) handleWS(w http.ResponseWriter, r *http.Request) {
	// We don't enforce ticket validity in the fake — the client mints
	// one via handleWSTicket and forwards it on the upgrade. Channel
	// scoping is what tests assert against.
	channel := r.URL.Query().Get("channel")
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := r.Context()

	ch := make(chan fakeMsg, 8)
	f.subsMu.Lock()
	f.subs[channel] = append(f.subs[channel], ch)
	f.subsMu.Unlock()
	defer func() {
		f.subsMu.Lock()
		defer f.subsMu.Unlock()
		filtered := f.subs[channel][:0]
		for _, s := range f.subs[channel] {
			if s != ch {
				filtered = append(filtered, s)
			}
		}
		f.subs[channel] = filtered
	}()

	// drain reads in a goroutine so we notice client-initiated close
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case m := <-ch:
			payload := map[string]interface{}{"type": "message", "data": m}
			data, _ := json.Marshal(payload)
			if err := c.Write(ctx, websocket.MessageText, data); err != nil {
				return
			}
		case <-readDone:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (f *fakeServer) broadcast(channelID string, m fakeMsg) {
	f.subsMu.Lock()
	subs := append([]chan fakeMsg(nil), f.subs[channelID]...)
	f.subsMu.Unlock()
	for _, s := range subs {
		select {
		case s <- m:
		default:
		}
	}
}

// safeBuf is a tiny mutex-protected wrapper so tests that read stdout
// while another goroutine (Watch) writes don't trip the race detector.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuf) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

// runRig wires an Env to in-memory streams + a temp config dir.
type runRig struct {
	env    *Env
	stdin  *bytes.Buffer
	stdout *safeBuf
	stderr *safeBuf
	dir    string
}

func newRig(t *testing.T, fs *fakeServer) *runRig {
	t.Helper()
	r := &runRig{
		stdin:  &bytes.Buffer{},
		stdout: &safeBuf{},
		stderr: &safeBuf{},
		dir:    t.TempDir(),
	}
	r.env = &Env{
		Stdin:     r.stdin,
		Stdout:    r.stdout,
		Stderr:    r.stderr,
		ConfigDir: r.dir,
		Server:    fs.URL(),
	}
	return r
}

// drainHTTPBody is here so unused-import errors don't surface if
// io.Discard usage shifts; intentionally a noop helper kept for tests
// that may want to read response bodies.
var _ = io.Discard

func mustCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
