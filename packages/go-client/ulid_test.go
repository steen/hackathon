package goclient_test

import (
	"encoding/json"
	"strings"
	"testing"

	goclient "hackathon/packages/go-client"
)

// validULID is a sample 26-char Crockford-base32 string used as a
// known-good fixture. Mirrors the cursor literal already present in
// messages_test.go ("01ABCDEFGHJKMNPQRSTVWXYZ00") so the two fixtures
// document the same shape side-by-side.
const validULID = "01ABCDEFGHJKMNPQRSTVWXYZ00"

func TestULIDValidAcceptsSpecConformantID(t *testing.T) {
	if !goclient.ULID(validULID).Valid() {
		t.Fatalf("Valid() = false for %q, want true", validULID)
	}
}

func TestULIDValidRejectsWrongLength(t *testing.T) {
	cases := []string{
		"",
		strings.Repeat("0", 25),
		strings.Repeat("0", 27),
		strings.Repeat("0", 32),
	}
	for _, c := range cases {
		if goclient.ULID(c).Valid() {
			t.Errorf("Valid() = true for length %d (%q), want false", len(c), c)
		}
	}
}

func TestULIDValidRejectsCrockfordHoles(t *testing.T) {
	// I, L, O, U are explicitly excluded from Crockford base32. Substituting
	// each into an otherwise-valid ULID must fail Valid().
	for _, ch := range []byte{'I', 'L', 'O', 'U', 'i', 'l', 'o', 'u'} {
		bad := []byte(validULID)
		bad[10] = ch
		if goclient.ULID(bad).Valid() {
			t.Errorf("Valid() = true for ULID containing %q, want false", string(ch))
		}
	}
}

func TestULIDValidRejectsLowercase(t *testing.T) {
	// The Crockford alphabet used by the ULID spec is upper-case; the
	// server emits upper-case. A lower-case variant is a sign the value
	// is hand-typed or transformed somewhere upstream.
	if goclient.ULID(strings.ToLower(validULID)).Valid() {
		t.Fatalf("Valid() should reject lower-case ULIDs")
	}
}

func TestULIDValidRejectsNonAlphanumeric(t *testing.T) {
	for _, ch := range []byte{' ', '-', '_', '/', '\n', '"'} {
		bad := []byte(validULID)
		bad[0] = ch
		if goclient.ULID(bad).Valid() {
			t.Errorf("Valid() = true for ULID containing %q, want false", string(ch))
		}
	}
}

func TestULIDStringReturnsUnderlying(t *testing.T) {
	if got := goclient.ULID(validULID).String(); got != validULID {
		t.Fatalf("String() = %q, want %q", got, validULID)
	}
}

// TestULIDMarshalIsPlainString confirms the wire format is an unwrapped
// JSON string — a server consuming the same bytes must not see a
// `{"ulid":"..."}` object or any other structural change.
func TestULIDMarshalIsPlainString(t *testing.T) {
	got, err := json.Marshal(goclient.ULID(validULID))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `"` + validULID + `"`
	if string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestULIDMarshalEmpty(t *testing.T) {
	got, err := json.Marshal(goclient.ULID(""))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `""` {
		t.Fatalf("Marshal(empty) = %s, want \"\"", got)
	}
}

func TestULIDUnmarshalRoundTrip(t *testing.T) {
	var u goclient.ULID
	if err := json.Unmarshal([]byte(`"`+validULID+`"`), &u); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(u) != validULID {
		t.Fatalf("u = %q, want %q", u, validULID)
	}
}

func TestULIDUnmarshalAcceptsAnyString(t *testing.T) {
	// Forward-compat: a slightly looser server (or a future ID format)
	// must not break decoding. Validation is opt-in via Valid().
	var u goclient.ULID
	if err := json.Unmarshal([]byte(`"not-a-ulid"`), &u); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(u) != "not-a-ulid" {
		t.Fatalf("u = %q, want non-validating value to round-trip", u)
	}
	if u.Valid() {
		t.Fatalf("Valid() = true for %q, want false", u)
	}
}

func TestULIDUnmarshalRejectsNonString(t *testing.T) {
	var u goclient.ULID
	if err := json.Unmarshal([]byte(`123`), &u); err == nil {
		t.Fatalf("Unmarshal of a JSON number should error")
	}
}

func TestULIDUnmarshalNullLeavesValueUnchanged(t *testing.T) {
	u := goclient.ULID("seed")
	if err := json.Unmarshal([]byte(`null`), &u); err != nil {
		t.Fatalf("Unmarshal null: %v", err)
	}
	if string(u) != "seed" {
		t.Fatalf("u = %q, want \"seed\" (null should leave receiver alone)", u)
	}
}

// TestULIDOnEmbeddedStructPreservesWireShape pins down the most
// load-bearing claim of the issue: changing User.ID/Channel.ID/Message.ID
// from string to ULID must not change the wire bytes. A server-side
// reader (or another non-Go client) must see exactly the same JSON.
func TestULIDOnEmbeddedStructPreservesWireShape(t *testing.T) {
	type beforeUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	beforeBytes, err := json.Marshal(beforeUser{ID: validULID, Username: "alice"})
	if err != nil {
		t.Fatalf("marshal beforeUser: %v", err)
	}

	afterBytes, err := json.Marshal(goclient.User{ID: goclient.ULID(validULID), Username: "alice"})
	if err != nil {
		t.Fatalf("marshal goclient.User: %v", err)
	}

	if string(beforeBytes) != string(afterBytes) {
		t.Fatalf("wire shape drifted:\n  before=%s\n  after =%s", beforeBytes, afterBytes)
	}
}

// TestULIDDecodeFromServerEnvelope round-trips a sample auth payload
// shaped exactly like apps/server/internal/http/auth_handlers.go emits,
// confirming decode through *both* AuthResponse and the embedded User.
func TestULIDDecodeFromServerEnvelope(t *testing.T) {
	const wire = `{"token":"jwt-abc","user":{"id":"` + validULID + `","username":"alice"}}`
	var resp goclient.AuthResponse
	if err := json.Unmarshal([]byte(wire), &resp); err != nil {
		t.Fatalf("Unmarshal AuthResponse: %v", err)
	}
	if resp.User.ID != goclient.ULID(validULID) {
		t.Fatalf("User.ID = %q, want %q", resp.User.ID, validULID)
	}
	if !resp.User.ID.Valid() {
		t.Fatalf("decoded User.ID should be Valid()")
	}
}
