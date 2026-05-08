package auth_endpoint_paths_align_with_prd_e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	_ "modernc.org/sqlite"
)

// goldenServer is the parallel running-server handle this test owns
// (separate from the harness's runningServer because that struct does
// not expose the random invite code; the AC-4 flow needs to call
// /api/auth/register, which requires the code). Built by
// startGoldenServer below.
type goldenServer struct {
	httpURL    string
	dbPath     string
	inviteCode string
	jwtSecret  string
	wait       chan struct{}
	cancel     context.CancelFunc
}

// startGoldenServer boots the production chat-server binary the same
// way harness_test.go's startServer does, but RETAINS the random
// invite code + JWT secret so the test below can drive a real
// register → login → me → ws-ticket → /ws → logout flow.
//
// Reusing the harness's startServer would force this test to read
// the invite code out of the running child process (not portable),
// or widen the harness's runningServer type (out of footprint scope
// for "add a test file"). Cleaner: keep the existing harness
// untouched and bring up our own server with a captured-in-Go
// invite code.
func startGoldenServer(t *testing.T) *goldenServer {
	t.Helper()

	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server failed: %v\n%s", err, out)
	}

	port := freePort(t)
	jwtSecret := randomSecret(t, 32)
	invite := randomSecret(t, 8)
	dbPath := filepath.Join(tmpDir, "chatd.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_LISTEN_ADDR=127.0.0.1:%d", port),
		"CHAT_JWT_SECRET="+jwtSecret,
		"CHAT_INVITE_CODE="+invite,
		"CHAT_DB_PATH="+dbPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	wait := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(wait)
	}()

	if err := waitForPort(port, 10*time.Second); err != nil {
		cancel()
		<-wait
		t.Fatalf("server did not listen on :%d in time: %v", port, err)
	}

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &goldenServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		dbPath:     dbPath,
		inviteCode: invite,
		jwtSecret:  jwtSecret,
		wait:       wait,
		cancel:     cancel,
	}
}

