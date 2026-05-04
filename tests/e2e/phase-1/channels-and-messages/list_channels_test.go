package channels_and_messages_e2e_test

import (
	"testing"
)

// AC-1: GET /api/channels returns the list of channels (US-3).
//
// Boots the server with a fresh sqlite db, registers + logs in, asserts
// the list endpoint returns a JSON array (initially empty since the
// schema does not seed a default channel), then creates one and
// confirms a follow-up list contains it by name.
func TestAC1_ListChannelsReturnsCreated(t *testing.T) {
	srv := startServer(t)

	username := randomUsername(t)
	password := randomPassword(t)
	tok, _ := register(t, srv, username, password)

	// Empty initially (no migration seeds channels).
	initial := listChannels(t, srv, tok)
	if len(initial) != 0 {
		t.Fatalf("AC-1: initial channel list should be empty, got %d entries: %+v", len(initial), initial)
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
