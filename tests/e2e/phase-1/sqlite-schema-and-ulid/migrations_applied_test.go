package sqlite_schema_and_ulid_e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// AC-3: A migration runner applies pending migrations on server startup.
//
// Black-box: boot the real apps/server binary against a fresh tempdir
// SQLite path. The runner in apps/server/internal/db/migrate.go must
// (a) create a `schema_migrations` bookkeeping table and (b) record
// each applied migration's filename. Open the file read-only and
// assert both: the table exists, and every `*.sql` file under
// migrations/ at the source tree has a matching row in
// schema_migrations after startup.
//
// Then stop the server and boot a second instance against the SAME
// CHAT_DB_PATH. The runner must be idempotent: schema_migrations
// row count stays equal — re-running pending detection on an
// already-applied file is a no-op.
func TestAC3_MigrationRunnerAppliesPendingMigrationsOnServerStartup(t *testing.T) {
	root := repoRoot(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chat-server")

	build := exec.Command("go", "build", "-o", binPath, "./apps/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./apps/server: %v\n%s", err, out)
	}

	dbPath := filepath.Join(tmpDir, "chatd.sqlite")
	jwtSecret := randomSecret(t, 32)
	invite := randomSecret(t, 8)

	wantMigrations := listSourceMigrations(t, root)
	if len(wantMigrations) == 0 {
		t.Fatalf("no migration .sql files found under %s/migrations", root)
	}

	srv1 := bootSharedDBServer(t, binPath, dbPath, jwtSecret, invite)
	db1 := openDBReadOnly(t, srv1)

	var hasTable int
	if err := db1.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`,
	).Scan(&hasTable); err != nil {
		t.Fatalf("query sqlite_master for schema_migrations: %v", err)
	}
	if hasTable != 1 {
		t.Fatalf("schema_migrations table missing after server startup")
	}

	gotFirst := readAppliedMigrations(t, db1)
	if !equalStringSlices(gotFirst, wantMigrations) {
		t.Errorf("after first boot: applied migrations = %v, want %v", gotFirst, wantMigrations)
	}

	stopSharedDBServer(t, srv1)

	srv2 := bootSharedDBServer(t, binPath, dbPath, jwtSecret, invite)
	db2 := openDBReadOnly(t, srv2)

	gotSecond := readAppliedMigrations(t, db2)
	if !equalStringSlices(gotSecond, wantMigrations) {
		t.Errorf("after second boot (idempotency): applied migrations = %v, want %v",
			gotSecond, wantMigrations)
	}
	if len(gotSecond) != len(gotFirst) {
		t.Errorf("idempotency violated: row count went from %d to %d",
			len(gotFirst), len(gotSecond))
	}
}

func listSourceMigrations(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if filepath.Ext(n) == ".sql" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func readAppliedMigrations(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		t.Fatalf("select schema_migrations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan schema_migrations row: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iter schema_migrations: %v", err)
	}
	return got
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bootSharedDBServer starts the chat-server binary against a caller-
// supplied dbPath / secrets / invite, choosing a fresh free port each
// call. Unlike the package-level startServer helper this does NOT
// register a t.Cleanup that cancels the process — the AC-3 test must
// stop and restart the server explicitly to exercise the idempotency
// path. The caller MUST invoke stopSharedDBServer to drain the wait
// channel.
func bootSharedDBServer(t *testing.T, binPath, dbPath, jwtSecret, invite string) *runningServer {
	t.Helper()

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAT_SERVER_PORT=%d", port),
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
		t.Fatalf("server did not listen on :%d: %v", port, err)
	}

	t.Cleanup(func() {
		cancel()
		<-wait
	})

	return &runningServer{
		httpURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
		dbPath:     dbPath,
		jwtSecret:  jwtSecret,
		inviteCode: invite,
		cancel:     cancel,
		wait:       wait,
	}
}

func stopSharedDBServer(t *testing.T, srv *runningServer) {
	t.Helper()
	srv.cancel()
	select {
	case <-srv.wait:
	case <-time.After(10 * time.Second):
		t.Fatalf("server did not exit within 10s of cancel")
	}
}