// TestAC4_PathRenameIsBehaviourPreserving covers AC-4 verbatim:
//
//	"No other behavioural change — request/response shapes, JWT
//	semantics, ticket TTL, audit-log entries all remain as PR #38
//	implemented them. This plan is path-only."
//
// Strategy: drive the full happy-path register → login → me →
// ws-ticket → /ws upgrade → logout → me-after-logout sequence
// against the PRD §10 paths, pinning at each step the exact
// envelope keys, status codes, JWT claim semantics, ws-ticket TTL,
// and the audit-log row kinds the merged PR #38 code emits. A
// behavioural drift in any of these (e.g. a ticket TTL change, a
// missing auth_events row, an envelope key rename, a JWT issuer
// flip) breaks this test even though the path layout is correct.
//
// Assertions are tight on shape but loose on values that
// legitimately vary per run (token contents, ULID bytes, exact
// timestamps), so behaviour-preserving refactors don't flake the
// test.
func TestAC4_PathRenameIsBehaviourPreserving(t *testing.T) {
	t.Parallel()

	srv := startGoldenServer(t)
	client := &http.Client{Timeout: 10 * time.Second}

	username := fmt.Sprintf("ac4-%s", randomSecret(t, 4))
	password := "ac4-test-password-1234567890"

	// --- 1. Register --------------------------------------------------
	registerStart := time.Now()
	regBody := map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	}
	regStatus, regEnv := doJSON(t, client, http.MethodPost,
		srv.httpURL+"/api/auth/register", regBody, "")
	if regStatus != http.StatusCreated {
		t.Fatalf("register: status=%d body=%s", regStatus, mustJSON(regEnv))
	}
	requireOKEnvelope(t, regEnv)
	regData, ok := regEnv["data"].(map[string]any)
	if !ok {
		t.Fatalf("register: data not an object: %s", mustJSON(regEnv))
	}
	regToken, _ := regData["token"].(string)
	if regToken == "" {
		t.Fatalf("register: data.token missing/empty: %s", mustJSON(regEnv))
	}
	regUser, ok := regData["user"].(map[string]any)
	if !ok {
		t.Fatalf("register: data.user not an object: %s", mustJSON(regEnv))
	}
	userID, _ := regUser["id"].(string)
	if !looksLikeULID(userID) {
		t.Fatalf("register: data.user.id not ULID-shaped: %q", userID)
	}
	if got, _ := regUser["username"].(string); got != username {
		t.Fatalf("register: data.user.username=%q want %q", got, username)
	}
	requireJWTSemantics(t, regToken, userID, 0, registerStart)

	// --- 2. Login -----------------------------------------------------
	loginStart := time.Now()
	loginStatus, loginEnv := doJSON(t, client, http.MethodPost,
		srv.httpURL+"/api/auth/login",
		map[string]string{"username": username, "password": password}, "")
	if loginStatus != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", loginStatus, mustJSON(loginEnv))
	}
	requireOKEnvelope(t, loginEnv)
	loginData, _ := loginEnv["data"].(map[string]any)
	loginToken, _ := loginData["token"].(string)
	if loginToken == "" {
		t.Fatalf("login: data.token missing: %s", mustJSON(loginEnv))
	}
	loginUser, _ := loginData["user"].(map[string]any)
	if id, _ := loginUser["id"].(string); id != userID {
		t.Fatalf("login: data.user.id=%q want %q", id, userID)
	}
	if uname, _ := loginUser["username"].(string); uname != username {
		t.Fatalf("login: data.user.username=%q want %q", uname, username)
	}
	requireJWTSemantics(t, loginToken, userID, 0, loginStart)

	// --- 3. Me (with login token) ------------------------------------
	meStatus, meEnv := doJSON(t, client, http.MethodGet,
		srv.httpURL+"/api/auth/me", nil, loginToken)
	if meStatus != http.StatusOK {
		t.Fatalf("me: status=%d body=%s", meStatus, mustJSON(meEnv))
	}
	requireOKEnvelope(t, meEnv)
	meData, _ := meEnv["data"].(map[string]any)
	meUser, _ := meData["user"].(map[string]any)
	if id, _ := meUser["id"].(string); id != userID {
		t.Fatalf("me: data.user.id=%q want %q", id, userID)
	}
	if uname, _ := meUser["username"].(string); uname != username {
		t.Fatalf("me: data.user.username=%q want %q", uname, username)
	}

	// --- 4. WS-ticket -------------------------------------------------
	ticketIssuedAt := time.Now()
	ticketStatus, ticketEnv := doJSON(t, client, http.MethodPost,
		srv.httpURL+"/api/auth/ws-ticket", nil, loginToken)
	if ticketStatus != http.StatusOK {
		t.Fatalf("ws-ticket: status=%d body=%s", ticketStatus, mustJSON(ticketEnv))
	}
	requireOKEnvelope(t, ticketEnv)
	ticketData, _ := ticketEnv["data"].(map[string]any)
	ticket, _ := ticketData["ticket"].(string)
	if ticket == "" {
		t.Fatalf("ws-ticket: data.ticket missing: %s", mustJSON(ticketEnv))
	}
	expRaw, _ := ticketData["expires_at"].(string)
	if expRaw == "" {
		t.Fatalf("ws-ticket: data.expires_at missing: %s", mustJSON(ticketEnv))
	}
	expAt, err := time.Parse(time.RFC3339Nano, expRaw)
	if err != nil {
		t.Fatalf("ws-ticket: data.expires_at not RFC3339: %q (%v)", expRaw, err)
	}
	// PRD §9 / auth.TicketTTL = 30s. Allow 2s slop on either side
	// for scheduler jitter between server and test clocks.
	gotTTL := expAt.Sub(ticketIssuedAt)
	if gotTTL < 28*time.Second || gotTTL > 32*time.Second {
		t.Fatalf("ws-ticket TTL = %v, want ~30s (PRD §9)", gotTTL)
	}

	// --- 5. /ws upgrade redeems the ticket ---------------------------
	// Look up the seeded "general" channel ULID; the WS handler now
	// requires ?channel= against a real ULID.
	channelsStatus, channelsEnv := doJSON(t, client, http.MethodGet,
		srv.httpURL+"/api/channels", nil, loginToken)
	if channelsStatus != http.StatusOK {
		t.Fatalf("/api/channels: status=%d body=%s", channelsStatus, mustJSON(channelsEnv))
	}
	channelsData, _ := channelsEnv["data"].(map[string]any)
	channelsList, _ := channelsData["channels"].([]any)
	channelID := ""
	for _, raw := range channelsList {
		row, _ := raw.(map[string]any)
		if name, _ := row["name"].(string); name == "general" {
			channelID, _ = row["id"].(string)
			break
		}
	}
	if channelID == "" {
		t.Fatalf("seeded 'general' channel not found in /api/channels: %s", mustJSON(channelsEnv))
	}
	wsURL := "ws" + strings.TrimPrefix(srv.httpURL, "http") + "/ws?ticket=" + ticket + "&channel=" + channelID
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wsCancel()
	conn, resp, err := websocket.Dial(wsCtx, wsURL, nil)
	if err != nil {
		body := ""
		if resp != nil && resp.Body != nil {
			b, _ := io.ReadAll(resp.Body)
			body = string(b)
			_ = resp.Body.Close()
		}
		t.Fatalf("ws dial: %v (resp body=%q)", err, body)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("ws upgrade status=%d want 101", resp.StatusCode)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "test done")

	// --- 6. Logout ----------------------------------------------------
	logoutStatus, logoutEnv := doJSON(t, client, http.MethodPost,
		srv.httpURL+"/api/auth/logout", nil, loginToken)
	if logoutStatus != http.StatusOK {
		t.Fatalf("logout: status=%d body=%s", logoutStatus, mustJSON(logoutEnv))
	}
	requireOKEnvelope(t, logoutEnv)
	logoutData, _ := logoutEnv["data"].(map[string]any)
	if ok, _ := logoutData["ok"].(bool); !ok {
		t.Fatalf("logout: data.ok=%v want true: %s", logoutData["ok"], mustJSON(logoutEnv))
	}

	// --- 7. Me after logout — bumped tv must invalidate the bearer --
	postLogoutStatus, postLogoutEnv := doJSON(t, client, http.MethodGet,
		srv.httpURL+"/api/auth/me", nil, loginToken)
	if postLogoutStatus != http.StatusUnauthorized {
		t.Fatalf("me-after-logout: status=%d want 401 (tv must increment); body=%s",
			postLogoutStatus, mustJSON(postLogoutEnv))
	}
	if ok, _ := postLogoutEnv["ok"].(bool); ok {
		t.Fatalf("me-after-logout: envelope.ok=true want false: %s", mustJSON(postLogoutEnv))
	}
	errBody, _ := postLogoutEnv["error"].(map[string]any)
	if errBody == nil {
		t.Fatalf("me-after-logout: envelope.error missing: %s", mustJSON(postLogoutEnv))
	}
	if code, _ := errBody["code"].(string); code != "unauthorized" {
		t.Fatalf("me-after-logout: error.code=%q want %q", code, "unauthorized")
	}

	// --- 8. Audit-log entries pinned ---------------------------------
	// Pin the kinds the merged PR #38 code emits at each step. Order
	// is deterministic because the test runs sequentially and each
	// handler writes its event before responding. A behavioural drift
	// here (kind rename, missing event, wrong user_id linkage) breaks
	// AC-4.
	requireAuditEvents(t, srv.dbPath, userID, []string{
		"register",
		"login_success",
		"ws_ticket_issued",
		"logout",
	})
}

