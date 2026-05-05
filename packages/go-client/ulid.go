package goclient

import (
	"encoding/json"
	"fmt"
)

// ULID is a 26-char Crockford-base32 identifier as defined by the ULID
// spec (https://github.com/ulid/spec). Server-issued IDs (User.ID,
// Channel.ID, Message.ID, ...) decode into this type so callers can
// branch on Valid() before forwarding an attacker-controlled cursor.
//
// Wire format is unchanged: MarshalJSON/UnmarshalJSON encode and decode
// as a plain JSON string, so a server upgrade is not required.
//
// The type does NOT validate on UnmarshalJSON. The server is the source
// of truth for ID shape; rejecting a server response that fails this
// client's stricter check would create a deploy-order coupling where
// a slightly looser server breaks every client. Use Valid() at the
// boundaries you care about (e.g. before passing a user-supplied
// cursor to ListMessages).
type ULID string

// ulidLength is the canonical Crockford-base32 ULID length per spec §2.
const ulidLength = 26

// crockfordAlphabet is the Crockford base-32 alphabet per
// https://www.crockford.com/base32.html as cited by the ULID spec §4.
// Excludes I, L, O, U to avoid visual collisions with 1, 1, 0, V.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// validCrockford is a 256-entry lookup table — true at index c iff c is
// one of the 32 alphabet bytes. Built once at package init so Valid()
// is a length check plus 26 table lookups, not 26 strings.IndexByte.
var validCrockford [256]bool

func init() {
	for i := 0; i < len(crockfordAlphabet); i++ {
		validCrockford[crockfordAlphabet[i]] = true
	}
}

// Valid reports whether u is a 26-char Crockford-base32 ULID. It does
// NOT enforce the spec's "first char ≤ 7" timestamp ceiling (year
// 10889) — server validation already covers that and a client check
// would only add a deploy-order trap.
func (u ULID) Valid() bool {
	if len(u) != ulidLength {
		return false
	}
	for i := 0; i < ulidLength; i++ {
		if !validCrockford[u[i]] {
			return false
		}
	}
	return true
}

// String returns the underlying string. Provided so fmt's %s and
// callers that need a plain string (e.g. url.PathEscape) can use a
// ULID without an explicit conversion at every site.
func (u ULID) String() string { return string(u) }

// MarshalJSON encodes the ULID as a JSON string. Empty ULIDs encode as
// the empty string ("") so a zero-valued field round-trips cleanly.
func (u ULID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(u))
}

// UnmarshalJSON decodes a JSON string into the ULID, accepting any
// string the server emits. JSON null leaves the receiver unchanged so
// optional ID fields don't fail the whole envelope decode.
func (u *ULID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("ulid: decode: %w", err)
	}
	*u = ULID(s)
	return nil
}
