package http

import (
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"time"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// MaxMessageBodyBytes is the historical 4 KiB plaintext-body cap from
// PRD §9. Phase-10 enforces it client-side BEFORE encryption (decision-
// log L17 — server cannot inspect plaintext); the server-visible
// ciphertext layer carries an independent ceiling.
const MaxMessageBodyBytes = 4 * 1024

// MaxCiphertextBytes caps the ciphertext payload of an inbound encrypted
// envelope. The 4 KiB plaintext cap (MaxMessageBodyBytes) plus a little
// headroom for secretbox MAC + future expansion gives 16 KiB; the
// existing REST 16 KiB request-body cap from PRD §9 enforces the outer
// shape independently. Decision-log L17 ("body cap").
const MaxCiphertextBytes = 16 * 1024

// CipherSuiteNaclbox is the only suite supported in v1 (decision-log
// §3 + L17). Inbound envelopes whose cipher_suite differs are rejected
// with 400 + CodeBadRequest at the handler boundary.
const CipherSuiteNaclbox uint8 = 0x01

// Envelope structural-byte counts for the L17 + L21 + L39 length
// validation. Exported so cross-package tests can assert against the
// same constants.
const (
	EnvelopeNonceBytes            = 24
	EnvelopeSenderSignPubkeyBytes = 32
	EnvelopeSignatureBytes        = 64
)

// WSEventMessage is the outbound WS frame type for a new message.
// Mirrors PRD §10's `{"type": "message", "data": <Message>}` shape so
// CLI/web clients can branch on the type field.
const WSEventMessage = "message"

// MessagesDeps mirrors ChannelsDeps; held separately so the two
// handlers can be wired without forcing a single fat constructor.
type MessagesDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
	Now  func() time.Time
}

// MessagesHandlers exposes List + Create for the per-channel messages
// endpoints. Both methods require the route to carry an `{id}` value.
type MessagesHandlers struct {
	deps MessagesDeps
}

// NewMessagesHandlers wires the dependency bag. Defaults Now to time.Now
// when unset so production callers do not have to think about clocks.
func NewMessagesHandlers(deps MessagesDeps) *MessagesHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &MessagesHandlers{deps: deps}
}

// List handles GET /api/channels/{id}/messages?before=&limit=. Returns
// newest-first paginated history. Caps limit at MaxMessagesLimit.
func (h *MessagesHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	channelID, ok := channelIDFromPath(r)
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	ch, err := h.deps.Repo.GetChannel(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if ch == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}

	q := r.URL.Query()
	limit := repo.DefaultMessagesLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	before := q.Get("before")
	if before != "" {
		normalized, ok := validULID(before)
		if !ok {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "before must be a ULID")
			return
		}
		before = normalized
	}
	msgs, err := h.deps.Repo.ListMessages(r.Context(), channelID, before, limit)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list messages")
		return
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"messages": msgs})
}

// Create handles POST /api/channels/{id}/messages. Persists the row
// then broadcasts the persisted record (with its assigned ULID and
// timestamp) onto the hub channel keyed by channel id. Order matters:
// insert first so a broadcast cannot deliver a message that no later
// history fetch would return.
func (h *MessagesHandlers) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	channelID, ok := channelIDFromPath(r)
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	uid, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	ch, err := h.deps.Repo.GetChannel(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if ch == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}
	var req struct {
		Envelope repo.MessageEnvelope `json:"envelope"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	if err := validateInboundEnvelope(req.Envelope); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	id := ids.NewULID()
	msg, err := h.deps.Repo.InsertMessageTx(r.Context(), id, channelID, uid, req.Envelope, h.deps.Now())
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert message")
		return
	}
	if h.deps.Hub != nil {
		if frame, err := json.Marshal(map[string]interface{}{
			"type": WSEventMessage,
			"data": msg,
		}); err == nil {
			h.deps.Hub.Broadcast(channelID, frame)
		}
	}
	WriteOK(w, stdhttp.StatusCreated, msg)
}

// errEnvelopeInvalid is returned by validateInboundEnvelope when the
// envelope fails L17 / L21 / L39 structural validation. The message
// surfaces in the 400 response.
var errEnvelopeInvalid = errors.New("envelope is invalid")

// validateInboundEnvelope runs the L17 + L21 + L39 structural checks the
// server can do without a key (the server cannot decrypt — receivers
// verify signature + secretbox client-side). Each branch returns a
// caller-facing message that is safe to surface in the error envelope.
//
// Reject reasons:
//   - cipher_suite != 0x01 (L17 downgrade defense — only naclbox-v1 in v1)
//   - nonce length != 24 (L17 + L21 — XSalsa20 nonce is 24 raw bytes)
//   - sender_sign_pubkey length != 32 (L39 — Ed25519 pubkey is 32 bytes)
//   - signature length != 64 (L39 — Ed25519 signature is 64 bytes)
//   - ciphertext empty or > MaxCiphertextBytes (L17 body cap)
//   - client_created_at zero (must be set; clients stamp before signing)
//
// L17 also forbids any plaintext-fallback frame; that's enforced by the
// handler's strict-decoded request shape — `body` is no longer a field.
func validateInboundEnvelope(env repo.MessageEnvelope) error {
	if env.CipherSuite != CipherSuiteNaclbox {
		return fmt.Errorf("%w: cipher_suite must be %d (naclbox-v1)", errEnvelopeInvalid, CipherSuiteNaclbox)
	}
	if len(env.Nonce) != EnvelopeNonceBytes {
		return fmt.Errorf("%w: nonce must be %d bytes", errEnvelopeInvalid, EnvelopeNonceBytes)
	}
	if len(env.SenderSignPubkey) != EnvelopeSenderSignPubkeyBytes {
		return fmt.Errorf("%w: sender_sign_pubkey must be %d bytes", errEnvelopeInvalid, EnvelopeSenderSignPubkeyBytes)
	}
	if len(env.Signature) != EnvelopeSignatureBytes {
		return fmt.Errorf("%w: signature must be %d bytes", errEnvelopeInvalid, EnvelopeSignatureBytes)
	}
	if len(env.Ciphertext) == 0 {
		return fmt.Errorf("%w: ciphertext must not be empty", errEnvelopeInvalid)
	}
	if len(env.Ciphertext) > MaxCiphertextBytes {
		return fmt.Errorf("%w: ciphertext exceeds %d-byte cap", errEnvelopeInvalid, MaxCiphertextBytes)
	}
	if env.ClientCreatedAt.IsZero() {
		return fmt.Errorf("%w: client_created_at must be set", errEnvelopeInvalid)
	}
	return nil
}

// validULID is a cheap shape check (Crockford base32, 26 chars). Lowercase
// input is accepted and upper-folded on return so SQLite's BINARY collation
// matches server-issued (uppercase) ULIDs in cursor comparisons and id
// lookups.
func validULID(s string) (string, bool) {
	if len(s) != 26 {
		return "", false
	}
	b := make([]byte, 26)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			b[i] = c
		case c >= 'A' && c <= 'Z':
			b[i] = c
		case c >= 'a' && c <= 'z':
			b[i] = c - ('a' - 'A')
		default:
			return "", false
		}
	}
	return string(b), true
}
