package channels_and_messages_e2e_test

import (
	"net/http"
	"testing"
)

// AC-2: POST /api/channels with {name} creates a channel and returns it
// (US-4); rejects duplicate or invalid names.
//
// Net-new black-box angles complementing create_channel_test.go:
//
//   - cross-user duplicate: a second user attempting the same name still
//     gets 409, confirming channel-name uniqueness is global rather than
//     per-creator. The existing happy/conflict test only retries from
//     the same caller.
//   - persistence: the row written by POST /api/channels survives a
//     full process round-trip back through GET /api/channels (the AC-1
//     test asserts list-after-create for the same caller; this asserts
//     the row is visible to a different authenticated user, proving
//     the channel is shared, not per-user).
func TestAC2_CreateChannelCrossUserDuplicateAndVisibility(t *testing.T) {
	srv := startServer(t)

	tokA, _ := register(t, srv, randomUsername(t), randomPassword(t))
	tokB, _ := register(t, srv, randomUsername(t), randomPassword(t))

	name := randomChannelName(t)
	created := createChannel(t, srv, tokA, name)

	status, body := createChannelRaw(t, srv, tokB, name)
	if status != http.StatusConflict {
		t.Fatalf("AC-2: cross-user duplicate status=%d body=%s; want 409", status, body)
	}
	env := decodeEnvelope(t, body)
	if env.OK || env.Error == nil || env.Error.Code != "conflict" {
		t.Fatalf("AC-2: cross-user duplicate envelope = %s; want ok=false code=conflict", body)
	}

	db := openDBReadOnly(t, srv)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM channels WHERE name = ?", name).Scan(&count); err != nil {
		t.Fatalf("AC-2: count channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("AC-2: rows for name %q after cross-user duplicate = %d, want 1", name, count)
	}

	visibleToB := listChannels(t, srv, tokB)
	var found bool
	for _, c := range visibleToB {
		if c.ID == created.ID && c.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("AC-2: channel %q (id=%s) not visible to second user: %+v", name, created.ID, visibleToB)
	}
}
