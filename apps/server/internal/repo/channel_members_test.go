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
