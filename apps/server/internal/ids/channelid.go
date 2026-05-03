package ids

import "strings"

// NormalizeChannelID upper-folds and validates a 26-char ULID-shaped channel
// id. Returns the canonical (upper-cased) form and true on a structurally
// valid id; the empty string and false otherwise. Centralized so the REST
// handler (`/api/channels/{id}/messages`) and the WS handler (`/ws?channel=`)
// agree on case-folding — a client that lower-cases the URL must hit the
// same channel on both surfaces (audit #78, info severity).
//
// Crockford-base32 ULIDs use 0-9 and A-Z; the validator accepts that
// alphabet rather than re-validating against the stricter ULID checksum,
// matching the prior in-tree check at channels_handlers.go.
//
// The legacy `defaultChannel` sentinel ("#general") is NOT a ULID and is
// not normalized here — callers gate the sentinel separately and only
// invoke this helper for non-sentinel ids.
func NormalizeChannelID(raw string) (string, bool) {
	id := strings.ToUpper(raw)
	if len(id) != 26 {
		return "", false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return "", false
		}
	}
	return id, true
}
