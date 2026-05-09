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

// MessageEnvelope is the encrypted-message wire shape from Phase 10
// (decision-log L21, specs/plans/phase-10/encryption.md). The TS mirror
// is `MessageEnvelope` in packages/api-client/src/types.ts; the Go-side
// name avoids colliding with the unexported response wrapper `envelope`
// in client.go and matches the TS-side rename (which dodges the existing
// `Envelope<T>` response wrapper).
//
// CipherSuite = 0x01 is the only suite in v1 (naclbox-v1 — decision §3).
// Signature is Ed25519 over the snakd-msg-v1:{channel,dm}: scope from L21.
// ClientCreatedAt is signed; the parent CreatedAt on Message/DMMessage is
// server-stamped and unsigned (display-only).
type MessageEnvelope struct {
	CipherSuite      uint8  `json:"cipher_suite"`
	KeyGenerationID  uint32 `json:"key_generation_id"`
	Nonce            string `json:"nonce"`
	Ciphertext       string `json:"ciphertext"`
	SenderSignPubkey string `json:"sender_sign_pubkey"`
	Signature        string `json:"signature"`
	ClientCreatedAt  string `json:"client_created_at"`
}

// WrapEntry is the per-recipient root-key wrap that travels on every
// wrap-carrying endpoint (decision-log L5 + §7). RecipientUserID is
// omitted when the wrap-list singularity already pins the recipient
// (e.g. POST /api/channels/{id}/members carries a single root_key_wrap
// keyed by the path). WrappedKey is base64 of crypto_box ciphertext (48
// bytes after MAC). Nonce is base64 of 24 random bytes (XSalsa20).
// SenderBoxPubkey is base64 of 32 raw bytes — the wrapper's box_pubkey
// at wrap time (server-validated against users.box_pubkey for the caller
// per L30).
type WrapEntry struct {
	RecipientUserID string `json:"recipient_user_id,omitempty"`
	WrappedKey      string `json:"wrapped_key"`
	SenderBoxPubkey string `json:"sender_box_pubkey"`
	Nonce           string `json:"nonce"`
}

// MembershipBlock is the inviter-signed channel-membership row from
// decision-log §10 + L22. Travels on POST /api/channels,
// POST /api/channels/{id}/members, the replay-wrap endpoint, and as the
// `membership` field of every entry in the wraps-needed response.
//
// InviterSignature is Ed25519 over the snakd-mship-v1: scope. Pointer
// for tri-state nullability — null only for the public-channel
// server-auto-add carve-out (R1.2 residual, see security.md);
// private-channel rows must carry a non-null signature (L33
// application-level enforcement in repo/channel_members.go).
type MembershipBlock struct {
	InviterUserID     string  `json:"inviter_user_id"`
	InviterSignPubkey string  `json:"inviter_sign_pubkey"`
	InviteeBoxPubkey  string  `json:"invitee_box_pubkey"`
	InviteeSignPubkey string  `json:"invitee_sign_pubkey"`
	AddedAt           string  `json:"added_at"`
	InviterSignature  *string `json:"inviter_signature"`
}

// Message mirrors the server-side repo.Message JSON shape (PRD §10).
//
// Envelope is the Phase-10 encrypted-message envelope (L21). Pointer for
// optional under the L26 optional-first rule; #983 narrows it to
// non-optional once every consumer of `.body` is migrated to
// decrypt-from-envelope (Wave 5 / E).
type Message struct {
	ID           ULID             `json:"id"`
	ChannelID    ULID             `json:"channel_id"`
	SenderUserID ULID             `json:"sender_user_id"`
	Body         string           `json:"body"`
	CreatedAt    time.Time        `json:"created_at"`
	Envelope     *MessageEnvelope `json:"envelope,omitempty"`
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
