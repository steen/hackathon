package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackathon/apps/server/internal/auth"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/migrations"
)

// fixture is the per-test wiring: an in-memory SQLite (file:?mode=memory
// would not survive multiple connections — we use a fresh tempfile via
// the migration runner under SetMaxOpenConns(1)) plus the handlers.
type fixture struct {
	db       *sql.DB
	handlers *AuthHandlers
	tickets  *auth.TicketStore
	cleanup  func()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWithInvite(t, "INVITE-OK")
}

// newFixtureWithInvite builds a fixture with a caller-supplied server
// invite code. Pass "" to exercise the registration-disabled branch.
func newFixtureWithInvite(t *testing.T, inviteCode string) *fixture {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := appdb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := appdb.ApplyFS(context.Background(), sqlDB, migrations.FS); err != nil {
		t.Fatalf("ApplyFS: %v", err)
	}
	tickets := auth.NewTicketStore()
	h := NewAuthHandlers(AuthDeps{
		DB:         sqlDB,
		Tickets:    tickets,
		SigningKey: []byte("test-signing-key-must-be-long-enough"),
		InviteCode: inviteCode,
		Now:        time.Now,
	})
	return &fixture{
		db:       sqlDB,
		handlers: h,
		tickets:  tickets,
		cleanup:  func() { _ = sqlDB.Close() },
	}
}

func (f *fixture) close() { f.cleanup() }

func (f *fixture) post(t *testing.T, path string, body interface{}, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(stdhttp.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	f.dispatch(path, rr, req)
	return rr
}

func (f *fixture) get(t *testing.T, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	f.dispatch(path, rr, req)
	return rr
}

// dispatch routes to the right handler, applying RequireJWT to the
// authenticated endpoints. Mirrors what main.go wires up; kept inline
// so the tests don't need a full *http.Server.
func (f *fixture) dispatch(path string, w stdhttp.ResponseWriter, r *stdhttp.Request) {
	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            f.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	switch path {
	case "/api/auth/register":
		f.handlers.Register(w, r)
	case "/api/auth/login":
		f.handlers.Login(w, r)
	case "/api/auth/me":
		require(stdhttp.HandlerFunc(f.handlers.Me)).ServeHTTP(w, r)
	case "/api/auth/logout":
		require(stdhttp.HandlerFunc(f.handlers.Logout)).ServeHTTP(w, r)
	case "/api/auth/ws-ticket":
		require(stdhttp.HandlerFunc(f.handlers.WSTicket)).ServeHTTP(w, r)
	default:
		stdhttp.NotFound(w, r)
	}
}

// envelope unmarshals the standard {ok, data, error} response body.
type envelope struct {
	OK    bool                   `json:"ok"`
	Data  map[string]interface{} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func mustEnvelope(t *testing.T, rr *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode envelope: %v (body=%q)", err, rr.Body.String())
	}
	return e
}

func mustToken(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	e := mustEnvelope(t, rr)
	if !e.OK {
		t.Fatalf("expected ok envelope, got: %+v", e.Error)
	}
	tok, _ := e.Data["token"].(string)
	if tok == "" {
		t.Fatalf("missing token in data: %+v", e.Data)
	}
	return tok
}

// US-1, US-11 — register requires the invite code and creates a user.
func TestRegisterCreatesUserWithInviteCode(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    "alice",
		"password":    "correct-horse-battery",
		"invite_code": "INVITE-OK",
	}, "")
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("status: got %d want 201; body=%s", rr.Code, rr.Body.String())
	}
	_ = mustToken(t, rr)

	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, "alice").Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count: got %d want 1", n)
	}
}

// US-11 — register without (or with wrong) invite code is rejected.
func TestRegisterRejectsMissingOrWrongInviteCode(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	cases := []struct {
		name string
		code string
	}{
		{"missing", ""},
		{"wrong", "WRONG"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := f.post(t, "/api/auth/register", map[string]string{
				"username":    "alice",
				"password":    "correct-horse-battery",
				"invite_code": tc.code,
			}, "")
			if rr.Code != stdhttp.StatusForbidden {
				t.Fatalf("status: got %d want 403", rr.Code)
			}
			e := mustEnvelope(t, rr)
			if e.OK || e.Error == nil || e.Error.Code != CodeForbidden {
				t.Fatalf("envelope: %+v", e)
			}
		})
	}
}

func TestRegisterRejectsBadUsername(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    "no spaces!",
		"password":    "correct-horse-battery",
		"invite_code": "INVITE-OK",
	}, "")
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    "alice",
		"password":    "short",
		"invite_code": "INVITE-OK",
	}, "")
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	body := map[string]string{
		"username":    "alice",
		"password":    "correct-horse-battery",
		"invite_code": "INVITE-OK",
	}
	if rr := f.post(t, "/api/auth/register", body, ""); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("first register: %d", rr.Code)
	}
	rr := f.post(t, "/api/auth/register", body, "")
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("second register: got %d want 409", rr.Code)
	}
}

