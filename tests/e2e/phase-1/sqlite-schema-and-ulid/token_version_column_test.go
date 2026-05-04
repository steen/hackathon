package sqlite_schema_and_ulid_e2e_test

import (
	"strings"
	"testing"
)

// AC-2: The `users` table includes a `token_version` column (used by
// US-12 logout-invalidation).
//
// Black-box: boot the real apps/server binary so the production
// migration runner applies 0001_init, then read the schema via
// PRAGMA table_info('users') and assert a `token_version` column
// exists with INTEGER affinity and NOT NULL. The behavioural half
// (logout increments token_version) is already covered by
// tests/e2e/phase-1/auth-endpoints/logout_test.go (AC-4 of that
// feature); this test pins the column-existence half to the
// sqlite-schema-and-ulid feature dir per the test analysis.
func TestAC2_UsersTableHasTokenVersionColumn(t *testing.T) {
	srv := startServer(t)
	db := openDBReadOnly(t, srv)

	rows, err := db.Query(`SELECT name, type, "notnull" FROM pragma_table_info('users')`)
	if err != nil {
		t.Fatalf("pragma_table_info(users): %v", err)
	}
	defer rows.Close()

	type col struct {
		colType string
		notNull int
	}
	cols := map[string]col{}
	for rows.Next() {
		var name, ctype string
		var notNull int
		if err := rows.Scan(&name, &ctype, &notNull); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = col{colType: ctype, notNull: notNull}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iter pragma_table_info: %v", err)
	}

	tv, ok := cols["token_version"]
	if !ok {
		names := make([]string, 0, len(cols))
		for k := range cols {
			names = append(names, k)
		}
		t.Fatalf("users.token_version column missing; columns = %v", names)
	}
	if !strings.EqualFold(tv.colType, "INTEGER") {
		t.Errorf("users.token_version type = %q, want INTEGER", tv.colType)
	}
	if tv.notNull != 1 {
		t.Errorf("users.token_version notnull = %d, want 1 (NOT NULL)", tv.notNull)
	}
}