// requireOKEnvelope asserts the response carries the PRD §10 envelope
// shape on the success arm: ok=true, data is non-nil, error is nil.
func requireOKEnvelope(t *testing.T, env map[string]any) {
	t.Helper()
	ok, hasOK := env["ok"]
	if !hasOK {
		t.Fatalf("envelope.ok missing: %s", mustJSON(env))
	}
	if b, _ := ok.(bool); !b {
		t.Fatalf("envelope.ok=%v want true: %s", ok, mustJSON(env))
	}
	if _, has := env["data"]; !has {
		t.Fatalf("envelope.data key missing: %s", mustJSON(env))
	}
	if env["data"] == nil {
		t.Fatalf("envelope.data nil on success arm: %s", mustJSON(env))
	}
	if _, has := env["error"]; !has {
		t.Fatalf("envelope.error key missing: %s", mustJSON(env))
	}
	if env["error"] != nil {
		t.Fatalf("envelope.error non-nil on success arm: %s", mustJSON(env))
	}
}

// requireJWTSemantics asserts the token PR #38 issues is HS256, three
// dot-separated base64url segments, with the JWT claims the merged
// code sets: iss="chat-server", sub=<userID>, tv=<wantTV>, iat ~now,
// exp ~iat+7d. Signature is not re-verified — server-side jwt
// package tests already cover that. AC-4 is about claim shape, not
// signing-key handling.
func requireJWTSemantics(t *testing.T, tok, wantSub string, wantTV int, issuedNear time.Time) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT: want 3 dot-separated segments, got %d (%q)", len(parts), tok)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("JWT header b64: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("JWT header json: %v", err)
	}
	if alg, _ := header["alg"].(string); alg != "HS256" {
		t.Fatalf("JWT alg=%q want HS256 (PR #38 default)", alg)
	}
	if typ, _ := header["typ"].(string); typ != "JWT" {
		t.Fatalf("JWT typ=%q want JWT", typ)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("JWT payload b64: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		t.Fatalf("JWT payload json: %v", err)
	}
	if iss, _ := claims["iss"].(string); iss != "chat-server" {
		t.Fatalf("JWT iss=%q want %q", iss, "chat-server")
	}
	if sub, _ := claims["sub"].(string); sub != wantSub {
		t.Fatalf("JWT sub=%q want %q", sub, wantSub)
	}
	tvNum, ok := claims["tv"].(float64)
	if !ok {
		t.Fatalf("JWT tv missing or non-numeric: %v", claims["tv"])
	}
	if int(tvNum) != wantTV {
		t.Fatalf("JWT tv=%v want %d", tvNum, wantTV)
	}
	iat, ok := claims["iat"].(float64)
	if !ok {
		t.Fatalf("JWT iat missing: %v", claims["iat"])
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("JWT exp missing: %v", claims["exp"])
	}
	// PRD §9 / auth.JWTTTL = 7 days. Allow 5s slop on iat vs the
	// test's stopwatch and 5s slop on the 7d delta.
	iatTime := time.Unix(int64(iat), 0)
	if delta := iatTime.Sub(issuedNear); delta < -5*time.Second || delta > 5*time.Second {
		t.Fatalf("JWT iat=%v far from issuedNear=%v (delta %v)", iatTime, issuedNear, delta)
	}
	want := iatTime.Add(7 * 24 * time.Hour)
	expTime := time.Unix(int64(exp), 0)
	if delta := expTime.Sub(want); delta < -5*time.Second || delta > 5*time.Second {
		t.Fatalf("JWT exp=%v far from iat+7d=%v (delta %v)", expTime, want, delta)
	}
}

