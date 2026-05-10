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

// channelEventKind discriminates the lifecycle events carried in a
// `channel` frame's data payload (PRD §10 + e2e decision-log L9 +
// L29). create + rename land with Phase 8; members_changed +
// key_received + wrap_failed land with Phase 10's keys-RPC (#984).
const (
	channelEventCreate         = "create"
	channelEventRename         = "rename"
	channelEventMembersChanged = "members_changed"
	channelEventKeyReceived    = "key_received"
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

// MembersChangedUser is the per-member projection embedded in the
// members_at_rotation array of a members_changed frame. Wire-mirrored
// in packages/api-client/src/types.ts (User) and packages/go-client/
// users.go (User). The keys-RPC handler builds it from the current
// channel_members + users join.
type MembersChangedUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// membersChangedPayload is the typed body of a members_changed frame
// per e2e decision-log L9:
//
//	{"type":"channel","data":{
//	    "kind":"members_changed","channel_id":"...",
//	    "current_generation_id":<int>,
//	    "members_at_rotation":[<User>...]
//	}}
type membersChangedPayload struct {
	Kind                string               `json:"kind"`
	ChannelID           string               `json:"channel_id"`
	CurrentGenerationID int64                `json:"current_generation_id"`
	MembersAtRotation   []MembersChangedUser `json:"members_at_rotation"`
}

// membersChangedFrame builds the L9 members_changed frame. Used by
// the keys-RPC rotation arm (#984) and by future member add/remove
// handlers that need to broadcast the new generation. Returns nil on
// marshal failure (the caller's BroadcastAll(nil) is a no-op).
func membersChangedFrame(channelID string, generation int64, members []MembersChangedUser) []byte {
	if members == nil {
		members = []MembersChangedUser{}
	}
	frame, err := json.Marshal(struct {
		Type string                `json:"type"`
		Data membersChangedPayload `json:"data"`
	}{
		Type: WSEventChannel,
		Data: membersChangedPayload{
			Kind:                channelEventMembersChanged,
			ChannelID:           channelID,
			CurrentGenerationID: generation,
			MembersAtRotation:   members,
		},
	})
	if err != nil {
		slog.Error("members_changed event marshal", "err", err, "channel_id", channelID)
		return nil
	}
	return frame
}

// keyReceivedPayload is the typed body of a key_received frame per
// e2e decision-log L9:
//
//	{"type":"channel","data":{
//	    "kind":"key_received","channel_id":"...","generation_id":<int>
//	}}
//
// Frame is fanned out only to the recipient's `user:<viewer>` topic,
// not BroadcastAll — peers don't care which wraps another user
// received. specs/plans/phase-10/keys.md "Fill-in mode" step 10.
type keyReceivedPayload struct {
	Kind         string `json:"kind"`
	ChannelID    string `json:"channel_id"`
	GenerationID int64  `json:"generation_id"`
}

// keyReceivedFrame builds the L9 key_received frame. Used by the
// keys-RPC handler (#984) when a fill-in or rotation insert lands.
// Returns nil on marshal failure.
func keyReceivedFrame(channelID string, generation int64) []byte {
	frame, err := json.Marshal(struct {
		Type string             `json:"type"`
		Data keyReceivedPayload `json:"data"`
	}{
		Type: WSEventChannel,
		Data: keyReceivedPayload{
			Kind:         channelEventKeyReceived,
			ChannelID:    channelID,
			GenerationID: generation,
		},
	})
	if err != nil {
		slog.Error("key_received event marshal", "err", err, "channel_id", channelID)
		return nil
	}
	return frame
}
