package goclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Message mirrors the server-side repo.Message JSON shape (PRD §10).
type Message struct {
	ID           ULID      `json:"id"`
	ChannelID    ULID      `json:"channel_id"`
	SenderUserID ULID      `json:"sender_user_id"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

// messagesListResponse is the envelope payload for
// GET /api/channels/{id}/messages.
type messagesListResponse struct {
	Messages []Message `json:"messages"`
}

// postMessageRequest is the wire body for POST /api/channels/{id}/messages.
type postMessageRequest struct {
	Body string `json:"body"`
}

// ListMessagesOptions tunes ListMessages. Zero values mean "use the
// server default" — limit defaults to 50, capped server-side at 200;
// before is an exclusive ULID cursor.
type ListMessagesOptions struct {
	Before ULID
	Limit  int
}

// ListMessages returns up to opts.Limit messages from channelID, newest
// first. When opts.Before is non-empty it acts as an exclusive ULID
// cursor, paging backwards through history.
func (c *Client) ListMessages(ctx context.Context, channelID string, opts ListMessagesOptions) ([]Message, error) {
	q := url.Values{}
	if opts.Before != "" {
		q.Set("before", string(opts.Before))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	path := fmt.Sprintf("/api/channels/%s/messages", url.PathEscape(channelID))
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out messagesListResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// PostMessage creates a message in channelID and returns the persisted
// row (with server-assigned ULID and timestamp). The server broadcasts
// the same record to every WS subscriber on the channel.
func (c *Client) PostMessage(ctx context.Context, channelID, body string) (*Message, error) {
	path := fmt.Sprintf("/api/channels/%s/messages", url.PathEscape(channelID))
	var out Message
	if err := c.do(ctx, http.MethodPost, path, postMessageRequest{Body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
