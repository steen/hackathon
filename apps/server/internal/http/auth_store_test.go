package http

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/seed"
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

// TestCreateUser_AutoJoinUsesRepoInsertChannelMemberTx is the #1003
// regression: CreateUser must call repo.InsertChannelMemberTx for each
// public channel rather than running its own inline INSERT, so the L33
// NULL-signature carve-out stays in one place. Asserts the post-state
// rather than mocking the call: a row per public channel, with the
// expected pinned pubkeys + NULL inviter_signature + self-invite.
func TestCreateUser_AutoJoinUsesRepoInsertChannelMemberTx(t *testing.T) {
	db := openAuthStoreDB(t)
	r, err := repo.New(db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	ctx := context.Background()
	// Seed #general (is_public = TRUE) so CreateUser has a target.
	if err := seed.EnsureGeneralChannel(ctx, r); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Add a second public channel to confirm the loop covers >1 row.
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	if _, err := r.CreateChannel(ctx, ids.NewULID(), "public-2", true, now); err != nil {
		t.Fatalf("CreateChannel public-2: %v", err)
	}
	// And a non-public channel that must NOT receive an auto-join row.
	privID := ids.NewULID()
	if _, err := r.CreateChannel(ctx, privID, "private-1", false, now); err != nil {
		t.Fatalf("CreateChannel private-1: %v", err)
	}

	s := newAuthStoreWithRepo(db, r)
	userID := ids.NewULID()
	box := bytes.Repeat([]byte{0x11}, 32)
	sign := bytes.Repeat([]byte{0x22}, 32)
	if err := s.CreateUser(ctx, userID, "alice", "hash", box, sign, now); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Auto-join landed exactly one row per is_public channel.
	gotChannels, err := r.ListChannelsForMember(ctx, userID)
	if err != nil {
		t.Fatalf("ListChannelsForMember: %v", err)
	}
	if len(gotChannels) != 2 {
		t.Fatalf("auto-join channel count: got %d want 2 (general + public-2); got=%v", len(gotChannels), gotChannels)
	}
	for _, ch := range gotChannels {
		if ch == privID {
			t.Fatalf("auto-join leaked into private channel %q", ch)
		}
	}

	// Spot-check a row's shape: NULL signature, self-invite, pinned pubkeys.
	m, err := r.GetChannelMember(ctx, gotChannels[0], userID)
	if err != nil {
		t.Fatalf("GetChannelMember: %v", err)
	}
	if m == nil {
		t.Fatalf("GetChannelMember: nil row")
	}
	if m.InviterUserID != userID {
		t.Errorf("inviter_user_id: got %q want %q (self-invite)", m.InviterUserID, userID)
	}
	if len(m.InviterSignature) != 0 {
		t.Errorf("inviter_signature: want NULL/empty (L33 carve-out), got %d bytes", len(m.InviterSignature))
	}
	if !bytes.Equal(m.InviteeBoxPubkey, box) {
		t.Errorf("invitee_box_pubkey: pinned bytes mismatch")
	}
	if !bytes.Equal(m.InviteeSignPubkey, sign) {
		t.Errorf("invitee_sign_pubkey: pinned bytes mismatch")
	}
	if !bytes.Equal(m.InviterSignPubkey, sign) {
		t.Errorf("inviter_sign_pubkey: want self-invite pin == sign_pubkey")
	}
}

// TestCreateUser_AutoJoinPubkeyPlaceholderForLegacyRegistration locks in
// the boxPin/signPin zero-byte substitution at the auth-store level so
// the schema's NOT NULL on invitee_*_pubkey holds for a NULL-pubkey
// legacy registration (decision-log L26). Once #983 hard-requires
// pubkeys this branch goes away — the test goes with it.
func TestCreateUser_AutoJoinPubkeyPlaceholderForLegacyRegistration(t *testing.T) {
	db := openAuthStoreDB(t)
	r, err := repo.New(db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	ctx := context.Background()
	if err := seed.EnsureGeneralChannel(ctx, r); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := newAuthStoreWithRepo(db, r)
	userID := ids.NewULID()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	if err := s.CreateUser(ctx, userID, "bob", "hash", nil, nil, now); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	chans, err := r.ListChannelsForMember(ctx, userID)
	if err != nil {
		t.Fatalf("ListChannelsForMember: %v", err)
	}
	if len(chans) != 1 {
		t.Fatalf("auto-join channel count: got %d want 1", len(chans))
	}
	m, err := r.GetChannelMember(ctx, chans[0], userID)
	if err != nil || m == nil {
		t.Fatalf("GetChannelMember: %v / %v", err, m)
	}
	zeros := make([]byte, 32)
	if !bytes.Equal(m.InviteeBoxPubkey, zeros) {
		t.Errorf("legacy invitee_box_pubkey: want 32-byte zero placeholder, got %d bytes %x", len(m.InviteeBoxPubkey), m.InviteeBoxPubkey)
	}
	if !bytes.Equal(m.InviteeSignPubkey, zeros) {
		t.Errorf("legacy invitee_sign_pubkey: want 32-byte zero placeholder, got %d bytes", len(m.InviteeSignPubkey))
	}
	if !bytes.Equal(m.InviterSignPubkey, zeros) {
		t.Errorf("legacy inviter_sign_pubkey: want 32-byte zero placeholder, got %d bytes", len(m.InviterSignPubkey))
	}
	if len(m.InviterSignature) != 0 {
		t.Errorf("legacy inviter_signature: want NULL/empty, got %d bytes", len(m.InviterSignature))
	}
}

// TestCreateUser_AutoJoinFailsWithoutRepo asserts the safety net: a
// store built via the legacy db-only constructor refuses to insert
// auto-join rows. Without this guard a future caller could silently
// skip the auto-add (no public channels → no error → empty membership)
// and break the registration AC.
func TestCreateUser_AutoJoinFailsWithoutRepo(t *testing.T) {
	db := openAuthStoreDB(t)
	r, err := repo.New(db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	ctx := context.Background()
	if err := seed.EnsureGeneralChannel(ctx, r); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// db-only constructor — Repo not wired.
	s := newAuthStore(db)
	userID := ids.NewULID()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	err = s.CreateUser(ctx, userID, "carol", "hash", nil, nil, now)
	if err == nil {
		t.Fatalf("CreateUser: want error when repo unset and public channels exist, got nil")
	}
	if !strings.Contains(err.Error(), "requires *repo.Repo") {
		t.Fatalf("CreateUser error: got %q, want substring %q", err.Error(), "requires *repo.Repo")
	}
	// Tx rolled back: user row also absent.
	row := db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID)
	var dummy int
	if scanErr := row.Scan(&dummy); scanErr != sql.ErrNoRows {
		t.Fatalf("user row should have been rolled back: scan err=%v dummy=%d", scanErr, dummy)
	}
}
