package http

import (
	"encoding/json"
	"log/slog"

	"hackathon/apps/server/internal/repo"
)

// WSEventChannel is the outbound WS frame type for channel lifecycle
// notifications. Mirrors WSEventMessage / wsapi.PresenceEvent — clients
// switch on the same `type` field.
const WSEventChannel = "channel"

// channelEventKind discriminates the two lifecycle events carried in a
// `channel` frame's data payload (PRD §10).
const (
	channelEventCreate = "create"
	channelEventRename = "rename"
)

// channelEventPayload is the typed body of a `channel` frame:
//
//	{"type":"channel","data":{"kind":"create"|"rename","channel":{...}}}
//
// repo.Channel carries the JSON tags so the frame embeds the same
// envelope shape REST handlers return — no second DTO.
type channelEventPayload struct {
	Kind    string       `json:"kind"`
	Channel repo.Channel `json:"channel"`
}

// channelEventFrame builds the {type, data:{kind, channel}} JSON envelope
// for a channel lifecycle broadcast. Returns nil on the (unreachable in
// practice) marshal-error path so callers' BroadcastAll(nil) is the
// failure mode rather than a panic.
func channelEventFrame(kind string, ch repo.Channel) []byte {
	frame, err := json.Marshal(struct {
		Type string              `json:"type"`
		Data channelEventPayload `json:"data"`
	}{
		Type: WSEventChannel,
		Data: channelEventPayload{Kind: kind, Channel: ch},
	})
	if err != nil {
		slog.Error("channel event marshal", "err", err, "kind", kind)
		return nil
	}
	return frame
}
