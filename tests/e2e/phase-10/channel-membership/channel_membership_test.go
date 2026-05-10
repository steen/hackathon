// Package channel_membership_e2e_test exercises the Phase-10 channel
// membership surface end-to-end against the production server binary
// (decision-log L27 black-box harness): invite, kick, self-leave,
// #general immutability (L8), is_public auto-add at registration (§9 +
// R1.2), and the L25 listing filter that hides non-member channels.
//
// REWRITTEN by #982 in lockstep with the wrap-carrying invite contract:
// private-channel invites now require BOTH the §10-signed
// MembershipBlock AND a root_key_wrap. Public-channel auto-fill
// (NULL signature, no wrap) is preserved unchanged.
package channel_membership_e2e_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

	"hackathon/tests/e2e/internal/testsupport"
)

// membershipSignatureScopePrefix mirrors
// apps/server/internal/auth/membership_verify.go's domain-separation
// tag. The internal package can't be imported from tests/e2e, so the
// constant + signing helpers are duplicated here. Drift fails the
// e2e signature tests below — the canonical source is the server
// constant; this is a copy that must move when the server moves.
const membershipSignatureScopePrefix = "snakd-mship-v1:"

// membershipSignatureMessage is a verbatim copy of
// auth.MembershipSignatureMessage. Kept in lockstep with the server
// — the e2e tests in this file fail loudly if it drifts.
func membershipSignatureMessage(
	channelID, userID, inviterUserID string,
	inviterSignPubkey []byte,
	inviteeBoxPubkey, inviteeSignPubkey []byte,
	addedAt time.Time,
) []byte {
	stamp := addedAt.UTC().Format(time.RFC3339)
	sep := []byte("|")
	out := make([]byte, 0, 256)
	out = append(out, []byte(membershipSignatureScopePrefix)...)
	out = append(out, []byte(channelID)...)
	out = append(out, sep...)
	out = append(out, []byte(userID)...)
	out = append(out, sep...)
	out = append(out, []byte(inviterUserID)...)
	out = append(out, sep...)
	out = append(out, inviterSignPubkey...)
	out = append(out, sep...)
	out = append(out, inviteeBoxPubkey...)
	out = append(out, sep...)
	out = append(out, inviteeSignPubkey...)
	out = append(out, sep...)
	out = append(out, []byte(stamp)...)
	return out
}

// boxSeedKeypair mirrors auth.BoxSeedKeypair (libsodium semantics).
func boxSeedKeypair(seed []byte) ([32]byte, [32]byte) {
	if len(seed) != 32 {
		panic("boxSeedKeypair: seed must be 32 bytes")
	}
	h := sha512.Sum512(seed)
	var priv [32]byte
	copy(priv[:], h[:32])
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pubBytes, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		panic("boxSeedKeypair: " + err.Error())
	}
	var pub [32]byte
	copy(pub[:], pubBytes)
	return pub, priv
}

var stdEnc = base64.StdEncoding

// registerUserWithIdentity registers a fresh user, derives a Phase-10
// identity from their passphrase, and uploads the pubkeys via the
// register payload's box_pubkey/sign_pubkey extras. The returned
// identity is what test bodies sign membership blocks with.
type fixtureUser struct {
	UserID   string
	Token    string
	BoxPub   []byte
	BoxPriv  []byte
	SignPub  ed25519.PublicKey
	SignSeed []byte
}

func registerFixture(t *testing.T, srv *testsupport.Server, prefix string, signSeedByte, boxSeedByte byte) fixtureUser {
	t.Helper()
	name := prefix + "-" + testsupport.RandomSecret(t, 4)
	pw := "test-passphrase-" + testsupport.RandomSecret(t, 8)
	signSeed := bytesRepeat(signSeedByte, 32)
	signPriv := ed25519.NewKeyFromSeed(signSeed)
	signPub, ok := signPriv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519: NewKeyFromSeed did not return a PublicKey")
	}
	boxSeed := bytesRepeat(boxSeedByte, 32)
	boxPub, boxPriv := boxSeedKeypair(boxSeed)
	uid, tok := testsupport.Register(t, srv.HTTPURL, srv.InviteCode, name, pw, testsupport.RegisterOptions{
		ExtraFields: map[string]any{
			"box_pubkey":  stdEnc.EncodeToString(boxPub[:]),
			"sign_pubkey": stdEnc.EncodeToString(signPub),
		},
	})
	return fixtureUser{
		UserID:   uid,
		Token:    tok,
		BoxPub:   boxPub[:],
		BoxPriv:  boxPriv[:],
		SignPub:  signPub,
		SignSeed: signSeed,
	}
}

