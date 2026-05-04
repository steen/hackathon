// AC-4: A password length policy is enforced (e.g., min 8, max 72 to
// stay within bcrypt's input limit).
//
// The AC text uses "e.g." for the lower bound; the implementation in
// apps/server/internal/auth/constants.go pins PasswordMinLen=10 and
// PasswordMaxBytes=72 (per PRD §9). The test asserts the real boundary
// pair: length 9 rejected, length 10 accepted, length 72 accepted,
// length 73 rejected. After each rejection, the SQLite users table is
// queried read-only to confirm no row was inserted for that username.

package auth_internals_e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// passwordMinLen mirrors apps/server/internal/auth/constants.go
// PasswordMinLen. Duplicated as a local const so this E2E asserts the
// public, on-the-wire behaviour without importing the production
// package (CLAUDE.md: black-box tests).
const passwordMinLen = 10

// passwordMaxBytes mirrors apps/server/internal/auth/constants.go
// PasswordMaxBytes. bcrypt silently truncates past 72 bytes, so the
// server rejects to avoid two distinct passwords colliding.
const passwordMaxBytes = 72

func TestAC4_PasswordLengthPolicyEnforced(t *testing.T) {
	srv := startServer(t)

	cases := []struct {
		name       string
		passwdLen  int
		wantStatus int // 0 means accept (200/201)
	}{
		{"below-min by one", passwordMinLen - 1, http.StatusBadRequest},
		{"at min boundary", passwordMinLen, 0},
		{"at max boundary", passwordMaxBytes, 0},
		{"above max by one", passwordMaxBytes + 1, http.StatusBadRequest},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			username := uniqueUsername(t)
			password := strings.Repeat("a", c.passwdLen)
			status, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
				"username":    username,
				"password":    password,
				"invite_code": srv.inviteCode,
			})

			if c.wantStatus == 0 {
				if status != http.StatusCreated && status != http.StatusOK {
					t.Fatalf("len=%d: expected accept, got status %d body=%s", c.passwdLen, status, raw)
				}
				if !env.OK {
					t.Fatalf("len=%d: expected envelope ok=true, got %+v body=%s", c.passwdLen, env, raw)
				}
				return
			}

			if status != c.wantStatus {
				t.Fatalf("len=%d: status %d, want %d body=%s", c.passwdLen, status, c.wantStatus, raw)
			}
			if env.OK || env.Error == nil {
				t.Fatalf("len=%d: expected envelope ok=false with error, got %+v body=%s", c.passwdLen, env, raw)
			}
			if env.Error.Code == "" {
				t.Fatalf("len=%d: empty error.code in envelope body=%s", c.passwdLen, raw)
			}
			if env.Error.Message == "" {
				t.Fatalf("len=%d: empty error.message in envelope body=%s", c.passwdLen, raw)
			}
			assertNoUserRow(t, srv, username)
		})
	}
}

// uniqueUsername returns a fresh `u_<8 hex>` username so each subtest
// can assert "this username was never inserted" without colliding with
// a previous subtest's accepted registration.
func uniqueUsername(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return "u_" + hex.EncodeToString(b)
}

// assertNoUserRow opens the running server's SQLite read-only and
// fails the test if a row exists in `users` for the given username.
// Confirms a rejected register did not partially commit.
func assertNoUserRow(t *testing.T, srv *runningServer, username string) {
	t.Helper()
	db := openDBReadOnly(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&n); err != nil {
		t.Fatalf("count users for %q: %v", username, err)
	}
	if n != 0 {
		t.Fatalf("expected 0 user rows for %q after rejected register, got %d", username, n)
	}
}