// US-2 — login with valid creds returns a token.
func TestLoginReturnsTokenForValidCredentials(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	registerOK(t, f, "alice", "correct-horse-battery")

	rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice",
		"password": "correct-horse-battery",
	}, "")
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rr.Code, rr.Body.String())
	}
	_ = mustToken(t, rr)
}

// US-2, SEC-4 — wrong password returns the byte-identical generic error.
func TestLoginRejectsWrongPassword(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	registerOK(t, f, "alice", "correct-horse-battery")

	rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice",
		"password": "wrong-password-here",
	}, "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
	e := mustEnvelope(t, rr)
	if e.Error == nil || e.Error.Message != auth.LoginErrorMessage {
		t.Fatalf("error message: got %+v want %q", e.Error, auth.LoginErrorMessage)
	}
}

// SEC-4 — unknown-username and wrong-password return the same body.
func TestLoginUnknownUserAndWrongPasswordReturnIdenticalEnvelope(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	registerOK(t, f, "alice", "correct-horse-battery")

	a := f.post(t, "/api/auth/login", map[string]string{"username": "alice", "password": "wrong-password-here"}, "")
	b := f.post(t, "/api/auth/login", map[string]string{"username": "nobody", "password": "anything-at-all"}, "")
	if a.Code != b.Code {
		t.Fatalf("status differ: %d vs %d", a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Fatalf("bodies differ:\n  unknown: %s\n  wrong:   %s", b.Body.String(), a.Body.String())
	}
}

// US-2 — /api/auth/me returns the caller for a valid token.
func TestMeReturnsCurrentUserForValidToken(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")

	rr := f.get(t, "/api/auth/me", tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rr.Code, rr.Body.String())
	}
	e := mustEnvelope(t, rr)
	user, _ := e.Data["user"].(map[string]interface{})
	if user["username"] != "alice" {
		t.Fatalf("user.username: got %v want alice", user["username"])
	}
}

func TestMeRejectsMissingBearer(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	rr := f.get(t, "/api/auth/me", "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
}

// US-12 — /api/auth/me rejects a token after logout.
func TestMeRejectsTokenAfterLogout(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")

	if rr := f.post(t, "/api/auth/logout", nil, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("logout: %d", rr.Code)
	}
	rr := f.get(t, "/api/auth/me", tok)
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("post-logout /me status: got %d want 401", rr.Code)
	}
}

// US-12 — logout bumps token_version for the user.
func TestLogoutIncrementsTokenVersion(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")
	tvBefore := tokenVersion(t, f, "alice")

	if rr := f.post(t, "/api/auth/logout", nil, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("logout: %d", rr.Code)
	}
	tvAfter := tokenVersion(t, f, "alice")
	if tvAfter != tvBefore+1 {
		t.Fatalf("token_version: before=%d after=%d", tvBefore, tvAfter)
	}
}

func TestWSTicketRequiresAuth(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	rr := f.post(t, "/api/auth/ws-ticket", nil, "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
}

// SEC-12 — ws-ticket is single-use within its 30s TTL.
func TestWSTicketIsSingleUse(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")

	rr := f.post(t, "/api/auth/ws-ticket", nil, tok)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("issue ticket: %d body=%s", rr.Code, rr.Body.String())
	}
	e := mustEnvelope(t, rr)
	ticket, _ := e.Data["ticket"].(string)
	if ticket == "" {
		t.Fatalf("missing ticket")
	}

	uid, ok := f.tickets.Redeem(ticket)
	if !ok || uid == "" {
		t.Fatalf("first Redeem: ok=%v uid=%q", ok, uid)
	}
	if _, ok := f.tickets.Redeem(ticket); ok {
		t.Fatalf("second Redeem must fail")
	}
}

func TestWSTicketReturnsExpiresAtInRFC3339(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")
	rr := f.post(t, "/api/auth/ws-ticket", nil, tok)
	e := mustEnvelope(t, rr)
	exp, _ := e.Data["expires_at"].(string)
	if _, err := time.Parse(time.RFC3339Nano, exp); err != nil {
		t.Fatalf("expires_at not RFC3339: %q (%v)", exp, err)
	}
}

// SEC-13 — auth_events records register, login_success, logout for a
// scripted register → login → logout flow.
func TestAuthEventsRecordsRegisterLoginLogoutKinds(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	tok := registerOK(t, f, "alice", "correct-horse-battery")
	if rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "correct-horse-battery",
	}, ""); rr.Code != stdhttp.StatusOK {
		t.Fatalf("login: %d", rr.Code)
	}
	if rr := f.post(t, "/api/auth/logout", nil, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("logout: %d", rr.Code)
	}

	want := map[string]int{
		AuthEventRegister:     1,
		AuthEventLoginSuccess: 1,
		AuthEventLogout:       1,
	}
	rows, err := f.db.Query(`SELECT kind, COUNT(*) FROM auth_events GROUP BY kind`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[kind] = n
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("auth_events kind %q: got %d want %d (full=%v)", k, got[k], n, got)
		}
	}
}