// bytesRepeat returns a length-n buffer where every byte == b. Used
// for deterministic per-fixture identities; tests that need uniqueness
// pass distinct b values.
func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// signMembership produces a §10-scope inviter_signature over the
// (channel, invitee, inviter) tuple. Mirror of auth.MembershipSignatureMessage.
func signMembership(
	t *testing.T,
	signPriv ed25519.PrivateKey,
	channelID, inviteeUserID, inviterUserID string,
	inviterSignPub, inviteeBoxPub, inviteeSignPub []byte,
	addedAt time.Time,
) []byte {
	t.Helper()
	msg := membershipSignatureMessage(
		channelID, inviteeUserID, inviterUserID,
		inviterSignPub,
		inviteeBoxPub, inviteeSignPub,
		addedAt,
	)
	return ed25519.Sign(signPriv, msg)
}

// dummyWrap returns a syntactically-valid (48/24/32-byte) WrapEntry
// owned by sender. Wrap content is opaque to the server in this PR;
// the L30/L39 checks pass on the byte-shape and pubkey ownership.
func dummyWrap(senderBoxPub []byte) map[string]any {
	wrapped := bytesRepeat(0x77, 48)
	nonce := bytesRepeat(0x55, 24)
	return map[string]any{
		"wrapped_key":       stdEnc.EncodeToString(wrapped),
		"sender_box_pubkey": stdEnc.EncodeToString(senderBoxPub),
		"nonce":             stdEnc.EncodeToString(nonce),
	}
}

// createPublicChannel posts /api/channels with is_public=true and
// returns the channel id. Public channels accept the legacy bare body
// (no membership / wraps) so the auto-fill path keeps working.
func createPublicChannel(t *testing.T, httpURL, bearer, name string) string {
	t.Helper()
	body := map[string]any{"name": name, "is_public": true}
	status, env, raw := testsupport.PostJSON(t, httpURL, "/api/channels", bearer, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /api/channels: status %d body %s", status, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v body=%s", err, raw)
	}
	return ch.ID
}

// createPrivateChannel posts /api/channels with the legacy private-bare
// shape. The current Phase-10 server tolerates this (membership-only
// row with NULL signature; lazy-wrap fills wraps later) so existing
// harnesses keep round-tripping.
func createPrivateChannel(t *testing.T, httpURL, bearer, name string) string {
	t.Helper()
	body := map[string]any{"name": name, "is_public": false}
	status, env, raw := testsupport.PostJSON(t, httpURL, "/api/channels", bearer, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /api/channels: status %d body %s", status, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v body=%s", err, raw)
	}
	return ch.ID
}

// listChannelIDs returns the ids of every channel the caller can see.
// Used to assert the L25 listing filter — non-members must not see a
// channel.
func listChannelIDs(t *testing.T, httpURL, bearer string) []string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpURL+"/api/channels", nil) //nolint:noctx
	if err != nil {
		t.Fatalf("new GET /api/channels: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/channels: status %d body %s", resp.StatusCode, raw)
	}
	var env testsupport.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v body %s", err, raw)
	}
	var data struct {
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode listing: %v body %s", err, raw)
	}
	out := make([]string, 0, len(data.Channels))
	for _, c := range data.Channels {
		out = append(out, c.ID)
	}
	return out
}

