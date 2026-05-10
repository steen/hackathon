// Package channel_membership_e2e_test exercises the Phase-10 channel
// membership surface end-to-end against the production server binary
// (decision-log L27 black-box harness): invite, kick, self-leave,
// #general immutability (L8), is_public auto-add at registration (§9 +
// R1.2), and the L25 listing filter that hides non-member channels.
//
// Out of scope (covered elsewhere): key-wrap on invite (#982 / #984),
// inviter-signature crypto verify (#984 — this PR validates byte-shape
// only).
package channel_membership_e2e_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/testsupport"
)

var stdEnc = base64.StdEncoding

func registerUser(t *testing.T, srv *testsupport.Server, prefix string) (userID, token string) {
	t.Helper()
	name := prefix + "-" + testsupport.RandomSecret(t, 4)
	pw := testsupport.RandomSecret(t, 12)
	return testsupport.Register(t, srv.HTTPURL, srv.InviteCode, name, pw)
}

// createChannel posts /api/channels with the given is_public toggle and
// returns the channel id. The caller's bearer becomes the only initial
// member (§10 self-bootstrap carve-out).
func createChannel(t *testing.T, httpURL, bearer, name string, isPublic bool) string {
	t.Helper()
	body := map[string]any{"name": name, "is_public": isPublic}
	status, env, raw := testsupport.PostJSON(t, httpURL, "/api/channels", bearer, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /api/channels: status %d body %s", status, raw)
	}
	var ch struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		IsPublic *bool  `json:"is_public"`
	}
	if err := json.Unmarshal(*env.Data, &ch); err != nil {
		t.Fatalf("decode channel: %v body=%s", err, raw)
	}
	if ch.IsPublic == nil || *ch.IsPublic != isPublic {
		t.Fatalf("is_public on response: got %v want %v (body=%s)", ch.IsPublic, isPublic, raw)
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
			ID       string `json:"id"`
			Name     string `json:"name"`
			IsPublic *bool  `json:"is_public"`
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

// deleteJSON issues a DELETE — testsupport does not export one yet so
// keep the helper local. Returns status + raw body.
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

// getJSON wraps GET with a body returned for easy inspection.
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
	_, tok := registerUser(t, srv, "alice")
	ids := listChannelIDs(t, srv.HTTPURL, tok)
	if len(ids) != 1 {
		t.Fatalf("listing on fresh registration: got %d channels want 1 (#general); ids=%v", len(ids), ids)
	}
}

// TestPrivateChannelInviteFlow — POST /api/channels with is_public=false
// makes the creator the sole member. A second user does NOT see the
// channel until invited; after invite, both see it.
func TestPrivateChannelInviteFlow(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	bobID, bobTok := registerUser(t, srv, "bob")

	chID := createChannel(t, srv.HTTPURL, aliceTok, "secret-"+testsupport.RandomSecret(t, 4), false)

	// Bob is not a member yet — listing must show only #general.
	bobIDs := listChannelIDs(t, srv.HTTPURL, bobTok)
	for _, id := range bobIDs {
		if id == chID {
			t.Fatalf("L25 violation: bob sees private channel %q before invite", chID)
		}
	}

	// Alice invites bob with a well-shaped membership block. The
	// signature is a 64-byte placeholder — the wrap loop in #984 will
	// verify it; this PR validates byte-shape only.
	body := map[string]any{
		"user_id": bobID,
		"membership": map[string]any{
			"inviter_user_id":     "", // re-set after we look up alice id
			"inviter_sign_pubkey": b64Pubkey(),
			"invitee_box_pubkey":  b64Pubkey(),
			"invitee_sign_pubkey": b64Pubkey(),
			"added_at":            time.Now().UTC().Format(time.RFC3339),
			"inviter_signature":   b64Signature(),
		},
	}
	// Resolve alice id via /api/auth/me.
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/auth/me", aliceTok)
	if status != http.StatusOK {
		t.Fatalf("GET /me: %d body %s", status, raw)
	}
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &me); err != nil {
		t.Fatalf("decode me: %v body %s", err, raw)
	}
	mb := body["membership"].(map[string]any)
	mb["inviter_user_id"] = me.User.ID

	status, _, raw = testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", aliceTok, body)
	if status != http.StatusCreated {
		t.Fatalf("invite: status %d body %s", status, raw)
	}
	bobIDs = listChannelIDs(t, srv.HTTPURL, bobTok)
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

