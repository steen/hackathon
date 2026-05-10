// Package registration_auto_add_e2e_test pins the §9 + R1.2 decision
// that the public-channel carve-out auto-add at registration uses
// self-invite semantics: `inviter_user_id == user_id` and the row's
// `inviter_signature` is NULL (omitted on the wire).
//
// Issue #996 picks self-invite over system-invite (sentinel user row).
// The corresponding rationale lives in
// specs/plans/phase-10/membership.md under "#general (the seeded
// baseline) → Decision — self-invite (not system-invite)". Any future
// flip to system-invite must update that doc section AND this test in
// the same PR.
//
// Out of scope: this PR does not assert key-wrap behaviour or
// inviter-signature crypto verify (#984). Byte-shape only.
package registration_auto_add_e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/testsupport"
)

// memberRow mirrors the relevant subset of the
// /api/channels/{id}/members wire shape (apps/server/internal/http/
// members_handlers.go memberWire). The full struct carries pubkey
// blobs and `added_at`; only the fields this test asserts on are
// declared so a future field addition does not force an unrelated
// edit here.
type memberRow struct {
	UserID           string `json:"user_id"`
	InviterUserID    string `json:"inviter_user_id"`
	InviterSignature string `json:"inviter_signature,omitempty"`
}

// TestGeneralAutoAddIsSelfInvite registers two fresh users and asserts
// that each user's row in `#general` carries `inviter_user_id == user_id`
// and an absent `inviter_signature` (NULL on the wire). Pins the
// self-invite decision against the alternative system-invite shape.
func TestGeneralAutoAddIsSelfInvite(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	aliceID, aliceTok := registerUser(t, srv, "alice")
	bobID, _ := registerUser(t, srv, "bob")

	generalID := singleChannelID(t, srv.HTTPURL, aliceTok)

	members := listMembers(t, srv.HTTPURL, generalID, aliceTok)
	if len(members) != 2 {
		t.Fatalf("expected 2 #general members after two registrations; got %d (%+v)", len(members), members)
	}

	wantSelfInviter := map[string]string{aliceID: aliceID, bobID: bobID}
	for _, m := range members {
		want, known := wantSelfInviter[m.UserID]
		if !known {
			t.Fatalf("unexpected member user_id=%q in #general (rows=%+v)", m.UserID, members)
		}
		if m.InviterUserID != want {
			t.Fatalf("self-invite violation: user_id=%q got inviter_user_id=%q want %q (full row=%+v)",
				m.UserID, m.InviterUserID, want, m)
		}
		if m.InviterSignature != "" {
			// L33 public-channel carve-out: signature MUST be NULL for
			// auto-add rows on is_public=TRUE channels. Omitempty makes
			// the JSON tag drop the field when the column is NULL, so a
			// non-empty value here means a signature got inserted on a
			// public-channel auto-add — the carve-out invariant is
			// broken.
			t.Fatalf("public-channel carve-out violation: user_id=%q has non-empty inviter_signature=%q (auto-add must leave it NULL)",
				m.UserID, m.InviterSignature)
		}
	}
}

// registerUser is the local helper version (the channel-membership
// package next door has its own copy; cross-package imports between
// _test packages aren't supported).
func registerUser(t *testing.T, srv *testsupport.Server, prefix string) (userID, token string) {
	t.Helper()
	name := prefix + "-" + testsupport.RandomSecret(t, 4)
	pw := testsupport.RandomSecret(t, 12)
	return testsupport.Register(t, srv.HTTPURL, srv.InviteCode, name, pw)
}

// singleChannelID asserts the listing returns exactly one channel
// (#general after a fresh registration) and returns its id.
func singleChannelID(t *testing.T, httpURL, bearer string) string {
	t.Helper()
	status, env, raw := getJSON(t, httpURL, "/api/channels", bearer)
	if status != http.StatusOK {
		t.Fatalf("GET /api/channels: %d body %s", status, raw)
	}
	var data struct {
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode listing: %v body %s", err, raw)
	}
	if len(data.Channels) != 1 {
		t.Fatalf("post-registration listing: got %d channels want 1 (#general); body=%s", len(data.Channels), raw)
	}
	return data.Channels[0].ID
}

// listMembers fetches /api/channels/{id}/members and returns the
// relevant subset of each row. Caller must be a current member of the
// channel (#general after registration).
func listMembers(t *testing.T, httpURL, channelID, bearer string) []memberRow {
	t.Helper()
	status, env, raw := getJSON(t, httpURL, "/api/channels/"+channelID+"/members", bearer)
	if status != http.StatusOK {
		t.Fatalf("GET /api/channels/%s/members: %d body %s", channelID, status, raw)
	}
	var data struct {
		Members []memberRow `json:"members"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /members: %v body %s", err, raw)
	}
	return data.Members
}

// getJSON wraps GET with a body returned for easy inspection. Local
// copy — testsupport.PostJSON exists but no shared GET helper does.
func getJSON(t *testing.T, httpURL, path, bearer string) (int, testsupport.Envelope, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpURL+path, nil) //nolint:noctx // loopback test helper
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
