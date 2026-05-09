package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// mkUser is a local helper that seeds a user row with a deterministic
// username derived from `tag`, so two seedings in the same test
// don't collide with each other on the case-insensitive uniqueness
// index added by migration 0006.
func mkUser(t *testing.T, r *repo.Repo, tag string) string {
	t.Helper()
	id := ids.NewULID()
	if _, err := r.DB().ExecContext(context.Background(),
		`INSERT INTO users(id, username, password_hash, token_version, created_at)
		 VALUES (?, ?, '', 0, ?)`, id, "u-"+tag, time.Now()); err != nil {
		t.Fatalf("seed user %q: %v", tag, err)
	}
	return id
}

// TestInsertChannelMemberL33RejectsNullSigOnPrivate pins the L33
// application-level rule: NULL inviter_signature is allowed ONLY
// for channels with is_public = TRUE. Private channels (the default)
// require a signature, and the typed sentinel surfaces the
// rejection so handlers can map to a 400 instead of an opaque
// constraint string.
func TestInsertChannelMemberL33RejectsNullSigOnPrivate(t *testing.T) {
	r, _ := newRepo(t)
	chID := ids.NewULID()
	uid := mkUser(t, r, "invitee")
	inviter := mkUser(t, r, "inviter")

	m := repo.ChannelMember{
		ChannelID:         chID,
		UserID:            uid,
		InviterUserID:     inviter,
		InviterSignPubkey: bytesOfLen(32),
		InviterSignature:  nil, // <- the L33 trigger
		InviteeBoxPubkey:  bytesOfLen(32),
		InviteeSignPubkey: bytesOfLen(32),
		AddedAt:           time.Now(),
	}
	err := r.InsertChannelMember(context.Background(), m, false)
	if !errors.Is(err, repo.ErrPrivateChannelNullSignature) {
		t.Fatalf("err: got %v want ErrPrivateChannelNullSignature", err)
	}
}

// TestInsertChannelMemberAcceptsNullSigOnPublic confirms the
// is_public = TRUE carve-out: server-side auto-add to #general (and
// any future public channel) inserts membership rows without a
// signature, since no client is present to compute one.
func TestInsertChannelMemberAcceptsNullSigOnPublic(t *testing.T) {
	r, _ := newRepo(t)

	// Need real channel + user rows for the FK constraints to hold.
	ch, err := r.CreateChannel(context.Background(), ids.NewULID(), "public-ch", true, time.Now())
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	uid := mkUser(t, r, "pub-invitee")
	inviter := mkUser(t, r, "pub-inviter")

	m := repo.ChannelMember{
		ChannelID:         ch.ID,
		UserID:            uid,
		InviterUserID:     inviter,
		InviterSignPubkey: bytesOfLen(32),
		InviterSignature:  nil,
		InviteeBoxPubkey:  bytesOfLen(32),
		InviteeSignPubkey: bytesOfLen(32),
		AddedAt:           time.Now(),
	}
	if err := r.InsertChannelMember(context.Background(), m, true); err != nil {
		t.Fatalf("insert: got %v want nil on public channel", err)
	}
}

func bytesOfLen(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// joinAsMember seeds a public channel membership row so the viewer
// passes the L25 filter on the listing/materialize path. The Phase-9
// channel_reads tests pre-date the explicit-membership rule and assume
// implicit-membership semantics; this helper backfills the row without
// reaching into the migration. The membership uses the public-channel
// carve-out (NULL signature) because the underlying channel was
// created with is_public=false in those tests — but the L33 enforce-
// ment runs against the channel's flag, not the row's, so the helper
// flips the row's is_public to TRUE for the duration of the insert.
//
// Tests that exercise membership semantics directly should NOT use
// this helper — they live in channel_members_test.go and seed via
// InsertChannelMember + a real public channel.
func joinAsMember(t *testing.T, r *repo.Repo, channelID, userID string) {
	t.Helper()
	// We bypass the Insert L33 guard with a direct SQL exec because the
	// helper backfills test rows where the underlying channel may have
	// is_public=false; the L33 enforcement is unit-tested in
	// channel_members_test.go above.
	if _, err := r.DB().ExecContext(context.Background(),
		`INSERT INTO channel_members(
		    channel_id, user_id, inviter_user_id,
		    inviter_sign_pubkey, inviter_signature,
		    invitee_box_pubkey, invitee_sign_pubkey,
		    added_at)
		 VALUES (?, ?, ?, ?, NULL, ?, ?, ?)`,
		channelID, userID, userID,
		bytesOfLen(32), bytesOfLen(32), bytesOfLen(32), time.Now(),
	); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
}
