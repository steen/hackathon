package goclient

// Wire types — keep in sync with packages/api-client/src/types.ts.
// When adding a JSON field here, mirror it in TS and add an e2e assertion.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Conversation mirrors the server-side dm_conversations row enriched with
// listing-only fields (Peer, UnreadCount). Pointer fields encode the
// "no message yet" baseline as JSON null per dms.md L26 (optional-first).
type Conversation struct {
	ID            ULID       `json:"id"`
	Peer          User       `json:"peer"`
	LastMessageAt *time.Time `json:"last_message_at"`
	UnreadCount   int        `json:"unread_count"`
	LastMessageID *ULID      `json:"last_message_id"`
}

// DMMessage mirrors the dm_messages row. Immutable on the wire (L9 — no
// edit/delete in v1).
//
// Envelope is the Phase-10 encrypted-message envelope (L21); shape is
// identical to Message.Envelope but the signature scope binds
// conversation_id (snakd-msg-v1:dm: prefix per L21) for cross-protocol
// confusion resistance. Pointer for optional under the L26
// optional-first rule; #983 narrows it.
type DMMessage struct {
	ID             ULID             `json:"id"`
	ConversationID ULID             `json:"conversation_id"`
	SenderUserID   ULID             `json:"sender_user_id"`
	Body           string           `json:"body"`
	CreatedAt      time.Time        `json:"created_at"`
	Envelope       *MessageEnvelope `json:"envelope,omitempty"`
}

// dmsListResponse is the envelope payload for GET /api/dms.
type dmsListResponse struct {
	Conversations []Conversation `json:"conversations"`
}

// dmMessagesListResponse is the envelope payload for
// GET /api/dms/{id}/messages.
type dmMessagesListResponse struct {
	Messages []DMMessage `json:"messages"`
}

// createDMRequest is the wire body for POST /api/dms.
//
// RootKeyWraps is the optional Phase-10 atomic-create payload (§7 +
// L6 + L12): exactly two entries on the 201 (newly-created) path,
// each carrying a per-recipient wrap of the conversation root key;
// the 200 (idempotent re-call) path MUST omit the field — server
// returns 409 `wraps_already_set` if supplied.
type createDMRequest struct {
	PeerUserID   string      `json:"peer_user_id"`
	RootKeyWraps []WrapEntry `json:"root_key_wraps,omitempty"`
}

// sendDMRequest is the wire body for POST /api/dms/{id}/messages.
type sendDMRequest struct {
	Body string `json:"body"`
}

// markReadRequest is the wire body for POST /api/dms/{id}/read and
// POST /api/channels/{id}/read.
type markReadRequest struct {
	MessageID string `json:"message_id"`
}

// CreateDM is idempotent find-or-create: returns the conversation row
// for the (viewer, peer) pair, creating it on first call. The server
// responds 201 on create / 200 on existing per L18; the client surfaces
// the same Conversation either way. Self-DM and unknown-peer rejections
// surface as *APIError; branch via IsCode at the call site.
//
// This call omits root_key_wraps; server falls through to the legacy
// create path (no wraps inserted). Use CreateDMWithWraps for the
// Phase-10 atomic-create shape.
func (c *Client) CreateDM(ctx context.Context, peerUserID string) (*Conversation, error) {
	var out Conversation
	if err := c.do(ctx, http.MethodPost, "/api/dms", createDMRequest{PeerUserID: peerUserID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDMWithWraps is the §7 + L6 + L7 atomic-create variant:
// supplies both per-recipient wraps in the same request so the
// conversation row + both dm_conversation_keys rows land in one
// transaction. wraps must contain exactly 2 entries
// (recipient_user_id ∈ {self, peer}). On the 200 idempotent re-call
// path the server returns 409 `wraps_already_set` (DMs never rotate).
func (c *Client) CreateDMWithWraps(ctx context.Context, peerUserID string, wraps []WrapEntry) (*Conversation, error) {
	var out Conversation
	body := createDMRequest{PeerUserID: peerUserID, RootKeyWraps: wraps}
	if err := c.do(ctx, http.MethodPost, "/api/dms", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDMs returns every conversation the viewer participates in that
// has at least one message (decision §3 — empty conversations are
// hidden until they have content). No pagination in v1 (L12).
func (c *Client) ListDMs(ctx context.Context) ([]Conversation, error) {
	var out dmsListResponse
	if err := c.do(ctx, http.MethodGet, "/api/dms", nil, &out); err != nil {
		return nil, err
	}
	return out.Conversations, nil
}

// SendDMMessage creates a message in the conversation and returns the
// persisted row. The server fans the same record out to both
// participants' user:<viewer> WS topics. 404 surfaces on
// non-participation (L8 — no membership leak).
func (c *Client) SendDMMessage(ctx context.Context, conversationID, body string) (*DMMessage, error) {
	path := fmt.Sprintf("/api/dms/%s/messages", url.PathEscape(conversationID))
	var out DMMessage
	if err := c.do(ctx, http.MethodPost, path, sendDMRequest{Body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDMMessagesOptions mirrors ListMessagesOptions for the DM
// transcript endpoint. Zero values use the server defaults — limit
// defaults to 50, capped server-side at 200; Before is an exclusive
// ULID cursor, paging backwards through history.
type ListDMMessagesOptions struct {
	Before ULID
	Limit  int
}

// ListDMMessages returns up to opts.Limit messages from conversationID,
// newest first. Same cursor semantics as ListMessages (different
// table). 404 on non-participation (L8).
func (c *Client) ListDMMessages(ctx context.Context, conversationID string, opts ListDMMessagesOptions) ([]DMMessage, error) {
	q := url.Values{}
	if opts.Before != "" {
		q.Set("before", string(opts.Before))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	path := fmt.Sprintf("/api/dms/%s/messages", url.PathEscape(conversationID))
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out dmMessagesListResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// MarkDMRead advances the viewer's read pointer for the conversation
// to messageID. The server applies the advance-only rule (L5) — a
// pointer that would move backwards is silently kept; the call still
// returns 200 (idempotent client behavior). The server emits a
// {type:"read"} WS frame to the viewer's user:<viewer> topic for
// cross-device sync (no peer fan-out — L10).
func (c *Client) MarkDMRead(ctx context.Context, conversationID, messageID string) error {
	path := fmt.Sprintf("/api/dms/%s/read", url.PathEscape(conversationID))
	return c.do(ctx, http.MethodPost, path, markReadRequest{MessageID: messageID}, nil)
}
