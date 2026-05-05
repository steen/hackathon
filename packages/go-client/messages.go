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

// ListMessagesOptions tunes ListMessages. Zero values mean "use the
// server default" — limit defaults to 50, capped server-side at 200;
// before is an exclusive ULID cursor.
type ListMessagesOptions struct {
	Before ULID
	Limit  int
}

// PostMessageOptions carries the body — and any future tunables — for
// PostMessage. It exists so PostMessage matches the package-wide
// `ctx, requiredPositional..., opts struct` shape used by ListMessages
// and Watch; new fields can land here without another breaking signature
// change. The JSON tag pins the wire field name so renaming the Go
// field never breaks the server contract.
type PostMessageOptions struct {
	Body string `json:"body"`
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
func (c *Client) PostMessage(ctx context.Context, channelID string, opts PostMessageOptions) (*Message, error) {
	path := fmt.Sprintf("/api/channels/%s/messages", url.PathEscape(channelID))
	var out Message
	if err := c.do(ctx, http.MethodPost, path, opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