func deleteJSON(t *testing.T, httpURL, path, bearer string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, httpURL+path, nil) //nolint:noctx
	if err != nil {
		t.Fatalf("new DELETE %s: %v", path, err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func getJSON(t *testing.T, httpURL, path, bearer string) (int, testsupport.Envelope, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpURL+path, nil) //nolint:noctx
	if err != nil {
		t.Fatalf("new GET %s: %v", path, err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var env testsupport.Envelope
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &env)
	}
	return resp.StatusCode, env, raw
}

// TestRegistrationAutoJoinsGeneral — §9 + R1.2: a fresh registration
// inserts a channel_members row in #general so the new user sees it on
// GET /api/channels.
func TestRegistrationAutoJoinsGeneral(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	ids := listChannelIDs(t, srv.HTTPURL, alice.Token)
	if len(ids) != 1 {
		t.Fatalf("listing on fresh registration: got %d channels want 1 (#general); ids=%v", len(ids), ids)
	}
}

// TestPrivateChannelInviteFlow — REWRITTEN per #982 AC: the invite
// body now carries both a §10-signed membership block AND a
// root_key_wrap. Server validates the signature, the L30 sender
// pubkey ownership, and the L39 byte-lengths, then atomically
// inserts (channel_members, channel_keys) per L7.
func TestPrivateChannelInviteFlow(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "secret-"+testsupport.RandomSecret(t, 4))

	// Bob is not a member yet — listing must show only #general.
	bobIDs := listChannelIDs(t, srv.HTTPURL, bob.Token)
	for _, id := range bobIDs {
		if id == chID {
			t.Fatalf("L25 violation: bob sees private channel %q before invite", chID)
		}
	}

	added := time.Now().UTC().Truncate(time.Second)
	signPriv := ed25519.NewKeyFromSeed(alice.SignSeed)
	sig := signMembership(t, signPriv, chID, bob.UserID, alice.UserID,
		alice.SignPub, bob.BoxPub, bob.SignPub, added)

	body := map[string]any{
		"user_id": bob.UserID,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(bob.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(bob.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(sig),
		},
		"root_key_wrap": dummyWrap(alice.BoxPub),
	}

	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("invite: status %d body %s", status, raw)
	}
	bobIDs = listChannelIDs(t, srv.HTTPURL, bob.Token)
	sawIt := false
	for _, id := range bobIDs {
		if id == chID {
			sawIt = true
			break
		}
	}
	if !sawIt {
		t.Fatalf("post-invite listing: bob does not see channel %q (got %v)", chID, bobIDs)
	}
}

