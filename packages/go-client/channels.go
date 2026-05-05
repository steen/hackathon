package goclient

import (
	"context"
	"net/http"
	"time"
)

// Channel mirrors the server-side repo.Channel JSON shape.
type Channel struct {
	ID        ULID      `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// channelsListResponse is the envelope payload for GET /api/channels.
type channelsListResponse struct {
	Channels []Channel `json:"channels"`
}

// createChannelRequest is the wire body for POST /api/channels.
type createChannelRequest struct {
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
