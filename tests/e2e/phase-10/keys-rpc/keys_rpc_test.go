// Package keys_rpc_e2e_test exercises the Phase-10 standalone keys-RPC
// (#984): POST /api/channels/{id}/keys (bootstrap | fill-in | rotation
// modes) plus GET /api/channels/{id}/members/wraps-needed. All
// assertions are black-box against the production server binary
// (decision-log L27 harness).
package keys_rpc_e2e_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/curve25519"

	"hackathon/tests/e2e/internal/testsupport"
)

var stdEnc = base64.StdEncoding

const membershipSignatureScopePrefix = "snakd-mship-v1:"

type fixtureUser struct {
	UserID   string
	Token    string
	BoxPub   []byte
	SignPub  ed25519.PublicKey
	SignSeed []byte
}

func freshULID(t *testing.T) string {
	t.Helper()
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		t.Fatalf("ulid.New: %v", err)
	}
	return id.String()
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func boxSeedKeypair(t *testing.T, seed []byte) ([32]byte, [32]byte) {
	t.Helper()
	if len(seed) != 32 {
		t.Fatal("boxSeedKeypair: seed must be 32 bytes")
	}
	h := sha512.Sum512(seed)
	var priv [32]byte
	copy(priv[:], h[:32])
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pubBytes, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatalf("boxSeedKeypair: %v", err)
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
	boxPub, _ := boxSeedKeypair(t, boxSeed)
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

// getJSON is a tiny GET helper local to this package — testsupport
// only exposes PostJSON today and adding a sibling helper would touch
// out-of-footprint code (#984's footprint stops at tests/e2e/phase-10/
// keys-rpc/).
func getJSON(t *testing.T, httpURL, path, bearer string) (int, testsupport.Envelope, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpURL+path, nil) //nolint:noctx // loopback test helper
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	var env testsupport.Envelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope: %v body=%s", err, raw)
		}
	}
	return resp.StatusCode, env, raw
}

