package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	stdhttp "net/http"
	"testing"
	"time"

	"hackathon/apps/server/internal/auth"
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

// registerWithKeys POSTs /api/auth/register with real ed25519 + box
// pubkeys. Returns the user id, JWT, and the 32-byte sign and box
// pubkey values plus the ed25519 private key for signing §10 blocks.
func registerWithKeys(t *testing.T, f *fixture, username, password string) (string, string, ed25519.PrivateKey, []byte, []byte) {
	t.Helper()
	_, signPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519 generate: %v", err)
	}
	signPub, ok := signPriv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519 public cast")
	}
	// Distinct 32-byte box pubkey. Each user gets a unique fill so
	// the L30 sender-pubkey check distinguishes them.
	boxPub := make([]byte, 32)
	for i := range boxPub {
		boxPub[i] = byte(i+1) ^ byte(len(username)) // simple uniqueness
	}
	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": "INVITE-OK",
		"box_pubkey":  base64.StdEncoding.EncodeToString(boxPub),
		"sign_pubkey": base64.StdEncoding.EncodeToString(signPub),
	}, "")
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("register %q: status=%d body=%s", username, rr.Code, rr.Body.String())
	}
	tok := mustToken(t, rr)
	var id string
	if err := f.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		t.Fatalf("lookup id for %q: %v", username, err)
	}
	return id, tok, signPriv, []byte(signPub), boxPub
}

// TestPrivateInviteRejectsWhenNoCreatorWrap — Phase 10 #1014: a
// wrap-carrying invite to a private channel that has no key
// generation on file (i.e. the creator skipped the keys-RPC bootstrap
// path) must be rejected with a 400, not silently bridged with the
// legacy creatorBootstrapGenID fallback. The fallback was removed
// once #984's bootstrap-mode keys-RPC landed.
func TestPrivateInviteRejectsWhenNoCreatorWrap(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()

	// Wire the members handler onto the fixture's mux so the request
	// goes through RequireJWT (the channels fixture already builds the
	// JWT middleware and registers the channels routes).
	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        []byte("test-signing-key-must-be-long-enough"),
		Lookup:            cf.handlers.LookupUserInfo,
		WriteUnauthorized: WriteUnauthorized,
		WithUserID:        WithUserID,
	})
	mh := NewMembersHandlers(MembersDeps{Repo: cf.repo, Hub: cf.hub, Now: time.Now})
	mh.Routes(cf.mux, require, nil)

	caller, tok, callerPriv, callerSignPub, callerBox := registerWithKeys(t, cf.fixture, "alice-no-boot", "correct-horse-battery")
	invitee, _, _, inviteeSign, inviteeBox := registerWithKeys(t, cf.fixture, "bob-no-boot", "correct-horse-battery")

	now := time.Now().UTC().Truncate(time.Second)

	// Private channel, creator membership row carries a non-empty
	// inviter_signature so the L33 NULL-sig check accepts; no
	// channel_keys rows exist (bootstrap was skipped).
	ch, err := cf.repo.CreateChannel(context.Background(), ids.NewULID(), "private-no-bootstrap", false, now)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := cf.repo.InsertChannelMember(context.Background(), repo.ChannelMember{
		ChannelID:         ch.ID,
		UserID:            caller,
		InviterUserID:     caller,
		InviterSignPubkey: callerSignPub,
		InviterSignature:  bytes.Repeat([]byte{0x42}, 64), // any valid-shape sig; not verified on read
		InviteeBoxPubkey:  callerBox,
		InviteeSignPubkey: callerSignPub,
		AddedAt:           now,
	}, false); err != nil {
		t.Fatalf("seed creator membership: %v", err)
	}

	// Sign the §10 scope so the handler's signature verification
	// accepts and flow reaches the MaxChannelKeyGenerationTx branch.
	msg := auth.MembershipSignatureMessage(ch.ID, invitee, caller, callerSignPub, auth.InviteePubkeys{
		BoxPubkey: inviteeBox, SignPubkey: inviteeSign,
	}, now)
	sig := ed25519.Sign(callerPriv, msg)

	body := map[string]any{
		"user_id": invitee,
		"membership": map[string]any{
			"inviter_user_id":     caller,
			"inviter_sign_pubkey": base64.StdEncoding.EncodeToString(callerSignPub),
			"invitee_box_pubkey":  base64.StdEncoding.EncodeToString(inviteeBox),
			"invitee_sign_pubkey": base64.StdEncoding.EncodeToString(inviteeSign),
			"added_at":            now.Format(time.RFC3339),
			"inviter_signature":   base64.StdEncoding.EncodeToString(sig),
		},
		"root_key_wrap": map[string]any{
			"wrapped_key":       base64.StdEncoding.EncodeToString(make([]byte, 48)),
			"sender_box_pubkey": base64.StdEncoding.EncodeToString(callerBox),
			"nonce":             base64.StdEncoding.EncodeToString(make([]byte, 24)),
		},
	}
	rr := cf.do(t, stdhttp.MethodPost, "/api/channels/"+ch.ID+"/members", body, tok)

	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status: got %d want 400 (body=%s)", rr.Code, rr.Body.String())
	}
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v body=%s", err, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != CodeBadRequest {
		t.Fatalf("error code: got %+v want %s", env.Error, CodeBadRequest)
	}
	// Spot-check the message names the keys-RPC remediation so
	// clients know which endpoint to call before retrying.
	if !bytes.Contains(rr.Body.Bytes(), []byte("/api/channels/")) {
		t.Fatalf("error message should reference the keys-RPC path; body=%s", rr.Body.String())
	}

	// L7 invariant: the rejection must NOT have written a partial
	// channel_members + channel_keys pair. Verify both tables stay
	// at the seed state (1 member, 0 keys).
	var memberCount, keyCount int
	if err := cf.repo.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM channel_members WHERE channel_id = ?`, ch.ID).Scan(&memberCount); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if memberCount != 1 {
		t.Fatalf("channel_members count after rejection: got %d want 1 (creator only)", memberCount)
	}
	if err := cf.repo.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM channel_keys WHERE channel_id = ?`, ch.ID).Scan(&keyCount); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("channel_keys count after rejection: got %d want 0", keyCount)
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