// looksLikeULID accepts a 26-char Crockford-base32 string. Production
// returns ULIDs from ids.NewULID; we re-validate the alphabet here
// rather than importing across the e2e package boundary.
func looksLikeULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return true
}

// doJSON sends a JSON request (or no body if body==nil) and decodes
// the JSON envelope. Bearer auth is set when token != "". Returns
// the HTTP status and the decoded envelope.
func doJSON(t *testing.T, client *http.Client, method, urlStr string, body any, token string) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, urlStr, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, urlStr, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", method, urlStr, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", urlStr, err)
	}
	var env map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope %s (status %d): %v\nraw=%s",
				urlStr, resp.StatusCode, err, string(raw))
		}
	}
	return resp.StatusCode, env
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json err: %v>", err)
	}
	return string(b)
}

// requireAuditEvents pins the auth_events rows the merged PR #38 code
// writes during the happy-path flow. Asserts the kinds match in
// order, the linked user_id is set for every user-bound kind, and
// the ip + ua columns are populated. Failure-arm kinds
// (login_failure, rate_limited) are intentionally NOT in the
// expected list — this test exercises the success path only.
func requireAuditEvents(t *testing.T, dbPath, userID string, want []string) {
	t.Helper()

	db := openAuditDB(t, dbPath)

	// Poll briefly: the logout response races the audit-row commit
	// because logEvent fires-and-logs-on-error after WriteOK. 1s is
	// plenty of headroom; the file is local sqlite.
	deadline := time.Now().Add(1 * time.Second)
	var got []auditRow
	for {
		rows, err := db.Query(
			`SELECT user_id, kind, ip, ua FROM auth_events
			 WHERE user_id = ? OR user_id IS NULL
			 ORDER BY at ASC, rowid ASC`, userID)
		if err != nil {
			t.Fatalf("select auth_events: %v", err)
		}
		got = got[:0]
		for rows.Next() {
			var r auditRow
			var uid sql.NullString
			if err := rows.Scan(&uid, &r.kind, &r.ip, &r.ua); err != nil {
				_ = rows.Close()
				t.Fatalf("scan auth_events: %v", err)
			}
			if uid.Valid {
				r.userID = uid.String
			}
			got = append(got, r)
		}
		_ = rows.Close()

		if len(got) >= len(want) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	wantSet := make(map[string]bool, len(want))
	for _, k := range want {
		wantSet[k] = true
	}
	var filtered []auditRow
	for _, r := range got {
		if wantSet[r.kind] {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) != len(want) {
		t.Fatalf("auth_events: got %d kinds, want %d. all rows=%+v filtered=%+v want=%v",
			len(filtered), len(want), got, filtered, want)
	}
	for i, r := range filtered {
		if r.kind != want[i] {
			t.Fatalf("auth_events[%d].kind=%q want %q (got=%+v)", i, r.kind, want[i], filtered)
		}
		if r.userID != userID {
			t.Fatalf("auth_events[%d] (kind=%s) user_id=%q want %q",
				i, r.kind, r.userID, userID)
		}
		if r.ip == "" {
			t.Fatalf("auth_events[%d] (kind=%s) ip empty — PR #38 sets clientIP",
				i, r.kind)
		}
		if r.ua == "" {
			t.Fatalf("auth_events[%d] (kind=%s) ua empty — PR #38 sets r.UserAgent",
				i, r.kind)
		}
	}
}

type auditRow struct {
	userID string
	kind   string
	ip     string
	ua     string
}

// openAuditDB opens the running server's SQLite file read-only so
// the test can SELECT from auth_events without contending with the
// server's own writes. Driver tag matches the modernc.org/sqlite
// blank import at the top of this file.
func openAuditDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
