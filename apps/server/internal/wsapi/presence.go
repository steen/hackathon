package wsapi

import "encoding/json"

// PresenceEvent is the WS frame type for join/leave notifications.
// Mirrors the chat-message envelope so client decoders can switch on
// the same `type` field.
const PresenceEvent = "presence"

// presenceJoin / presenceLeave are the two `kind` values carried in
// the PresenceEvent payload (US-7).
const (
	presenceJoin  = "join"
	presenceLeave = "leave"
)

// presencePayload is the typed body of a presence frame: a discriminator
// plus the affected user's ID. Username is intentionally omitted —
// clients can join the ID against /api/presence (or the canonical user
// record from /api/auth/me) when they need a display name.
type presencePayload struct {
	Kind   string `json:"kind"`
	UserID string `json:"user_id"`
}

// presenceFrame builds a `{type:"presence", data:{kind,user_id}}` JSON
// envelope. Marshal errors here are unreachable in practice — the
// payload is two fixed strings — but we still surface a non-nil bytes
// slice on success and `nil` on the impossible error path so the
// caller's `BroadcastAll(nil)` would fan out an empty frame instead of
// panicking.
func presenceFrame(kind, userID string) []byte {
	frame, err := json.Marshal(struct {
		Type string          `json:"type"`
		Data presencePayload `json:"data"`
	}{
		Type: PresenceEvent,
		Data: presencePayload{Kind: kind, UserID: userID},
	})
	if err != nil {
		return nil
	}
	return frame
}
