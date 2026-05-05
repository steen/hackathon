package wsapi

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
)

// Stable error-frame codes per PRD §10. Exported so e2e callers and
// the web client can switch on the same constants the server emits.
const (
	ErrCodeBodyTooLarge = "body_too_large"
	ErrCodeRateLimited  = "rate_limited"
)

// errorFrameType is the discriminator value of a server→client error
// envelope (PRD §10). Mirrors PresenceEvent / MessageEvent layout so
// client decoders can switch on a single `type` field.
const errorFrameType = "error"

// errorFrame is the typed body of an outbound error envelope:
//
//	{"type":"error","data":{"code":"<stable code>","message":"<human-readable>"}}
//
// PRD §10 fixes the shape; callers MUST use one of the Err* code
// constants above so a well-behaved client can branch on the code
// without parsing the message text.
type errorFrame struct {
	Type string         `json:"type"`
	Data errorFrameData `json:"data"`
}

type errorFrameData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeErrorFrame emits a single text frame carrying the PRD §10 error
// envelope. The caller is expected to follow this with a websocket
// close (1008/1009 per the trigger). Marshal errors are unreachable in
// practice (fixed-shape struct, ASCII-only fields) so the error from
// json.Marshal is dropped; if conn.Write itself fails the connection
// is already gone and the subsequent Close will be a no-op.
func writeErrorFrame(ctx context.Context, conn *websocket.Conn, code, message string) {
	payload, err := json.Marshal(errorFrame{
		Type: errorFrameType,
		Data: errorFrameData{Code: code, Message: message},
	})
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, payload)
}
