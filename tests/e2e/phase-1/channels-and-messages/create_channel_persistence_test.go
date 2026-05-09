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
//     full process round-trip back through GET /api/channels for users
//     who are members of the channel.
//
// Phase-10 §6 + L25 changes the visibility semantics: GET /api/channels
// filters to channels the viewer is a member of. The original "second
// user sees the channel" assertion held under implicit-membership and
// is preserved here against a fresh A-creator who DOES see it; the
// non-creator B sees only #general (the seeded public channel they
// auto-joined at registration). The duplicate-name uniqueness check
// remains global — it asserts persistence at the schema layer through
// the SELECT on `channels`.
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

	// Creator (member via §10 self-bootstrap) sees the channel.
	visibleToA := listChannels(t, srv, tokA)
	var foundA bool
	for _, c := range visibleToA {
		if c.ID == created.ID && c.Name == name {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Fatalf("AC-2 (Phase-10): channel %q (id=%s) not visible to creator: %+v", name, created.ID, visibleToA)
	}

	// Non-member B sees only the seeded #general channel they were
	// auto-joined to at registration; the new private channel must NOT
	// appear in their listing per L25.
	visibleToB := listChannels(t, srv, tokB)
	for _, c := range visibleToB {
		if c.ID == created.ID {
			t.Fatalf("AC-2 (Phase-10): non-member B should not see private channel %q (id=%s) — L25 listing filter; got %+v", name, created.ID, visibleToB)
		}
	}
}
