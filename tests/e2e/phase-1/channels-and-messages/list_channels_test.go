package channels_and_messages_e2e_test

import (
	"testing"
)

// AC-1: GET /api/channels returns the list of channels (US-3).
//
// Boots the server with a fresh sqlite db, registers + logs in, asserts
// the list endpoint contains the seeded #general channel (phase-3 seed,
// apps/server/internal/seed/seed.go) and nothing else, then creates a
// channel with a random name and confirms a follow-up list contains it.
func TestAC1_ListChannelsReturnsCreated(t *testing.T) {
	srv := startServer(t)

	username := randomUsername(t)
	password := randomPassword(t)
	tok, _ := register(t, srv, username, password)

	// Phase 3 seeds a #general channel on first boot; the only entry
	// in a fresh-db list should be that one.
	initial := listChannels(t, srv, tok)
	if len(initial) != 1 || initial[0].Name != "general" {
		t.Fatalf("AC-1: fresh-db list should be exactly the seeded #general channel, got %+v", initial)
	}

	name := randomChannelName(t)
	created := createChannel(t, srv, tok, name)

	after := listChannels(t, srv, tok)
	var found bool
	for _, c := range after {
		if c.Name == name && c.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("AC-1: created channel %q (id=%s) not in list: %+v", name, created.ID, after)
	}
}
