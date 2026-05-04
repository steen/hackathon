package sqlite_schema_and_ulid_e2e_test

import (
	"testing"
)

// AC-1: `migrations/0001_init.sql` defines tables for users, channels,
// messages, and `auth_events`.
//
// Black-box: boot the real apps/server binary against a fresh tempdir
// SQLite file (so the production migration runner applies 0001_init at
// startup), then open the same file read-only and read sqlite_master
// to assert the four tables exist.
func TestAC1_SchemaDefinesUsersChannelsMessagesAuthEvents(t *testing.T) {
	srv := startServer(t)
	db := openDBReadOnly(t, srv)

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iter sqlite_master: %v", err)
	}

	for _, want := range []string{"users", "channels", "messages", "auth_events"} {
		if !got[want] {
			t.Errorf("table %q missing from schema; sqlite_master tables = %v", want, keys(got))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
