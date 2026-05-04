// AC-1: `internal/auth` exposes password hashing/verification using
// bcrypt with a sane cost.
//
// Internals are not directly observable from the wire, but bcrypt's
// effects are: register two users with the same password, then read
// their `password_hash` rows from the SQLite DB read-only and check
// (a) the prefix is one of the bcrypt variants `$2a$`/`$2b$`/`$2y$`,
// (b) the cost factor encoded after the second `$` is in the
// PRD-aligned sane range [10, 14], and (c) the two hashes differ
// (proving a per-record salt is in play).

package auth_internals_e2e_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// minBcryptCost is PRD §9 / OWASP floor; matches
// apps/server/internal/auth/constants.go (BcryptCost = 10).
const minBcryptCost = 10

// maxBcryptCost is the upper bound of "sane" for an MVP — bcrypt is
// not a KDF for offline scrypt-class adversaries here, and any cost
// above 14 would push login latency past a second per attempt.
const maxBcryptCost = 14

func TestAC1_BcryptHashUsesSaneCostAndPerRecordSalt(t *testing.T) {
	srv := startServer(t)

	// "internal/auth exposes password hashing/verification using bcrypt
	// with a sane cost." — feature-auth-internals.md AC-1.
	const sharedPassword = "correct-horse-battery-staple"
	register(t, srv, "alice", sharedPassword)
	register(t, srv, "bob", sharedPassword)

	db := openDBReadOnly(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx,
		`SELECT username, password_hash FROM users WHERE username IN ('alice','bob') ORDER BY username`)
	if err != nil {
		t.Fatalf("select password_hash: %v", err)
	}
	defer rows.Close()

	hashes := map[string]string{}
	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		hashes[username] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(hashes) != 2 {
		t.Fatalf("expected 2 user rows, got %d (%v)", len(hashes), hashes)
	}

	for username, hash := range hashes {
		assertBcryptHashShape(t, username, hash)
	}

	if hashes["alice"] == hashes["bob"] {
		t.Fatalf("identical password hashed to identical bytes for alice and bob — bcrypt salt is not in effect: %q", hashes["alice"])
	}
}

// assertBcryptHashShape parses a bcrypt-formatted hash and fails the
// test if the prefix or cost falls outside the sane range. A bcrypt
// hash has the form `$<variant>$<cost>$<22-char-salt><31-char-digest>`.
func assertBcryptHashShape(t *testing.T, username, hash string) {
	t.Helper()

	parts := strings.Split(hash, "$")
	// "" / variant / cost / saltdigest -> 4 segments.
	if len(parts) != 4 {
		t.Fatalf("%s: expected 4 `$`-separated segments in bcrypt hash, got %d (%q)", username, len(parts), hash)
	}

	variant := parts[1]
	switch variant {
	case "2a", "2b", "2y":
	default:
		t.Fatalf("%s: bcrypt variant %q not in {2a,2b,2y}; full=%q", username, variant, hash)
	}

	cost, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("%s: cost segment %q not an integer: %v (full=%q)", username, parts[2], err, hash)
	}
	if cost < minBcryptCost || cost > maxBcryptCost {
		t.Fatalf("%s: bcrypt cost %d outside sane range [%d,%d] (full=%q)", username, cost, minBcryptCost, maxBcryptCost, hash)
	}

	// salt+digest segment is fixed 53 chars in standard bcrypt output.
	if got := len(parts[3]); got != 53 {
		t.Fatalf("%s: salt+digest length %d != 53 (full=%q)", username, got, hash)
	}
}