// TestPrivateInviteRejectsBadSignature — §10: a tampered signature
// returns 400 invalid_membership_signature; member + wrap are NOT
// inserted (L7 — the atomic transaction rolls back).
func TestPrivateInviteRejectsBadSignature(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "badsig-"+testsupport.RandomSecret(t, 4))

	added := time.Now().UTC().Truncate(time.Second)
	tamperedSig := bytesRepeat(0xEE, 64) // not a real Ed25519 signature

	body := map[string]any{
		"user_id": bob.UserID,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(bob.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(bob.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(tamperedSig),
		},
		"root_key_wrap": dummyWrap(alice.BoxPub),
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("bad signature invite: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "invalid_membership_signature" {
		t.Fatalf("expected invalid_membership_signature; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestPrivateInviteRejectsSenderPubkeyMismatch — L30: a wrap that
// claims sender_box_pubkey != caller's stored box_pubkey returns 400
// sender_pubkey_mismatch.
func TestPrivateInviteRejectsSenderPubkeyMismatch(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "l30-"+testsupport.RandomSecret(t, 4))

	added := time.Now().UTC().Truncate(time.Second)
	signPriv := ed25519.NewKeyFromSeed(alice.SignSeed)
	sig := signMembership(t, signPriv, chID, bob.UserID, alice.UserID,
		alice.SignPub, bob.BoxPub, bob.SignPub, added)

	otherBoxPub := bytesRepeat(0xFF, 32) // not alice's
	body := map[string]any{
		"user_id": bob.UserID,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(bob.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(bob.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(sig),
		},
		"root_key_wrap": dummyWrap(otherBoxPub),
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("L30 invite: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "sender_pubkey_mismatch" {
		t.Fatalf("expected sender_pubkey_mismatch; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestPrivateInviteRejectsBadWrapSize — L39: a wrap with the wrong
// byte length returns 400 wrap_size_invalid.
func TestPrivateInviteRejectsBadWrapSize(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "l39-"+testsupport.RandomSecret(t, 4))

	added := time.Now().UTC().Truncate(time.Second)
	signPriv := ed25519.NewKeyFromSeed(alice.SignSeed)
	sig := signMembership(t, signPriv, chID, bob.UserID, alice.UserID,
		alice.SignPub, bob.BoxPub, bob.SignPub, added)

	// 47-byte wrapped_key — one short of the 48-byte invariant.
	shortWrap := map[string]any{
		"wrapped_key":       stdEnc.EncodeToString(bytesRepeat(0x77, 47)),
		"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
		"nonce":             stdEnc.EncodeToString(bytesRepeat(0x55, 24)),
	}
	body := map[string]any{
		"user_id": bob.UserID,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(bob.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(bob.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(sig),
		},
		"root_key_wrap": shortWrap,
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("L39 invite: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "wrap_size_invalid" {
		t.Fatalf("expected wrap_size_invalid; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestKickRemovesMember — DELETE on the membership endpoint removes
// the row; the kicked user no longer sees the channel. Public-channel
// auto-fill path so no membership block is needed.
func TestKickRemovesMember(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	chID := createPublicChannel(t, srv.HTTPURL, alice.Token, "kick-"+testsupport.RandomSecret(t, 4))

	body := map[string]any{"user_id": bob.UserID}
	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("public invite: %d body %s", status, raw)
	}
	if !sees(listChannelIDs(t, srv.HTTPURL, bob.Token), chID) {
		t.Fatalf("bob should see channel after invite")
	}
	status, raw = deleteJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members/"+bob.UserID, alice.Token)
	if status != http.StatusNoContent {
		t.Fatalf("kick: %d body %s", status, raw)
	}
	if sees(listChannelIDs(t, srv.HTTPURL, bob.Token), chID) {
		t.Fatalf("post-kick: bob still sees channel %q", chID)
	}
}

// TestSelfLeaveSucceeds — a member can leave a non-#general channel.
func TestSelfLeaveSucceeds(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)
	chID := createPublicChannel(t, srv.HTTPURL, alice.Token, "leave-"+testsupport.RandomSecret(t, 4))
	if status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token,
		map[string]any{"user_id": bob.UserID}); status != http.StatusCreated {
		t.Fatalf("invite bob: %d body %s", status, raw)
	}
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members/"+bob.UserID, bob.Token)
	if status != http.StatusNoContent {
		t.Fatalf("self-leave: %d body %s", status, raw)
	}
}

// TestSelfLeaveOnGeneralIs403 — L8: #general membership is immutable.
func TestSelfLeaveOnGeneralIs403(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	ids := listChannelIDs(t, srv.HTTPURL, alice.Token)
	if len(ids) != 1 {
		t.Fatalf("expected #general only on fresh listing; got %v", ids)
	}
	generalID := ids[0]
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+generalID+"/members/"+alice.UserID, alice.Token)
	if status != http.StatusForbidden {
		t.Fatalf("self-leave on #general: status %d body %s want 403", status, raw)
	}
}

// TestKickOnGeneralIs403 — L8: also rejects kick attempts on #general.
func TestKickOnGeneralIs403(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)
	ids := listChannelIDs(t, srv.HTTPURL, alice.Token)
	generalID := ids[0]
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+generalID+"/members/"+bob.UserID, alice.Token)
	if status != http.StatusForbidden {
		t.Fatalf("kick on #general: status %d body %s want 403", status, raw)
	}
}

// TestListMembersRequiresMembership — non-members get 403.
func TestListMembersRequiresMembership(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)
	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "private-"+testsupport.RandomSecret(t, 4))

	status, _, _ := getJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", bob.Token)
	if status != http.StatusForbidden {
		t.Fatalf("non-member GET /members: status %d want 403", status)
	}
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token)
	if status != http.StatusOK {
		t.Fatalf("member GET /members: %d body %s", status, raw)
	}
	var data struct {
		Members []map[string]any `json:"members"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode: %v body %s", err, raw)
	}
	if len(data.Members) != 1 {
		t.Fatalf("creator-bootstrap: got %d members want 1", len(data.Members))
	}
}

// TestPrivateChannelInviteWithoutWrapRejected — REWRITTEN per #982 AC.
// Posting a private-channel invite without root_key_wrap returns 400
// (was: TestPrivateChannelInviteWithoutMembershipRejected; the "no
// membership" path is now also a 400 but the ROOT-cause expectation
// flips — clients must supply both blocks).
func TestPrivateChannelInviteWithoutWrapRejected(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)
	chID := createPrivateChannel(t, srv.HTTPURL, alice.Token, "nowrap-"+testsupport.RandomSecret(t, 4))
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", alice.Token,
		map[string]any{"user_id": bob.UserID})
	if status != http.StatusBadRequest {
		t.Fatalf("invite without wrap on private channel: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code == "" {
		t.Fatalf("expected error envelope; got %s", raw)
	}
}

func sees(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func codeOrEmpty(e *testsupport.EnvelopeError) string {
	if e == nil {
		return ""
	}
	return e.Code
}

// silence unused-import warnings if any path is removed in
// follow-on edits.
var _ = bytes.Equal
