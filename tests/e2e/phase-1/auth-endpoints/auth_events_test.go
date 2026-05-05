package auth_endpoints_e2e_test

import (
	"database/sql"
	"net/http"
	"testing"
)

// unknownProbeUser is the username we send into /api/auth/login expecting
// a 401 so we can assert the failure row carries the attempted username
// even though no user with this name exists. Kept long + random-ish so a
// stray real account cannot collide with it across test runs.
const unknownProbeUser = "nobody-probe-9c1f"

// AC-6: All auth endpoints write entries to auth_events.
//
// The kind strings come from apps/server/internal/http/auth_handlers.go:
// register, register_failed, login_success, login_failure, logout,
// ws_ticket_issued. We drive register → login (success) → ws-ticket →
// logout → login (wrong password) → register (bad invite) and assert
// each lands in auth_events with the right kind and a non-null `at`.
func TestAC6_AuthEvents_AllEndpointsLog(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	uid, _ := register(t, srv, username, password)
	tok := login(t, srv, username, password)
	mintTicket(t, srv, tok)

	// Logout uses the same token we just minted ws-ticket with — the
	// JWT's tv has not yet been bumped.
	status, _, raw := postJSON(t, srv, "/api/auth/logout", tok, nil)
	if status != http.StatusOK {
		t.Fatalf("/logout: status %d body %s", status, raw)
	}

	// One deliberate login failure: wrong password.
	status, _, _ = postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": username,
		"password": "definitely-wrong-" + randomSecret(t, 4),
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-password login expected 401, got %d", status)
	}

	// One deliberate login failure against an unknown username so we
	// can assert the audit row attributes the attempt (issue #716).
	status, _, _ = postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": unknownProbeUser,
		"password": "anything-" + randomSecret(t, 4),
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown-user login expected 401, got %d", status)
	}

	// One deliberate register failure: invalid invite code.
	status, _, raw = postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    "bob",
		"password":    randomSecret(t, 12),
		"invite_code": "wrong-" + randomSecret(t, 4),
	})
	if status != http.StatusForbidden {
		t.Fatalf("bad-invite register expected 403, got %d body %s", status, raw)
	}

	db := openDBReadOnly(t, srv)
	rows := selectAuthEvents(t, db, uid)
	mustHaveKind(t, rows, "register")
	mustHaveKind(t, rows, "login_success")
	mustHaveKind(t, rows, "ws_ticket_issued")
	mustHaveKind(t, rows, "logout")

	// login_failure for the wrong-password attempt against the existing
	// user: user_id stays NULL (auth.AuthenticateLogin collapses unknown-
	// user and wrong-password into one error so the handler cannot
	// resolve the user safely), but the username column must carry the
	// attempted username so the audit log is attributable (issue #716).
	failures := selectAuthEventsByKind(t, db, "login_failure")
	if len(failures) == 0 {
		t.Fatalf("auth_events: no login_failure rows after wrong-password login")
	}
	if !hasUsername(failures, username) {
		t.Errorf("auth_events login_failure: missing username=%q row, saw=%v",
			username, usernamesOf(failures))
	}
	if !hasUsername(failures, unknownProbeUser) {
		t.Errorf("auth_events login_failure: missing username=%q row, saw=%v",
			unknownProbeUser, usernamesOf(failures))
	}
	for _, r := range failures {
		if r.UserID.Valid {
			t.Errorf("auth_events login_failure: user_id should be NULL, got %q (username=%v)",
				r.UserID.String, r.Username)
		}
	}

	// register_failed is recorded with user_id NULL (no user is created
	// when the invite check rejects the request — same handler). The
	// username column carries the attempted name when the request body
	// included one (issue #716).
	regFailures := selectAuthEventsByKind(t, db, "register_failed")
	if len(regFailures) == 0 {
		t.Fatalf("auth_events: no register_failed rows after bad-invite register")
	}
	if !hasUsername(regFailures, "bob") {
		t.Errorf("auth_events register_failed: missing username=%q row, saw=%v",
			"bob", usernamesOf(regFailures))
	}

	// Successful kinds must carry the resolved username so a flat
	// audit-log query "what did alice do?" works without a join on
	// users (issue #716).
	for _, kind := range []string{"register", "login_success", "ws_ticket_issued", "logout"} {
		matched := false
		for _, r := range rows {
			if r.Kind == kind && r.Username.Valid && r.Username.String == username {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("auth_events kind=%q: missing row with username=%q (rows=%v)",
				kind, username, rows)
		}
	}

	// Every recorded row must carry a non-null `at`.
	for _, r := range rows {
		if !r.At.Valid {
			t.Errorf("auth_events row kind=%q has NULL at", r.Kind)
		}
	}
}

type eventRow struct {
	Kind     string
	At       sql.NullTime
	IP       sql.NullString
	UserID   sql.NullString
	Username sql.NullString
}

func selectAuthEvents(t *testing.T, db *sql.DB, userID string) []eventRow {
	t.Helper()
	rs, err := db.Query(`SELECT kind, at, ip, user_id, username FROM auth_events WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("select auth_events: %v", err)
	}
	defer rs.Close()
	var out []eventRow
	for rs.Next() {
		var r eventRow
		if err := rs.Scan(&r.Kind, &r.At, &r.IP, &r.UserID, &r.Username); err != nil {
			t.Fatalf("scan auth_events: %v", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		t.Fatalf("iterate auth_events: %v", err)
	}
	return out
}

func selectAuthEventsByKind(t *testing.T, db *sql.DB, kind string) []eventRow {
	t.Helper()
	rs, err := db.Query(`SELECT kind, at, ip, user_id, username FROM auth_events WHERE kind = ? ORDER BY id`, kind)
	if err != nil {
		t.Fatalf("select auth_events by kind: %v", err)
	}
	defer rs.Close()
	var out []eventRow
	for rs.Next() {
		var r eventRow
		if err := rs.Scan(&r.Kind, &r.At, &r.IP, &r.UserID, &r.Username); err != nil {
			t.Fatalf("scan auth_events: %v", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		t.Fatalf("iterate auth_events: %v", err)
	}
	return out
}

func hasUsername(rows []eventRow, want string) bool {
	for _, r := range rows {
		if r.Username.Valid && r.Username.String == want {
			return true
		}
	}
	return false
}

func usernamesOf(rows []eventRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Username.Valid {
			out = append(out, r.Username.String)
		} else {
			out = append(out, "<NULL>")
		}
	}
	return out
}

func mustHaveKind(t *testing.T, rows []eventRow, kind string) {
	t.Helper()
	for _, r := range rows {
		if r.Kind == kind {
			return
		}
	}
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.Kind)
	}
	t.Errorf("auth_events missing kind=%q; saw=%v", kind, got)
}