// makePrivateChannel registers `creator` and creates a fresh PRIVATE
// channel via the §10 atomic-bootstrap path so a pre-existing
// channel_keys row (gen 1) is in place. Returns the channel id; the
// caller can then add another member to drive the fill-in / rotation
// arms of the keys-RPC.
func makePrivateChannel(t *testing.T, srv *testsupport.Server, creator fixtureUser) string {
	t.Helper()
	channelID := freshULID(t)
	added := time.Now().UTC().Truncate(time.Second)
	wrapped, nonce := dummyWrapBytes()
	signPriv := ed25519.NewKeyFromSeed(creator.SignSeed)
	sig := ed25519.Sign(signPriv, membershipSignatureMessage(
		channelID, creator.UserID, creator.UserID,
		creator.SignPub, creator.BoxPub, creator.SignPub, added,
	))
	body := map[string]any{
		"channel_id": channelID,
		"name":       "kr-" + testsupport.RandomSecret(t, 4),
		"is_public":  false,
		"membership": map[string]any{
			"inviter_user_id":     creator.UserID,
			"inviter_sign_pubkey": stdEnc.EncodeToString(creator.SignPub),
			"invitee_box_pubkey":  stdEnc.EncodeToString(creator.BoxPub),
			"invitee_sign_pubkey": stdEnc.EncodeToString(creator.SignPub),
			"added_at":            added.Format(time.RFC3339),
			"inviter_signature":   stdEnc.EncodeToString(sig),
		},
		"root_key_wraps": []map[string]any{
			{
				"recipient_user_id": creator.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(creator.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", creator.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("create private channel: status %d body=%s", status, raw)
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode created channel: %v body=%s", err, raw)
	}
	if ch.ID != channelID {
		t.Fatalf("channel id mismatch: got %s want %s", ch.ID, channelID)
	}
	return channelID
}

// TestKeysRPCInvalidGenerationRejected — the keys-RPC server picks a
// mode based on generation_id vs. max(channel_keys.generation_id).
// Anything else (gen 0, gen MaxGen+5, etc.) returns 400
// invalid_generation per specs/plans/phase-10/keys.md.
func TestKeysRPCInvalidGenerationRejected(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	channelID := makePrivateChannel(t, srv, alice)

	wrapped, nonce := dummyWrapBytes()
	body := map[string]any{
		"generation_id": 17, // far past MaxGen=1
		"wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	path := "/api/channels/" + channelID + "/keys"
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, path, alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid_generation: status %d body=%s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "invalid_generation" {
		t.Fatalf("expected invalid_generation; got %v body=%s", env.Error, raw)
	}
}

// TestKeysRPCBootstrapFirstUser — the first user of a #general /
// public channel that has no wraps yet posts gen=1 with one
// wrap-to-self. specs/plans/phase-10/keys.md "Bootstrap mode" steps 1-7.
//
// We assert against `#general` because the seed step always plants
// it as is_public=TRUE and registration auto-adds every user to it
// without inserting any channel_keys row. After bootstrap the
// wraps-needed response must report empty `missing` for the
// bootstrapping user.
func TestKeysRPCBootstrapFirstUser(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)

	// Resolve #general's id from the channel listing.
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/channels", alice.Token)
	if status != http.StatusOK {
		t.Fatalf("/api/channels: status %d body=%s", status, raw)
	}
	var listResp struct {
		Channels []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			IsPublic *bool  `json:"is_public"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &listResp); err != nil {
		t.Fatalf("decode listing: %v body=%s", err, raw)
	}
	var generalID string
	for _, c := range listResp.Channels {
		if c.Name == "general" {
			generalID = c.ID
		}
	}
	if generalID == "" {
		t.Fatal("no #general channel in listing — seed broken?")
	}

	wrapped, nonce := dummyWrapBytes()
	body := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	path := "/api/channels/" + generalID + "/keys"
	status, env, raw = testsupport.PostJSON(t, srv.HTTPURL, path, alice.Token, body)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap: status %d body=%s want 201", status, raw)
	}
	var resp struct {
		Mode         string `json:"mode"`
		GenerationID int64  `json:"generation_id"`
		Inserted     int    `json:"inserted"`
	}
	if err := json.Unmarshal(*env.Data, &resp); err != nil {
		t.Fatalf("decode resp: %v body=%s", err, raw)
	}
	if resp.Mode != "bootstrap" {
		t.Fatalf("mode = %q want bootstrap; body=%s", resp.Mode, raw)
	}
	if resp.GenerationID != 1 || resp.Inserted != 1 {
		t.Fatalf("unexpected resp: %+v body=%s", resp, raw)
	}

	// After bootstrap, wraps-needed reports empty missing for alice
	// (the only member with a current-gen wrap).
	wnPath := "/api/channels/" + generalID + "/members/wraps-needed"
	status, env, raw = getJSON(t, srv.HTTPURL, wnPath, alice.Token)
	if status != http.StatusOK {
		t.Fatalf("wraps-needed: status %d body=%s", status, raw)
	}
	var wn struct {
		ChannelID string           `json:"channel_id"`
		IsPublic  bool             `json:"is_public"`
		Missing   []map[string]any `json:"missing"`
	}
	if err := json.Unmarshal(*env.Data, &wn); err != nil {
		t.Fatalf("decode wraps-needed: %v body=%s", err, raw)
	}
	if wn.ChannelID != generalID {
		t.Fatalf("channel_id mismatch: %q want %q", wn.ChannelID, generalID)
	}
	if !wn.IsPublic {
		t.Fatalf("expected is_public=true for #general; body=%s", raw)
	}
	if len(wn.Missing) != 0 {
		t.Fatalf("expected empty missing after bootstrap; got %v body=%s", wn.Missing, raw)
	}
}

// TestKeysRPCWrapSizeInvalid — L39: a wrap with a 47-byte wrapped_key
// returns 400 wrap_size_invalid. Sanity-checks that the keys-RPC
// reuses the shared L39 byte-length validation.
func TestKeysRPCWrapSizeInvalid(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	channelID := makePrivateChannel(t, srv, alice)

	body := map[string]any{
		"generation_id": 1, // == MaxGen for this freshly-bootstrapped channel
		"wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(bytesRepeat(0x77, 47)),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(bytesRepeat(0x55, 24)),
			},
		},
	}
	path := "/api/channels/" + channelID + "/keys"
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, path, alice.Token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("L39 wrap_size_invalid: status %d body=%s want 400", status, raw)
	}
	if env.Error == nil || env.Error.Code != "wrap_size_invalid" {
		t.Fatalf("expected wrap_size_invalid; got %v body=%s", env.Error, raw)
	}
}

// TestKeysRPCFillInDelivers — fill-in mode: invite a second member
// to a channel under the public-channel auto-fill path (no wrap row
// created), then call wraps-needed and post a fill-in wrap. Server
// should accept (mode=fill-in, inserted=1) and the recipient wrap
// row should now exist.
//
// We use a fresh public channel (not #general) so we know the only
// missing row is bob's: alice creates the channel via legacy bootstrap
// (no §10 block — wraps-omitted public channel), then bootstraps
// gen=1 for herself, then auto-adds bob via the public-channel
// invite path. Bob's row is missing a wrap; alice supplies it.
func TestKeysRPCFillInDelivers(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	// Alice creates a fresh PRIVATE channel via §10 atomic-bootstrap.
	// Gen=1 wrap exists for alice. Then alice invites bob (private
	// path supplies the §10 inviter-signature + a fresh wrap for bob).
	// To exercise the FILL-IN arm we need a member without a wrap —
	// the cleanest path is to use the public-channel auto-fill body
	// (membership block + root_key_wrap omitted), which inserts the
	// row without a wrap. That requires is_public=TRUE.
	//
	// So: alice creates a PUBLIC channel via the legacy bootstrap
	// (no membership block), then bootstraps gen=1 for herself via
	// the keys-RPC, then invites bob via the auto-fill body. Bob's
	// row exists at gen=1 without a wrap → wraps-needed surfaces
	// it → alice posts the fill-in.
	createBody := map[string]any{
		"name":      "fillin-" + testsupport.RandomSecret(t, 4),
		"is_public": true,
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", alice.Token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("create public channel: status %d body=%s", status, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &created); err != nil {
		t.Fatalf("decode created: %v body=%s", err, raw)
	}
	channelID := created.ID

	// Bootstrap gen=1 for alice via the keys-RPC.
	wrapped, nonce := dummyWrapBytes()
	bootstrapBody := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	keysPath := "/api/channels/" + channelID + "/keys"
	status, _, raw = testsupport.PostJSON(t, srv.HTTPURL, keysPath, alice.Token, bootstrapBody)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap public channel: status %d body=%s", status, raw)
	}

	// Add bob via the public-channel auto-fill path (no membership +
	// no wrap → server creates a NULL-signature row, no wrap insert).
	invitePath := "/api/channels/" + channelID + "/members"
	status, _, raw = testsupport.PostJSON(t, srv.HTTPURL, invitePath, alice.Token, map[string]any{
		"user_id": bob.UserID,
	})
	if status != http.StatusCreated {
		t.Fatalf("invite bob (auto-fill): status %d body=%s", status, raw)
	}

	// Alice queries wraps-needed → should surface bob (and only bob).
	wnPath := "/api/channels/" + channelID + "/members/wraps-needed"
	status, env, raw = getJSON(t, srv.HTTPURL, wnPath, alice.Token)
	if status != http.StatusOK {
		t.Fatalf("wraps-needed: status %d body=%s", status, raw)
	}
	var wn struct {
		Missing []struct {
			UserID       string `json:"user_id"`
			GenerationID int64  `json:"generation_id"`
			Membership   struct {
				InviterUserID string `json:"inviter_user_id"`
			} `json:"membership"`
		} `json:"missing"`
	}
	if err := json.Unmarshal(*env.Data, &wn); err != nil {
		t.Fatalf("decode wraps-needed: %v body=%s", err, raw)
	}
	if len(wn.Missing) != 1 {
		t.Fatalf("expected 1 missing wrap; got %d body=%s", len(wn.Missing), raw)
	}
	if wn.Missing[0].UserID != bob.UserID {
		t.Fatalf("wrong missing user: got %s want %s body=%s",
			wn.Missing[0].UserID, bob.UserID, raw)
	}
	if wn.Missing[0].GenerationID != 1 {
		t.Fatalf("wrong generation: got %d want 1 body=%s",
			wn.Missing[0].GenerationID, raw)
	}

	// Alice posts the fill-in wrap for bob.
	fillBody := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	status, env, raw = testsupport.PostJSON(t, srv.HTTPURL, keysPath, alice.Token, fillBody)
	if status != http.StatusCreated {
		t.Fatalf("fill-in: status %d body=%s want 201", status, raw)
	}
	var fillResp struct {
		Mode      string `json:"mode"`
		Recipient string `json:"recipient"`
	}
	if err := json.Unmarshal(*env.Data, &fillResp); err != nil {
		t.Fatalf("decode fill-in resp: %v body=%s", err, raw)
	}
	if fillResp.Mode != "fill-in" {
		t.Fatalf("mode = %q want fill-in; body=%s", fillResp.Mode, raw)
	}
	if fillResp.Recipient != bob.UserID {
		t.Fatalf("recipient = %q want %s; body=%s", fillResp.Recipient, bob.UserID, raw)
	}

	// wraps-needed should now report empty missing.
	status, env, raw = getJSON(t, srv.HTTPURL, wnPath, alice.Token)
	if status != http.StatusOK {
		t.Fatalf("post-fill wraps-needed: status %d body=%s", status, raw)
	}
	if err := json.Unmarshal(*env.Data, &wn); err != nil {
		t.Fatalf("decode post-fill wraps-needed: %v body=%s", err, raw)
	}
	if len(wn.Missing) != 0 {
		t.Fatalf("expected empty missing after fill-in; got %v body=%s", wn.Missing, raw)
	}
}

// TestKeysRPCFillInRaceLoss — atomic insert of the same fill-in
// recipient by two callers in the same generation: first wins (201);
// second sees the channel_keys PRIMARY KEY violation and gets 409
// conflict. Important so a slow second caller doesn't see "ok"
// silently after the first caller's POST already landed.
func TestKeysRPCFillInRaceLoss(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	bob := registerFixture(t, srv, "bob", 0xDD, 0xBC)

	createBody := map[string]any{
		"name":      "race-" + testsupport.RandomSecret(t, 4),
		"is_public": true,
	}
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels", alice.Token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(*env.Data, &created); err != nil {
		t.Fatalf("decode created: %v body=%s", err, raw)
	}
	channelID := created.ID

	wrapped, nonce := dummyWrapBytes()
	keysPath := "/api/channels/" + channelID + "/keys"
	bootstrapBody := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": alice.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	if status, _, _ := testsupport.PostJSON(t, srv.HTTPURL, keysPath, alice.Token, bootstrapBody); status != http.StatusCreated {
		t.Fatalf("bootstrap: status %d", status)
	}
	invitePath := "/api/channels/" + channelID + "/members"
	if status, _, _ := testsupport.PostJSON(t, srv.HTTPURL, invitePath, alice.Token, map[string]any{
		"user_id": bob.UserID,
	}); status != http.StatusCreated {
		t.Fatalf("auto-invite bob: status %d", status)
	}

	fillBody := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": bob.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(alice.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	if status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, keysPath, alice.Token, fillBody); status != http.StatusCreated {
		t.Fatalf("first fill-in: status %d body=%s", status, raw)
	}
	status, env, raw = testsupport.PostJSON(t, srv.HTTPURL, keysPath, alice.Token, fillBody)
	if status != http.StatusConflict {
		t.Fatalf("second fill-in: status %d body=%s want 409", status, raw)
	}
	if env.Error == nil || env.Error.Code != "conflict" {
		t.Fatalf("expected conflict; got %v body=%s", env.Error, raw)
	}
}

// TestKeysRPCRequiresMember — a non-member calling
// POST /api/channels/{id}/keys is rejected with 403.
func TestKeysRPCRequiresMember(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	mallory := registerFixture(t, srv, "mallory", 0xEE, 0xCD)
	channelID := makePrivateChannel(t, srv, alice)

	wrapped, nonce := dummyWrapBytes()
	body := map[string]any{
		"generation_id": 1,
		"wraps": []map[string]any{
			{
				"recipient_user_id": mallory.UserID,
				"wrapped_key":       stdEnc.EncodeToString(wrapped),
				"sender_box_pubkey": stdEnc.EncodeToString(mallory.BoxPub),
				"nonce":             stdEnc.EncodeToString(nonce),
			},
		},
	}
	path := "/api/channels/" + channelID + "/keys"
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, path, mallory.Token, body)
	if status != http.StatusForbidden {
		t.Fatalf("non-member: status %d body=%s want 403", status, raw)
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("expected forbidden; got %v body=%s", env.Error, raw)
	}
}

// TestWrapsNeededRequiresMember — a non-member calling
// GET /api/channels/{id}/members/wraps-needed is rejected with 403.
func TestWrapsNeededRequiresMember(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	alice := registerFixture(t, srv, "alice", 0xCC, 0xAB)
	mallory := registerFixture(t, srv, "mallory", 0xEE, 0xCD)
	channelID := makePrivateChannel(t, srv, alice)

	path := "/api/channels/" + channelID + "/members/wraps-needed"
	status, env, raw := getJSON(t, srv.HTTPURL, path, mallory.Token)
	if status != http.StatusForbidden {
		t.Fatalf("non-member: status %d body=%s want 403", status, raw)
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("expected forbidden; got %v body=%s", env.Error, raw)
	}
}
