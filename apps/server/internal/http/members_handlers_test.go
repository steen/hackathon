package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// seedUserRow inserts a users row directly so the test can reference
// the (id, username) without paying for the full /api/auth/register
// path. The pubkey columns stay empty — broadcastMembersChanged only
// joins username, not the pubkey columns.
func seedUserRow(t *testing.T, r *repo.Repo, username string) string {
	t.Helper()
	id := ids.NewULID()
	if _, err := r.DB().ExecContext(context.Background(),
		`INSERT INTO users(id, username, password_hash, token_version, created_at)
		 VALUES (?, ?, '', 0, ?)`, id, username, time.Now()); err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return id
}

// seedChannelKey writes a channel_keys row at generationID so
// MaxChannelKeyGeneration returns it. Wrap byte-fields use the right
// lengths (48-byte wrap, 32-byte sender, 24-byte nonce) so a future
// CHECK migration doesn't break the seed.
func seedChannelKey(t *testing.T, r *repo.Repo, channelID, memberID string, generationID int64) {
	t.Helper()
	if err := r.InsertChannelKey(context.Background(), repo.ChannelKey{
		ChannelID:       channelID,
		GenerationID:    generationID,
		MemberUserID:    memberID,
		WrappedKey:      make([]byte, 48),
		SenderBoxPubkey: make([]byte, 32),
		Nonce:           make([]byte, 24),
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("insert channel key: %v", err)
	}
}

// seedChannelMember writes a channel_members row using the public-channel
// auto-fill shape (NULL signature). Use a public channel in the test
// setup so the L33 check accepts.
func seedChannelMember(t *testing.T, r *repo.Repo, channelID, userID, inviterID string) {
	t.Helper()
	if err := r.InsertChannelMember(context.Background(), repo.ChannelMember{
		ChannelID:         channelID,
		UserID:            userID,
		InviterUserID:     inviterID,
		InviterSignPubkey: make([]byte, 32),
		InviteeBoxPubkey:  make([]byte, 32),
		InviteeSignPubkey: make([]byte, 32),
		AddedAt:           time.Now(),
	}, true); err != nil {
		t.Fatalf("insert channel member: %v", err)
	}
}

type membersChangedFrameWire struct {
	Type string `json:"type"`
	Data struct {
		Kind                string               `json:"kind"`
		ChannelID           string               `json:"channel_id"`
		CurrentGenerationID int64                `json:"current_generation_id"`
		MembersAtRotation   []MembersChangedUser `json:"members_at_rotation"`
	} `json:"data"`
}

// TestBroadcastMembersChangedPopulatesGenerationAndMembers — Phase 10
// follow-up to issue #1006 + PR #1027: the members_changed frame must
// carry the live MAX(channel_keys.generation_id) and the current
// channel_members projection, not the previous hardcoded
// `current_generation_id: 1` + empty array.
func TestBroadcastMembersChangedPopulatesGenerationAndMembers(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	r, err := repo.New(cf.db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	now := time.Now()

	// Seed a public channel so seedChannelMember's NULL-sig path is
	// allowed by the L33 check.
	ch, err := r.CreateChannel(context.Background(), ids.NewULID(), "members-changed", true, now)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	alice := seedUserRow(t, r, "alice-mc")
	bob := seedUserRow(t, r, "bob-mc")
	seedChannelMember(t, r, ch.ID, alice, alice)
	seedChannelMember(t, r, ch.ID, bob, alice)

	// Two wraps at gen 7 → MaxChannelKeyGeneration returns 7.
	seedChannelKey(t, r, ch.ID, alice, 7)
	seedChannelKey(t, r, ch.ID, bob, 7)

	h := hub.New()
	rec := &recorder{}
	h.Subscribe("watcher", rec)

	mh := NewMembersHandlers(MembersDeps{Repo: r, Hub: h, Now: time.Now})
	mh.broadcastMembersChanged(context.Background(), ch.ID)

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("frame count: got %d want 1", len(got))
	}
	var frame membersChangedFrameWire
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(got[0]))
	}
	if frame.Type != WSEventChannel {
		t.Fatalf("type: got %q want %q", frame.Type, WSEventChannel)
	}
	if frame.Data.Kind != "members_changed" {
		t.Fatalf("kind: got %q", frame.Data.Kind)
	}
	if frame.Data.ChannelID != ch.ID {
		t.Fatalf("channel_id: got %q want %q", frame.Data.ChannelID, ch.ID)
	}
	if frame.Data.CurrentGenerationID != 7 {
		t.Fatalf("current_generation_id: got %d want 7", frame.Data.CurrentGenerationID)
	}
	if len(frame.Data.MembersAtRotation) != 2 {
		t.Fatalf("members_at_rotation len: got %d want 2 (%+v)",
			len(frame.Data.MembersAtRotation), frame.Data.MembersAtRotation)
	}
	byID := map[string]string{}
	for _, m := range frame.Data.MembersAtRotation {
		byID[m.ID] = m.Username
	}
	if byID[alice] != "alice-mc" {
		t.Fatalf("alice username: got %q want %q", byID[alice], "alice-mc")
	}
	if byID[bob] != "bob-mc" {
		t.Fatalf("bob username: got %q want %q", byID[bob], "bob-mc")
	}
}

// TestBroadcastMembersChangedFallsBackToBootstrapGenWhenNoKeys —
// channels created via the legacy bootstrap path have zero wrap rows,
// so MaxChannelKeyGeneration returns hasGen=false. The frame must
// fall back to creatorBootstrapGenID rather than emitting a zero
// generation on the wire.
func TestBroadcastMembersChangedFallsBackToBootstrapGenWhenNoKeys(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	r, err := repo.New(cf.db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	ch, err := r.CreateChannel(context.Background(), ids.NewULID(), "no-keys-channel", true, time.Now())
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	alice := seedUserRow(t, r, "alice-bootstrap")
	seedChannelMember(t, r, ch.ID, alice, alice)

	h := hub.New()
	rec := &recorder{}
	h.Subscribe("watcher", rec)

	mh := NewMembersHandlers(MembersDeps{Repo: r, Hub: h, Now: time.Now})
	mh.broadcastMembersChanged(context.Background(), ch.ID)

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("frame count: got %d want 1", len(got))
	}
	var frame membersChangedFrameWire
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(got[0]))
	}
	if frame.Data.CurrentGenerationID != creatorBootstrapGenID {
		t.Fatalf("current_generation_id: got %d want %d (bootstrap fallback)",
			frame.Data.CurrentGenerationID, creatorBootstrapGenID)
	}
	if len(frame.Data.MembersAtRotation) != 1 || frame.Data.MembersAtRotation[0].ID != alice {
		t.Fatalf("members_at_rotation: %+v", frame.Data.MembersAtRotation)
	}
}

// TestBroadcastMembersChangedNilHubIsNoop guards the test path: when
// MembersDeps.Hub is nil (some unit tests construct without a hub) the
// helper must not panic and must not attempt the repo lookups.
func TestBroadcastMembersChangedNilHubIsNoop(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	r, err := repo.New(cf.db)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	mh := NewMembersHandlers(MembersDeps{Repo: r, Hub: nil, Now: time.Now})
	// Pass a non-existent channel id; with Hub nil the helper short-circuits
	// before touching the DB, so this must not error or panic.
	mh.broadcastMembersChanged(context.Background(), ids.NewULID())
}
