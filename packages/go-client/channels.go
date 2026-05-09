package goclient

// Wire types — keep in sync with packages/api-client/src/types.ts.
// When adding a JSON field here, mirror it in TS and add an e2e assertion.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Channel mirrors the server-side repo.Channel JSON shape.
//
// LastMessageID, LastMessageAt, UnreadCount, and LastReadMessageID are
// optional listing-additive fields per the Phase 9 read-state contract
// (decision-log L26 — optional-first wire shape). They are nil on every
// endpoint until the channel-listing populator ships server-side, so
// consumers must tolerate nil.
//
// IsPublic is the Phase-10 public-channel flag (decision-log §9 + L24).
// Pointer for tri-state (nil = server has not populated; *true / *false
// = explicit). Immutable after channel creation per L15. Wave 2 (M /
// membership PR) populates it.
type Channel struct {
	ID                ULID       `json:"id"`
	Name              string     `json:"name"`
	CreatedAt         time.Time  `json:"created_at"`
	LastMessageID     *ULID      `json:"last_message_id,omitempty"`
	LastMessageAt     *time.Time `json:"last_message_at,omitempty"`
	UnreadCount       *int       `json:"unread_count,omitempty"`
	LastReadMessageID *ULID      `json:"last_read_message_id,omitempty"`
	IsPublic          *bool      `json:"is_public,omitempty"`
}

// channelsListResponse is the envelope payload for GET /api/channels.
type channelsListResponse struct {
	Channels []Channel `json:"channels"`
}

// createChannelRequest is the wire body for POST /api/channels.
type createChannelRequest struct {
	Name string `json:"name"`
}

// renameChannelRequest is the wire body for PATCH /api/channels/{id}.
type renameChannelRequest struct {
	Name string `json:"name"`
}

// ListChannels returns every channel the server knows about, ordered
// by id (ULID — chronological).
func (c *Client) ListChannels(ctx context.Context) ([]Channel, error) {
	var out channelsListResponse
	if err := c.do(ctx, http.MethodGet, "/api/channels", nil, &out); err != nil {
		return nil, err
	}
	return out.Channels, nil
}

// CreateChannel creates a new channel with the given name. The server
// validates the name shape (lowercase letters, digits, hyphens; 1-40
// chars) and returns 409 conflict when the name is already taken —
// callers can branch on `IsCode(err, "conflict")`.
func (c *Client) CreateChannel(ctx context.Context, name string) (*Channel, error) {
	var out Channel
	if err := c.do(ctx, http.MethodPost, "/api/channels", createChannelRequest{Name: name}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenameChannel renames the channel with the given id. The server
// applies the same name validation as CreateChannel and returns 409
// conflict when the new name is already taken — callers can branch on
// `IsCode(err, "conflict")`. 404 surfaces as `IsCode(err, "not_found")`.
func (c *Client) RenameChannel(ctx context.Context, id, name string) (Channel, error) {
	var out Channel
	if err := c.do(ctx, http.MethodPatch, "/api/channels/"+id, renameChannelRequest{Name: name}, &out); err != nil {
		return Channel{}, err
	}
	return out, nil
}

// MarkChannelRead advances the viewer's read pointer for channelID to
// messageID. The server applies the advance-only rule (decision-log
// L5) — a pointer that would move backwards is silently kept; the call
// still returns 200 (idempotent client behavior). The server emits a
// {type:"read"} WS frame to the viewer's user:<viewer> topic for
// cross-device sync (no peer fan-out — L10).
func (c *Client) MarkChannelRead(ctx context.Context, channelID, messageID string) error {
	path := fmt.Sprintf("/api/channels/%s/read", url.PathEscape(channelID))
	return c.do(ctx, http.MethodPost, path, markReadRequest{MessageID: messageID}, nil)
}