// TestKickRemovesMember — DELETE on the membership endpoint removes
// the row; the kicked user no longer sees the channel.
func TestKickRemovesMember(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	bobID, bobTok := registerUser(t, srv, "bob")

	chID := createChannel(t, srv.HTTPURL, aliceTok, "kick-"+testsupport.RandomSecret(t, 4), true)

	// Alice invites bob (public-channel — NULL signature is accepted).
	body := map[string]any{"user_id": bobID}
	status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", aliceTok, body)
	if status != http.StatusCreated {
		t.Fatalf("public invite: %d body %s", status, raw)
	}
	if !sees(listChannelIDs(t, srv.HTTPURL, bobTok), chID) {
		t.Fatalf("bob should see channel after invite")
	}
	status, raw = deleteJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members/"+bobID, aliceTok)
	if status != http.StatusNoContent {
		t.Fatalf("kick: %d body %s", status, raw)
	}
	if sees(listChannelIDs(t, srv.HTTPURL, bobTok), chID) {
		t.Fatalf("post-kick: bob still sees channel %q", chID)
	}
}

// TestSelfLeaveSucceeds — a member can leave a non-#general channel.
func TestSelfLeaveSucceeds(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	bobID, bobTok := registerUser(t, srv, "bob")
	chID := createChannel(t, srv.HTTPURL, aliceTok, "leave-"+testsupport.RandomSecret(t, 4), true)
	if status, _, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", aliceTok,
		map[string]any{"user_id": bobID}); status != http.StatusCreated {
		t.Fatalf("invite bob: %d body %s", status, raw)
	}
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members/"+bobID, bobTok)
	if status != http.StatusNoContent {
		t.Fatalf("self-leave: %d body %s", status, raw)
	}
}

// TestSelfLeaveOnGeneralIs403 — L8: #general membership is immutable.
func TestSelfLeaveOnGeneralIs403(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	aliceID, aliceTok := registerUser(t, srv, "alice")
	// Resolve #general id via the listing.
	ids := listChannelIDs(t, srv.HTTPURL, aliceTok)
	if len(ids) != 1 {
		t.Fatalf("expected #general only on fresh listing; got %v", ids)
	}
	generalID := ids[0]
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+generalID+"/members/"+aliceID, aliceTok)
	if status != http.StatusForbidden {
		t.Fatalf("self-leave on #general: status %d body %s want 403", status, raw)
	}
}

// TestKickOnGeneralIs403 — L8: also rejects kick attempts on #general.
func TestKickOnGeneralIs403(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	bobID, _ := registerUser(t, srv, "bob")
	ids := listChannelIDs(t, srv.HTTPURL, aliceTok)
	generalID := ids[0]
	status, raw := deleteJSON(t, srv.HTTPURL, "/api/channels/"+generalID+"/members/"+bobID, aliceTok)
	if status != http.StatusForbidden {
		t.Fatalf("kick on #general: status %d body %s want 403", status, raw)
	}
}

// TestListMembersRequiresMembership — non-members get 403.
func TestListMembersRequiresMembership(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	_, bobTok := registerUser(t, srv, "bob")
	chID := createChannel(t, srv.HTTPURL, aliceTok, "private-"+testsupport.RandomSecret(t, 4), false)

	status, _, _ := getJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", bobTok)
	if status != http.StatusForbidden {
		t.Fatalf("non-member GET /members: status %d want 403", status)
	}
	// Alice (member) gets 200.
	status, env, raw := getJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", aliceTok)
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

// TestPrivateChannelInviteWithoutMembershipRejected — L33 enforcement:
// inserting a NULL-signature row on a private channel returns 400.
func TestPrivateChannelInviteWithoutMembershipRejected(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	_, aliceTok := registerUser(t, srv, "alice")
	bobID, _ := registerUser(t, srv, "bob")
	chID := createChannel(t, srv.HTTPURL, aliceTok, "l33-"+testsupport.RandomSecret(t, 4), false)
	status, env, raw := testsupport.PostJSON(t, srv.HTTPURL, "/api/channels/"+chID+"/members", aliceTok,
		map[string]any{"user_id": bobID})
	if status != http.StatusBadRequest {
		t.Fatalf("invite without membership block on private channel: status %d body %s want 400", status, raw)
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

func b64Pubkey() string {
	// 32 raw bytes base64-encoded — equivalent to "AA"*32 in StdEncoding.
	b := bytes.Repeat([]byte{0x01}, 32)
	return base64Encode(b)
}

func b64Signature() string {
	// 64 raw bytes base64-encoded — placeholder; this PR validates byte-
	// length only, the signature crypto verify lands with #984.
	b := bytes.Repeat([]byte{0x02}, 64)
	return base64Encode(b)
}

func base64Encode(b []byte) string {
	return stdEnc.EncodeToString(b)
}
