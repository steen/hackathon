// Package key_wrapping_e2e_test exercises the Phase-10 atomic key-wrap
// surface (#982): self-bootstrap on POST /api/channels, atomic 2-wrap
// 201 path on POST /api/dms, the L6 idempotent-re-supply rejection on
// the 200 path, the L30 sender-pubkey check on every wrap-insert
// path, and the L39 byte-length invariants.
//
// All assertions are black-box against the production server binary
// (decision-log L27 harness).
package key_wrapping_e2e_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/curve25519"

	"hackathon/tests/e2e/internal/testsupport"
)

// freshULID returns a fresh Crockford-base32 26-char ULID for the
// caller-supplied channel_id field on the §10 atomic-bootstrap path.
func freshULID(t *testing.T) string {
	t.Helper()
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		t.Fatalf("ulid.New: %v", err)
	}
	return id.String()
}

var stdEnc = base64.StdEncoding

const membershipSignatureScopePrefix = "snakd-mship-v1:"

type fixtureUser struct {
	UserID   string
	Token    string
	BoxPub   []byte
	BoxPriv  []byte
	SignPub  ed25519.PublicKey
	SignSeed []byte
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

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

func membershipSignatureMessage(
	channelID, userID, inviterUserID string,
	inviterSignPubkey, inviteeBoxPubkey, inviteeSignPubkey []byte,
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

func dummyWrapBytes() (wrapped, nonce []byte) {
	return bytesRepeat(0x77, 48), bytesRepeat(0x55, 24)
}

// TestChannelCreateAtomicSelfBootstrap — AC: POST /api/channels with
// the §10-signed MembershipBlock + a 1-entry root_key_wraps inserts
// channel + member + wrap atomically. The caller picks the channel
// id (signature is bound to it under §10) and the server enforces
// uniqueness inside the transaction.
func TestChannelCreateAtomicSelfBootstrap(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	signPriv := ed25519.NewKeyFromSeed(alice.SignSeed)

	channelID := freshULID(t)
	added := time.Now().UTC().Truncate(time.Second)
	wrapped, nonce := dummyWrapBytes()
	sig := ed25519.Sign(signPriv, membershipSignatureMessage(
		channelID, alice.UserID, alice.UserID,
		alice.SignPub, alice.BoxPub, alice.SignPub, added,
	))
	body := map[string]any{
		"channel_id": channelID,
		"name":       "atomic-" + testsupport.RandomSecret(t, 4),
		"is_public":  false,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(alice.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(sig),
		},
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", alice.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("atomic bootstrap: status %d body %s", status, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v body %s", err, raw)
	}
	if ch.ID != channelID {
		t.Fatalf("server picked id %q want caller-supplied %q", ch.ID, channelID)
	}
}

// TestChannelCreateRejectsBadSelfSignature — §10: a tampered
// inviter_signature returns 400 invalid_membership_signature; the
// channel + member + wrap rows are NOT inserted (transaction rolls
// back).
func TestChannelCreateRejectsBadSelfSignature(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)

	channelID := freshULID(t)
	added := time.Now().UTC().Truncate(time.Second)
	wrapped, nonce := dummyWrapBytes()
	tampered := bytesRepeat(0xEE, 64)
	body := map[string]any{
		"channel_id": channelID,
		"name":       "badsig-" + testsupport.RandomSecret(t, 4),
		"is_public":  false,
		"membership": map[string]any{
			"inviter_user_id":     alice.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(alice.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(alice.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(tampered),
		},
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("bad sig bootstrap: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "invalid_membership_signature" {
		t.Fatalf("expected invalid_membership_signature; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestDMCreateAtomicWraps — POST /api/dms 201 path with two wraps
// inserts the conversation row + both dm_conversation_keys rows
// atomically.
func TestDMCreateAtomicWraps(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	wrapped, nonce := dummyWrapBytes()
	body := map[string]any{
		"peer_user_id": bob.UserID,
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", alice.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /api/dms with 2 wraps: status %d body %s", status, raw)
	}
	var conv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &conv); err != nil {
		t.Fatalf("decode conversation: %v body=%s", err, raw)
	}
	if conv.ID == "" {
		t.Fatalf("empty conversation id; body=%s", raw)
	}
}

// TestDMIdempotentReSupplyReject — L6 + L12: POST /api/dms 200 path
// with non-empty root_key_wraps returns 409 wraps_already_set.
func TestDMIdempotentReSupplyReject(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	// First call: legacy bare body — 201 + conversation row, no wraps.
	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", alice.Token,
		map[string]any{"peer_user_id": bob.UserID})
	if status != http.StatusCreated {
		t.Fatalf("first POST /api/dms: status %d body %s", status, raw)
	}

	// Second call: 200 idempotent path with wraps re-supplied — 409.
	wrapped, nonce := dummyWrapBytes()
	body := map[string]any{
		"peer_user_id": bob.UserID,
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", alice.Token, body)
	if status != http.StatusConflict {
		t.Fatalf("L6 re-supply: status %d body %s want 409", status, raw)
	}
	if env.Error == nil || env.Error.Code != "wraps_already_set" {
		t.Fatalf("expected wraps_already_set; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestDMCreateRejectsBadWrapSize — L39: a wrap with 47-byte
// wrapped_key returns 400 wrap_size_invalid.
func TestDMCreateRejectsBadWrapSize(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	body := map[string]any{
		"peer_user_id": bob.UserID,
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(bytesRepeat(0x77, 47)),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(bytesRepeat(0x55, 24)),
			},
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(bytesRepeat(0x77, 47)),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(bytesRepeat(0x55, 24)),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("L39 DM: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "wrap_size_invalid" {
		t.Fatalf("expected wrap_size_invalid; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

// TestDMCreateRejectsSenderPubkeyMismatch — L30: a wrap that claims
// sender_box_pubkey != caller's stored box_pubkey returns 400
// sender_pubkey_mismatch.
func TestDMCreateRejectsSenderPubkeyMismatch(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	wrapped, nonce := dummyWrapBytes()
	otherBoxPub := bytesRepeat(0xFF, 32) // not alice's
	body := map[string]any{
		"peer_user_id": bob.UserID,
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(otherBoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(otherBoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/dms", alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("L30 DM: status %d body %s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "sender_pubkey_mismatch" {
		t.Fatalf("expected sender_pubkey_mismatch; got %s body=%s", codeOrEmpty(env.Error), raw)
	}
}

func codeOrEmpty(e *testsupport.EnvelopeError) string {
	if e == nil {
		return ""
	}
	return e.Code
}
