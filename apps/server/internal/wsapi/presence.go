package wsapi

import (
	"encoding/json"
	"sync/atomic"
)

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

// presencePayload is the typed body of a presence frame: a discriminator,
// the affected user's ID, and (best-effort) their username. Username uses
// `omitempty` so the frame remains additive on the wire — old clients
// that decode without the field continue to work, and a handler that
// has not registered a username lookup still emits a wire-compatible
// {kind,user_id} payload (#490).
type presencePayload struct {
	Kind     string `json:"kind"`
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"`
}

// usernameLookup is consulted by presenceFrame to enrich outbound
// presence frames with the affected user's username. The hook is set
// at wiring time via SetPresenceUsernameLookup; when nil (or when the
// lookup returns an empty string) the frame is emitted with username
// omitted, matching the pre-#490 wire shape. atomic.Pointer keeps the
// read path lock-free and lets tests swap the hook without racing.
var usernameLookup atomic.Pointer[func(userID string) string]

// SetPresenceUsernameLookup registers (or clears, when fn is nil) the
// hook that resolves a user ID to a display username at presence-frame
// emit time. Called from the server wiring once the user store is
// available; tests use it to inject a deterministic resolver. Passing
// a nil fn restores the no-lookup behavior (frame emitted without the
// username field).
func SetPresenceUsernameLookup(fn func(userID string) string) {
	if fn == nil {
		usernameLookup.Store(nil)
		return
	}
	usernameLookup.Store(&fn)
}

// presenceFrame builds a `{type:"presence", data:{kind,user_id,username?}}`
// JSON envelope. Marshal errors here are unreachable in practice — the
// payload is fixed strings — but we still surface a non-nil bytes slice
// on success and `nil` on the impossible error path so the caller's
// `BroadcastAll(nil)` would fan out an empty frame instead of panicking.
func presenceFrame(kind, userID string) []byte {
	var username string
	if p := usernameLookup.Load(); p != nil {
		username = (*p)(userID)
	}
	frame, err := json.Marshal(struct {
		Type string          `json:"type"`
		Data presencePayload `json:"data"`
	}{
		Type: PresenceEvent,
		Data: presencePayload{Kind: kind, UserID: userID, Username: username},
	})
	if err != nil {
		return nil
	}
	return frame
}
