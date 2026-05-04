package channels_and_messages_e2e_test

import (
	"net/http"
	"strings"
	"testing"
)

// AC-2: POST /api/channels with {name} creates a channel and returns it
// (US-4); rejects duplicate or invalid names.
//
// Three arms:
//   - happy path: a fresh name returns 201 with id (26-char ULID) + name.
//   - duplicate name: a second create with the same name returns 409
//     conflict, and the DB still has exactly one row for that name.
//   - invalid names: empty, whitespace-only, illegal characters, and
//     over-length all 4xx with no DB row created.
func TestAC2_CreateChannelHappyPathAndConflict(t *testing.T) {
	srv := startServer(t)
	tok, _ := register(t, srv, randomUsername(t), randomPassword(t))

	name := randomChannelName(t)
	ch := createChannel(t, srv, tok, name)
	if got, want := len(ch.ID), 26; got != want {
		t.Fatalf("AC-2: channel id length = %d, want %d (ULID)", got, want)
	}
	if ch.Name != name {
		t.Fatalf("AC-2: channel name = %q, want %q", ch.Name, name)
	}

	// Duplicate.
	status, body := createChannelRaw(t, srv, tok, name)
	if status != http.StatusConflict {
		t.Fatalf("AC-2: duplicate create status=%d body=%s; want 409", status, body)
	}
	env := decodeEnvelope(t, body)
	if env.OK || env.Error == nil || env.Error.Code != "conflict" {
		t.Fatalf("AC-2: duplicate envelope = %s; want ok=false code=conflict", body)
	}

	// DB has exactly one row for this name.
	db := openDBReadOnly(t, srv)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM channels WHERE name = ?", name).Scan(&count); err != nil {
		t.Fatalf("AC-2: count channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("AC-2: rows for name %q = %d, want 1", name, count)
	}
}

// AC-2 (gap): negative twin — invalid names are rejected with 4xx and
// no row is written. Sibling of the happy-path test above so a
// regression in either arm surfaces independently.
func TestAC2_CreateChannelRejectsInvalidNames(t *testing.T) {
	srv := startServer(t)
	tok, _ := register(t, srv, randomUsername(t), randomPassword(t))

	cases := []struct {
		label string
		name  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"uppercase letters", "Engineering"},
		{"slash", "team/eng"},
		{"hash prefix", "#general"},
		{"leading hyphen", "-leadhyphen"},
		{"too long", strings.Repeat("a", 41)},
	}

	for _, c := range cases {
		c := c
		t.Run(c.label, func(t *testing.T) {
			status, body := createChannelRaw(t, srv, tok, c.name)
			if status < 400 || status >= 500 {
				t.Fatalf("AC-2: %q status=%d body=%s; want 4xx", c.name, status, body)
			}
			env := decodeEnvelope(t, body)
			if env.OK || env.Error == nil {
				t.Fatalf("AC-2: %q envelope = %s; want ok=false", c.name, body)
			}
		})
	}

	// Confirm none of the invalid names landed a row.
	db := openDBReadOnly(t, srv)
	for _, c := range cases {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM channels WHERE name = ?", strings.TrimSpace(c.name)).Scan(&count); err != nil {
			t.Fatalf("AC-2: count for %q: %v", c.name, err)
		}
		if count != 0 {
			t.Fatalf("AC-2: invalid name %q produced %d row(s); want 0", c.name, count)
		}
	}
}
