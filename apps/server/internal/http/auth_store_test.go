package http

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/migrations"
)

// openAuthStoreDB spins up a fresh migrated SQLite for the LogAuthEvent
// truncation tests. Mirrors newFixture's bootstrap but skips the
// AuthHandlers wiring — these tests exercise the store directly.
func openAuthStoreDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := appdb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := appdb.ApplyFS(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("ApplyFS: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// readLastAuthEventUsername returns the username column of the most
// recently inserted auth_events row as (value, isNull).
func readLastAuthEventUsername(t *testing.T, db *sql.DB) (string, bool) {
	t.Helper()
	var u sql.NullString
	row := db.QueryRowContext(context.Background(),
		`SELECT username FROM auth_events ORDER BY at DESC, rowid DESC LIMIT 1`)
	if err := row.Scan(&u); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return u.String, !u.Valid
}

func TestLogAuthEvent_UsernameLengthCap(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantNull bool
		want     string
	}{
		{
			name:     "empty stays NULL",
			input:    "",
			wantNull: true,
		},
		{
			name:  "32-char passes through",
			input: strings.Repeat("a", 32),
			want:  strings.Repeat("a", 32),
		},
		{
			name:  "64-char boundary passes through",
			input: strings.Repeat("b", 64),
			want:  strings.Repeat("b", 64),
		},
		{
			name:  "100-char truncated to 64-char prefix",
			input: strings.Repeat("c", 100),
			want:  strings.Repeat("c", 64),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openAuthStoreDB(t)
			s := newAuthStore(db)
			if err := s.LogAuthEvent(context.Background(), "", tc.input, AuthEventLoginFailure, "127.0.0.1", "ua"); err != nil {
				t.Fatalf("LogAuthEvent: %v", err)
			}
			got, isNull := readLastAuthEventUsername(t, db)
			if tc.wantNull {
				if !isNull {
					t.Fatalf("want NULL username, got %q", got)
				}
				return
			}
			if isNull {
				t.Fatalf("want %q, got NULL", tc.want)
			}
			if got != tc.want {
				t.Fatalf("username mismatch: want %q (len=%d), got %q (len=%d)", tc.want, len(tc.want), got, len(got))
			}
		})
	}
}