// SEC-13 — ws_ticket_issued is recorded when a ticket is minted.
func TestAuthEventsRecordsWSTicketIssued(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	tok := registerOK(t, f, "alice", "correct-horse-battery")
	if rr := f.post(t, "/api/auth/ws-ticket", nil, tok); rr.Code != stdhttp.StatusOK {
		t.Fatalf("ws-ticket: %d", rr.Code)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind = ?`,
		AuthEventTicketIssued).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("ws_ticket_issued rows: got %d want 1", n)
	}
}

// SEC-13 — register_failed is recorded for every rejection branch in
// Register, with user_id NULL (no user exists at the point of the log
// call). Issue #462 broadens the original bad-invite-only assertion to
// each of the five branches so a regression that drops one of the
// h.logEvent calls in auth_handlers.go is caught by the unit suite.
// The hash-failure branch is excluded — it has no test seam without
// refactoring auth.Hash, which #462 puts out of scope.
func TestAuthEventsRecordsRegisterFailed(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, f *fixture)
		inviteCode string // server-side; empty means registration disabled
		body       map[string]string
		wantStatus int
	}{
		{
			name:       "disabled server invite",
			inviteCode: "",
			body: map[string]string{
				"username":    "alice",
				"password":    "correct-horse-battery",
				"invite_code": "anything",
			},
			wantStatus: stdhttp.StatusForbidden,
		},
		{
			name:       "bad invite code",
			inviteCode: "INVITE-OK",
			body: map[string]string{
				"username":    "alice",
				"password":    "correct-horse-battery",
				"invite_code": "WRONG",
			},
			wantStatus: stdhttp.StatusForbidden,
		},
		{
			name:       "username regex miss",
			inviteCode: "INVITE-OK",
			body: map[string]string{
				"username":    "ab", // < 3 chars
				"password":    "correct-horse-battery",
				"invite_code": "INVITE-OK",
			},
			wantStatus: stdhttp.StatusBadRequest,
		},
		{
			name:       "password policy violation",
			inviteCode: "INVITE-OK",
			body: map[string]string{
				"username":    "alice",
				"password":    "short", // < PasswordMinLen
				"invite_code": "INVITE-OK",
			},
			wantStatus: stdhttp.StatusBadRequest,
		},
		{
			name: "username taken",
			setup: func(t *testing.T, f *fixture) {
				registerOK(t, f, "alice", "correct-horse-battery")
			},
			inviteCode: "INVITE-OK",
			body: map[string]string{
				"username":    "alice",
				"password":    "another-correct-horse",
				"invite_code": "INVITE-OK",
			},
			wantStatus: stdhttp.StatusConflict,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixtureWithInvite(t, c.inviteCode)
			defer f.close()
			if c.setup != nil {
				c.setup(t, f)
			}
			// Reset auth_events so the seeded successful register from
			// the username-taken setup doesn't leak into the assertion.
			if _, err := f.db.Exec(`DELETE FROM auth_events`); err != nil {
				t.Fatalf("reset auth_events: %v", err)
			}
			rr := f.post(t, "/api/auth/register", c.body, "")
			if rr.Code != c.wantStatus {
				t.Fatalf("register: got %d want %d body=%s", rr.Code, c.wantStatus, rr.Body.String())
			}
			var n int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind = ? AND user_id IS NULL`,
				AuthEventRegisterFailed).Scan(&n); err != nil {
				t.Fatalf("query: %v", err)
			}
			if n != 1 {
				t.Fatalf("register_failed rows: got %d want 1", n)
			}
		})
	}
}

// SEC-13 — login_failure is recorded for wrong-password.
func TestAuthEventsRecordsLoginFailure(t *testing.T) {
	f := newFixture(t)
	defer f.close()
	registerOK(t, f, "alice", "correct-horse-battery")
	rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "wrong-password-here",
	}, "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("login: %d", rr.Code)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind = ?`,
		AuthEventLoginFailure).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("login_failure rows: got %d want 1", n)
	}
}

// helpers

func registerOK(t *testing.T, f *fixture, username, password string) string {
	t.Helper()
	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": "INVITE-OK",
	}, "")
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("register: %d body=%s", rr.Code, rr.Body.String())
	}
	return mustToken(t, rr)
}

func tokenVersion(t *testing.T, f *fixture, username string) int {
	t.Helper()
	var tv int
	if err := f.db.QueryRow(`SELECT token_version FROM users WHERE username = ?`, username).Scan(&tv); err != nil {
		t.Fatalf("token_version: %v", err)
	}
	return tv
}

// Sanity: bearer parsing happens in the middleware, not the handler.
// Cover the obvious shapes here so a regression in the parser surfaces
// as a 401 rather than a panic.
func TestMiddlewareRejectsMalformedBearer(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	bad := []string{"", "Token foo", "Bearer", "Bearer ", "bearer\n"}
	for _, h := range bad {
		req := httptest.NewRequest(stdhttp.MethodGet, "/api/auth/me", nil)
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		rr := httptest.NewRecorder()
		f.dispatch("/api/auth/me", rr, req)
		if rr.Code != stdhttp.StatusUnauthorized {
			t.Errorf("Authorization=%q: got %d want 401", h, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "unauthorized") {
			t.Errorf("Authorization=%q: body did not contain 'unauthorized': %s", h, rr.Body.String())
		}
	}
}
