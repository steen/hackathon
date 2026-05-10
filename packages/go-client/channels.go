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
//
// IsPublic is the Phase-10 public-channel flag (decision-log §9 + L24).
// Pointer + omitempty so older clients that don't pass the field send
// `{"name": "..."}`, which the server treats as private-by-default.
type createChannelRequest struct {
	Name     string `json:"name"`
	IsPublic *bool  `json:"is_public,omitempty"`
}

// MembershipBlockReq mirrors the §10 inviter-signed channel-membership
// payload carried on POST /api/channels/{id}/members. The server-side
// type is in apps/server/internal/http/members_handlers.go (kept in
// sync — see top-of-file note in packages/api-client/src/types.ts on
// MembershipBlock). Pubkey + signature fields are base64-encoded
// raw bytes (32 / 64 respectively).
type MembershipBlockReq struct {
	InviterUserID     string `json:"inviter_user_id"`
	InviterSignPubkey string `json:"inviter_sign_pubkey"`
	InviteeBoxPubkey  string `json:"invitee_box_pubkey"`
	InviteeSignPubkey string `json:"invitee_sign_pubkey"`
	AddedAt           string `json:"added_at"`
	InviterSignature  string `json:"inviter_signature"`
}

// inviteRequest is the wire body for POST /api/channels/{id}/members.
// The server validates the membership block when present; for public
// channels callers may omit it (the server-auto-add carve-out).
type inviteRequest struct {
	UserID     string              `json:"user_id"`
	Membership *MembershipBlockReq `json:"membership,omitempty"`
}

// ChannelMember mirrors the JSON shape of one row in
// `GET /api/channels/{id}/members` (decision-log §6). InviterSignature
// is empty for the public-channel server-auto-add carve-out (R1.2).
type ChannelMember struct {
	UserID            string    `json:"user_id"`
	InviterUserID     string    `json:"inviter_user_id"`
	InviterSignPubkey string    `json:"inviter_sign_pubkey"`
	InviterSignature  string    `json:"inviter_signature,omitempty"`
	InviteeBoxPubkey  string    `json:"invitee_box_pubkey"`
	InviteeSignPubkey string    `json:"invitee_sign_pubkey"`
	AddedAt           time.Time `json:"added_at"`
	Username          string    `json:"username,omitempty"`
}

type membersListResponse struct {
	Members []ChannelMember `json:"members"`
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

// CreateChannel creates a new channel with the given name. Calls
// CreateChannelOpts(ctx, name, false). Older callers stay private-by-
// default — `is_public` is opt-in.
func (c *Client) CreateChannel(ctx context.Context, name string) (*Channel, error) {
	return c.CreateChannelOpts(ctx, name, false)
}

// CreateChannelOpts creates a new channel and toggles is_public per the
// caller. Phase-10 §9: public channels are server-readable by every
// registered user (R1.2 carve-out); private channels (the default)
// gate access through the explicit channel_members relation. The
// server validates the name shape (lowercase letters, digits, hyphens;
// 1-40 chars) and returns 409 conflict when the name is already taken
// — callers can branch on `IsCode(err, "conflict")`.
func (c *Client) CreateChannelOpts(ctx context.Context, name string, isPublic bool) (*Channel, error) {
	flag := isPublic
	body := createChannelRequest{Name: name, IsPublic: &flag}
	var out Channel
	if err := c.do(ctx, http.MethodPost, "/api/channels", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListChannelMembers fetches the member list for channelID. The caller
// must be a current member of the channel; non-members get 403.
func (c *Client) ListChannelMembers(ctx context.Context, channelID string) ([]ChannelMember, error) {
	path := fmt.Sprintf("/api/channels/%s/members", url.PathEscape(channelID))
	var out membersListResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Members, nil
}

// InviteChannelMember adds invitee to channelID. The caller must be a
// current member of the channel. The server validates the membership
// block byte-shape; the full inviter-signature crypto verify lands
// with the wrap loop (#984). Pass nil for the membership argument when
// inviting to a public channel — the server auto-fills the row.
func (c *Client) InviteChannelMember(
	ctx context.Context, channelID, inviteeUserID string, membership *MembershipBlockReq,
) (*ChannelMember, error) {
	path := fmt.Sprintf("/api/channels/%s/members", url.PathEscape(channelID))
	body := inviteRequest{UserID: inviteeUserID, Membership: membership}
	var out ChannelMember
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// KickChannelMember removes targetUserID from channelID. Self-leave is
// allowed (caller == target); kick-by-member is also allowed when the
// caller is a current member of the channel. The seeded #general
// channel is immutable per L8 — server returns 403.
func (c *Client) KickChannelMember(ctx context.Context, channelID, targetUserID string) error {
	path := fmt.Sprintf("/api/channels/%s/members/%s",
		url.PathEscape(channelID), url.PathEscape(targetUserID))
	return c.do(ctx, http.MethodDelete, path, nil, nil)
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
