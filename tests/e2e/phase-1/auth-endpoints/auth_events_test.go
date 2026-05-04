package auth_endpoints_e2e_test

import (
	"database/sql"
	"net/http"
	"testing"
)

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

	// login_failure is recorded with user_id NULL (handler logs failures
	// without resolving the username — apps/server/internal/http/auth_handlers.go).
	failures := selectAuthEventsByKind(t, db, "login_failure")
	if len(failures) == 0 {
		t.Errorf("auth_events: no login_failure rows after wrong-password login")
	}

	// register_failed is recorded with user_id NULL (no user is created
	// when the invite check rejects the request — same handler).
	regFailures := selectAuthEventsByKind(t, db, "register_failed")
	if len(regFailures) == 0 {
		t.Errorf("auth_events: no register_failed rows after bad-invite register")
	}

	// Every recorded row must carry a non-null `at`.
	for _, r := range rows {
		if !r.At.Valid {
			t.Errorf("auth_events row kind=%q has NULL at", r.Kind)
		}
	}
}

type eventRow struct {
	Kind string
	At   sql.NullTime
	IP   sql.NullString
}

func selectAuthEvents(t *testing.T, db *sql.DB, userID string) []eventRow {
	t.Helper()
	rs, err := db.Query(`SELECT kind, at, ip FROM auth_events WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("select auth_events: %v", err)
	}
	defer rs.Close()
	var out []eventRow
	for rs.Next() {
		var r eventRow
		if err := rs.Scan(&r.Kind, &r.At, &r.IP); err != nil {
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
	rs, err := db.Query(`SELECT kind, at, ip FROM auth_events WHERE kind = ? ORDER BY id`, kind)
	if err != nil {
		t.Fatalf("select auth_events by kind: %v", err)
	}
	defer rs.Close()
	var out []eventRow
	for rs.Next() {
		var r eventRow
		if err := rs.Scan(&r.Kind, &r.At, &r.IP); err != nil {
			t.Fatalf("scan auth_events: %v", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		t.Fatalf("iterate auth_events: %v", err)
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
