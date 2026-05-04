package rate_limits_e2e_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// AC-3: "Limits are observable in `auth_events` (rejected attempts logged)."
//
// The implementation logs rate-limit rejections via
// apps/server/internal/http/auth_store.go:LogRateLimited, which writes
// rows with kind = AuthEventRateLimited (= "rate_limited"). It is
// called from two places:
//
//   - apps/server/internal/http/middleware_ratelimit.go for per-IP
//     login/register limiter rejections (userID always empty).
//   - apps/server/internal/http/auth_handlers.go:183 for per-username
//     login backoff rejections (also userID empty — the handler does
//     not resolve the username to an ID before logging; see the
//     comment at line ~160 explaining why failure events have no
//     user_id).
//
// AC-3 says rejected attempts are observable in `auth_events`. The
// observable surface per-row is (kind, ip, ua, at). This test trips
// both the per-IP login limiter and the per-IP register limiter from
// 127.0.0.1, then opens the server's SQLite file read-only and asserts
// that `auth_events` has at least one row with kind="rate_limited" and
// ip="127.0.0.1" produced by each trip.
func TestAC3_RateLimitRejectionsLoggedToAuthEvents(t *testing.T) {
	srv := startServer(t)

	// Snapshot the rate_limited row count before we start so the
	// per-trip assertions are deltas, not absolutes (the test is
	// parallel-safe with future siblings that may also trip the
	// limiter; today the count starts at 0 because the harness gives
	// each test a fresh DB).
	db := openDBReadOnlyLocal(t, srv)
	startCount := countRateLimited(t, db)

	// 1. Trip the per-IP login limiter. LoginIPConfig.Burst = 10, so
	// the 11th attempt within the window is 429. The first 10 are
	// 401s (the IP limiter counts attempts, not auth outcomes). The
	// pre-existing AC-1 test (ip_rate_limit_test.go) uses the same
	// numbers; if iplimit.go's defaults move, both tests will need
	// updating — that's a feature, not a bug.
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 1; i <= 10; i++ {
		// Vary username to avoid tripping per-username backoff
		// before the per-IP limiter, same trick AC-1 uses.
		user := fmt.Sprintf("ac3-login-%03d", i)
		status, _, raw := loginRaw(t, client, srv, user, "wrong-password")
		if status == http.StatusTooManyRequests {
			t.Fatalf("login attempt %d/10 unexpectedly 429; LoginIPConfig burst=10 should permit the first 10 (body=%s)", i, raw)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("login attempt %d/10 status=%d, want 401; body=%s", i, status, raw)
		}
	}
	status, _, raw := loginRaw(t, client, srv, "ac3-login-011", "wrong-password")
	if status != http.StatusTooManyRequests {
		t.Fatalf("11th login attempt must be 429 to trip the IP limiter for AC-3; got %d body=%s", status, raw)
	}

	// 2. Trip the per-IP register limiter. RegisterIPConfig.Burst = 5.
	// We deliberately use a wrong invite_code so the handler doesn't
	// commit users rows; the IP limiter still counts each attempt.
	const wrongInvite = "definitely-not-the-invite"
	for i := 1; i <= 5; i++ {
		status, _, raw := registerRaw(t, client, srv, "ac3-reg", "correct-horse-battery", wrongInvite)
		if status == http.StatusTooManyRequests {
			t.Fatalf("register attempt %d/5 unexpectedly 429; RegisterIPConfig burst=5 should permit the first 5 (body=%s)", i, raw)
		}
	}
	status, _, raw = registerRaw(t, client, srv, "ac3-reg", "correct-horse-battery", wrongInvite)
	if status != http.StatusTooManyRequests {
		t.Fatalf("6th register attempt must be 429 to trip the IP limiter for AC-3; got %d body=%s", status, raw)
	}

	// The audit-log writes happen synchronously inside the handler /
	// middleware (LogRateLimited → INSERT INTO auth_events), so by
	// the time we got the 429 response the row is already committed.
	// No sleep needed; if a later refactor moves the write off the
	// hot path this test will fail loudly and that's the point.

	rows := selectRateLimitedRows(t, db)
	if got := len(rows) - startCount; got < 2 {
		t.Fatalf("auth_events: expected ≥2 new rate_limited rows after tripping login+register limiters, got %d new (total=%d, start=%d)", got, len(rows), startCount)
	}

	// Every rate_limited row from this test must have kind="rate_limited",
	// a non-NULL `at`, and ip="127.0.0.1" (the source IP we used).
	for _, r := range rows {
		if r.Kind != "rate_limited" {
			t.Errorf("row in selectRateLimitedRows has wrong kind=%q; query bug", r.Kind)
		}
		if !r.At.Valid {
			t.Errorf("auth_events rate_limited row has NULL at")
		}
		if !r.IP.Valid || r.IP.String != "127.0.0.1" {
			t.Errorf("auth_events rate_limited row has ip=%v; want 127.0.0.1 (the source IP these tests dial from)", r.IP)
		}
	}
}

// rateLimitEventRow mirrors the columns AC-3 asserts on. Defined in
// this file rather than the harness because no other rate-limit test
// reads from auth_events yet (CLAUDE.md: no shared abstraction until
// 3+ call sites need it).
type rateLimitEventRow struct {
	Kind string
	At   sql.NullTime
	IP   sql.NullString
}

func countRateLimited(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind = 'rate_limited'`).Scan(&n); err != nil {
		t.Fatalf("count rate_limited: %v", err)
	}
	return n
}

func selectRateLimitedRows(t *testing.T, db *sql.DB) []rateLimitEventRow {
	t.Helper()
	rs, err := db.Query(`SELECT kind, at, ip FROM auth_events WHERE kind = 'rate_limited' ORDER BY id`)
	if err != nil {
		t.Fatalf("select rate_limited rows: %v", err)
	}
	defer rs.Close()
	var out []rateLimitEventRow
	for rs.Next() {
		var r rateLimitEventRow
		if err := rs.Scan(&r.Kind, &r.At, &r.IP); err != nil {
			t.Fatalf("scan auth_events rate_limited: %v", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		t.Fatalf("iterate auth_events rate_limited: %v", err)
	}
	return out
}

// openDBReadOnlyLocal opens srv.dbPath in read-only mode. Inlined
// here (vs. moved to harness_test.go) because (a) AC-3 is the only
// rate-limit test that touches the DB right now, and (b) the agent
// dispatch told this PR not to modify existing files.
func openDBReadOnlyLocal(t *testing.T, srv *runningServer) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: srv.dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
