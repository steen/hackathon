package http

import (
	"encoding/base64"
	"errors"
	stdhttp "net/http"
	"regexp"
	"strings"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/seed"
)

// Phase-10 wrap-validation error codes (decision-log L30 / L39 + §10).
// Surfaced from every wrap-insert handler in this PR; downstream
// (#984's standalone keys-RPC) reuses the same codes for parity.
const (
	// CodeSenderPubkeyMismatch — L30: wrap.sender_box_pubkey did not
	// match the caller's stored users.box_pubkey.
	CodeSenderPubkeyMismatch = "sender_pubkey_mismatch"
	// CodeWrapSizeInvalid — L39: wrap byte-length invariants failed
	// (wrapped_key != 48 OR nonce != 24 OR sender_box_pubkey != 32).
	CodeWrapSizeInvalid = "wrap_size_invalid"
	// CodeInvalidMembershipSignature — §10: inviter_signature did not
	// verify under inviter_sign_pubkey over the snakd-mship-v1: scope.
	CodeInvalidMembershipSignature = "invalid_membership_signature"
	// CodeWrapsAlreadySet — L6 / L12: POST /api/dms 200 path
	// received a non-empty root_key_wraps. DMs never rotate.
	CodeWrapsAlreadySet = "wraps_already_set"
)

// wrap byte-length constants (L39 wire-shape pins). pubkeyByteLen and
// the membership-signature byte length are defined in auth_handlers.go
// and members_handlers.go respectively; reuse those.
const (
	wrappedKeyByteLen     = 48
	wrapNonceByteLen      = 24
	creatorBootstrapGenID = 1
)

// channelNameRe pins the friend-group-safe shape: 1-40 chars, ASCII
// lowercase letters, digits, hyphens. Lowercase-only avoids the
// Slack-style "is #General the same as #general?" foot-gun. Drift
// against the CLI copy (apps/cli/cmd/channels.go) is guarded by
// TestChannelNameRegexMatchesServer.
var channelNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ChannelNameRe exposes channelNameRe to the cross-package drift test in
// apps/server/internal/http/channels_regex_drift_test.go
// (TestChannelNameRegexMatchesServer), which compares it against the
// CLI's equivalent. Aliased instead of inlined so the runtime validator
// and the test see the same compiled pattern.
var ChannelNameRe = channelNameRe

// ChannelsDeps is everything the channel handlers need wired in.
type ChannelsDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
	Now  func() time.Time
}

// ChannelsHandlers groups the http.HandlerFunc values for /api/channels
// and /api/channels/{id}/messages. Construct with NewChannelsHandlers
// and wire each method onto your mux.
type ChannelsHandlers struct {
	deps ChannelsDeps
}

// NewChannelsHandlers wires the dependency bag. Defaults Now to time.Now
// when unset so production callers do not have to think about clocks.
func NewChannelsHandlers(deps ChannelsDeps) *ChannelsHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &ChannelsHandlers{deps: deps}
}

// List handles GET /api/channels. Must be wrapped in auth.RequireJWT
// — the per-viewer read-state arm needs the authenticated user id for
// materialization + the JOIN into channel_reads (decision log §9 / §11).
func (h *ChannelsHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	uid, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	chans, err := h.deps.Repo.ListChannelsWithReadState(r.Context(), uid)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list channels")
		return
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"channels": chans})
}

// createChannelRequest is the wire body for POST /api/channels under
// Phase-10 (decision-log §7 + §10). The body now carries the §10
// self-signed `MembershipBlock` plus a single-entry `root_key_wraps`
// (the creator wrapping the channel root key to themselves under §6).
// Strict-decoded per L1 — DisallowUnknownFields catches typos at the
// edge.
//
// ChannelID is required on the §10 atomic-bootstrap path: the
// signature scope binds the channel id, so the caller must pick the
// id BEFORE signing. The legacy wraps-omitted path ignores
// ChannelID and lets the server pick, preserving the pre-Phase-10
// contract that older clients rely on.
type createChannelRequest struct {
	ChannelID    string                 `json:"channel_id,omitempty"`
	Name         string                 `json:"name"`
	IsPublic     bool                   `json:"is_public,omitempty"`
	Membership   *inviteMembershipBlock `json:"membership,omitempty"`
	RootKeyWraps []wrapEntryWire        `json:"root_key_wraps,omitempty"`
}

// wrapEntryWire is the WrapEntry wire shape (decision-log L5).
// recipient_user_id is required on every list with len > 1; on the
// /members endpoint the URL pins it and the field is omitted, but
// channel-create + DM-create lists carry it.
type wrapEntryWire struct {
	RecipientUserID string `json:"recipient_user_id,omitempty"`
	WrappedKey      string `json:"wrapped_key"`
	SenderBoxPubkey string `json:"sender_box_pubkey"`
	Nonce           string `json:"nonce"`
}

// decodedWrap holds the raw-bytes form of a wrapEntryWire after
// base64 + L39 byte-length validation has passed.
type decodedWrap struct {
	RecipientUserID string
	WrappedKey      []byte
	SenderBoxPubkey []byte
	Nonce           []byte
}

// Create handles POST /api/channels. Must be wrapped in auth.RequireJWT.
//
// Phase-10 (decision-log §7 + §10 self-bootstrap carve-out): the body
// MAY carry a self-signed `MembershipBlock` and a single-entry
// `root_key_wraps`. When BOTH are supplied, server validates the §10
// inviter-signature, the L30 sender pubkey ownership, and the L39
// wrap byte-lengths, then inserts the channel + the creator's
// `channel_members` row + the creator's wrap-to-self `channel_keys`
// row in ONE transaction (L7 invariant — member ⇔ wrap holds
// post-create). When BOTH are absent, the legacy bootstrap shape runs
// (membership row only, NULL signature; lazy-wrap-on-online supplies
// the wrap later — #984). Mixed shapes (one block but not the other)
// are rejected with 400 to avoid the half-bootstrapped state.
//
// `is_public` (L24) toggles the channels.is_public column. Public
// channels are server-readable; private channels gate access through
// the explicit membership relation.
func (h *ChannelsHandlers) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	var req createChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if !channelNameRe.MatchString(name) {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"channel name must be 1-40 chars: lowercase letters, digits, hyphens; must start with a letter or digit")
		return
	}
	caller, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	now := h.deps.Now()

	hasMembership := req.Membership != nil
	hasWraps := len(req.RootKeyWraps) > 0
	if hasMembership != hasWraps {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"membership and root_key_wraps must both be supplied (or both omitted)")
		return
	}

	if !hasMembership {
		// Legacy bootstrap path — kept so phase-1/2/9 e2e harnesses
		// continue to round-trip. Channel + member with NULL
		// signature, no wrap. Lazy-wrap-on-online (#984) fills the
		// wrap later. Behaviorally identical to the pre-PR Create.
		// Server picks the id since the legacy contract did not
		// surface it on the body.
		h.createLegacyBootstrap(w, r, ids.NewULID(), name, req.IsPublic, caller, now)
		return
	}

	// §10 atomic-bootstrap path — the signature is bound to the
	// channel id, so the caller MUST pick the id and put it in the
	// body. Server validates the shape only; uniqueness is enforced
	// by the channels.id PRIMARY KEY constraint inside the tx.
	id, idOK := ids.NormalizeChannelID(req.ChannelID)
	if !idOK {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"channel_id is required for the atomic-bootstrap shape; pass a fresh ULID the caller signed against")
		return
	}

	if len(req.RootKeyWraps) != 1 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"root_key_wraps must contain exactly 1 entry (creator wrap-to-self)")
		return
	}
	if rid := req.RootKeyWraps[0].RecipientUserID; rid != "" && rid != caller {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"root_key_wraps[0].recipient_user_id must equal caller (self-bootstrap)")
		return
	}
	wrap, werr := decodeWrapEntry(req.RootKeyWraps[0])
	if werr != nil {
		WriteError(w, stdhttp.StatusBadRequest, werr.Code, werr.Msg)
		return
	}
	// L30 — caller's box_pubkey must match the wrap's sender claim.
	exists, callerBox, callerSign, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), caller)
	if perr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load caller pubkeys")
		return
	}
	if !exists {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "caller user row not found")
		return
	}
	if !bytesEqualConstantTime(callerBox, wrap.SenderBoxPubkey) {
		WriteError(w, stdhttp.StatusBadRequest, CodeSenderPubkeyMismatch,
			"root_key_wraps[0].sender_box_pubkey does not match caller's box_pubkey (L30)")
		return
	}
	// Self-bootstrap §10 shape: inviter == invitee == caller; both
	// pinned pubkeys must equal the caller's stored pubkeys.
	row, sigBytes, addedAt, derr := buildSelfBootstrapRow(id, caller, callerBox, callerSign, req.Membership, now)
	if derr != nil {
		WriteError(w, stdhttp.StatusBadRequest, derr.Code, derr.Msg)
		return
	}
	// §10 signature verify.
	if err := auth.VerifyMembershipSignature(
		row.InviterSignPubkey, sigBytes,
		id, caller, caller,
		auth.InviteePubkeys{BoxPubkey: row.InviteeBoxPubkey, SignPubkey: row.InviteeSignPubkey},
		addedAt,
	); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeInvalidMembershipSignature,
			"membership.inviter_signature does not verify (§10 scope)")
		return
	}

	// Atomic transaction: channel + member + wrap (L7 invariant).
	tx, err := h.deps.Repo.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not begin transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(),
		`INSERT INTO channels(id, name, is_public, created_at) VALUES (?, ?, ?, ?)`,
		id, name, req.IsPublic, now.UTC(),
	); err != nil {
		if isChannelNameTakenSQLite(err) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel name already taken")
			return
		}
		if strings.Contains(err.Error(), "UNIQUE constraint failed") &&
			strings.Contains(err.Error(), "channels.id") {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel id already taken (pick a fresh ULID)")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create channel")
		return
	}
	if err := h.deps.Repo.InsertChannelMemberTx(r.Context(), tx, row, req.IsPublic); err != nil {
		switch {
		case errors.Is(err, repo.ErrPrivateChannelNullSignature):
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"membership block with non-empty inviter_signature is required for private channels")
		default:
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert membership")
		}
		return
	}
	if err := h.deps.Repo.InsertChannelKeyTx(r.Context(), tx, repo.ChannelKey{
		ChannelID:       id,
		GenerationID:    creatorBootstrapGenID,
		MemberUserID:    caller,
		WrappedKey:      wrap.WrappedKey,
		SenderBoxPubkey: wrap.SenderBoxPubkey,
		Nonce:           wrap.Nonce,
		CreatedAt:       now,
	}); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert wrap")
		return
	}
	if err := tx.Commit(); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not commit transaction")
		return
	}

	flag := req.IsPublic
	ch := &repo.Channel{ID: id, Name: name, CreatedAt: now.UTC(), IsPublic: &flag}
	h.broadcastChannelEvent(channelEventCreate, *ch)
	WriteOK(w, stdhttp.StatusCreated, ch)
}

// createLegacyBootstrap is the wraps-omitted path — channel +
// NULL-signature member row, no wrap. Kept so phase-1/2/9 harnesses
// continue to round-trip while #984's lazy-wrap loop hasn't shipped.
// On a private channel this leaves a row with NULL inviter_signature,
// which the L33 repo guard rejects on every other invite path so
// nothing stale sneaks in elsewhere.
func (h *ChannelsHandlers) createLegacyBootstrap(
	w stdhttp.ResponseWriter, r *stdhttp.Request,
	id, name string, isPublic bool, caller string, now time.Time,
) {
	ch, err := h.deps.Repo.CreateChannel(r.Context(), id, name, isPublic, now)
	if err != nil {
		if errors.Is(err, repo.ErrChannelNameTaken) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel name already taken")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create channel")
		return
	}
	_, boxPub, signPub, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), caller)
	if perr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load creator pubkeys")
		return
	}
	if len(boxPub) == 0 {
		boxPub = make([]byte, 32)
	}
	if len(signPub) == 0 {
		signPub = make([]byte, 32)
	}
	row := repo.ChannelMember{
		ChannelID:         id,
		UserID:            caller,
		InviterUserID:     caller,
		InviterSignPubkey: signPub,
		InviteeBoxPubkey:  boxPub,
		InviteeSignPubkey: signPub,
		AddedAt:           now,
	}
	if err := h.deps.Repo.InsertChannelMember(r.Context(), row, true); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not bootstrap creator membership")
		return
	}
	h.broadcastChannelEvent(channelEventCreate, *ch)
	WriteOK(w, stdhttp.StatusCreated, ch)
}

// wrapDecodeError carries the (status-code, message) pair callers
// surface as-is when WrapEntry decode fails.
type wrapDecodeError struct {
	Code string
	Msg  string
}

func (e *wrapDecodeError) Error() string { return e.Msg }

// decodeWrapEntry runs the L39 byte-length checks AFTER base64 decode.
// Returns the raw-bytes form on success.
func decodeWrapEntry(w wrapEntryWire) (decodedWrap, *wrapDecodeError) {
	wrapped, err := base64.StdEncoding.DecodeString(w.WrappedKey)
	if err != nil {
		return decodedWrap{}, &wrapDecodeError{Code: CodeBadRequest, Msg: "wrapped_key must be base64-encoded"}
	}
	sender, err := base64.StdEncoding.DecodeString(w.SenderBoxPubkey)
	if err != nil {
		return decodedWrap{}, &wrapDecodeError{Code: CodeBadRequest, Msg: "sender_box_pubkey must be base64-encoded"}
	}
	nonce, err := base64.StdEncoding.DecodeString(w.Nonce)
	if err != nil {
		return decodedWrap{}, &wrapDecodeError{Code: CodeBadRequest, Msg: "nonce must be base64-encoded"}
	}
	if len(wrapped) != wrappedKeyByteLen {
		return decodedWrap{}, &wrapDecodeError{Code: CodeWrapSizeInvalid,
			Msg: "wrapped_key must decode to 48 bytes"}
	}
	if len(nonce) != wrapNonceByteLen {
		return decodedWrap{}, &wrapDecodeError{Code: CodeWrapSizeInvalid,
			Msg: "nonce must decode to 24 bytes"}
	}
	if len(sender) != pubkeyByteLen {
		return decodedWrap{}, &wrapDecodeError{Code: CodeWrapSizeInvalid,
			Msg: "sender_box_pubkey must decode to 32 bytes"}
	}
	return decodedWrap{
		RecipientUserID: w.RecipientUserID,
		WrappedKey:      wrapped,
		SenderBoxPubkey: sender,
		Nonce:           nonce,
	}, nil
}

// buildSelfBootstrapRow returns the §10 self-bootstrap row (channel
// creator wrapping themselves), the raw inviter_signature bytes, and
// the parsed added_at timestamp. Validates every cross-reference per
// specs/plans/phase-10/membership.md "POST /api/channels":
//
//   - membership.inviter_user_id == caller
//   - membership.inviter_sign_pubkey == caller's stored sign_pubkey
//   - membership.invitee_box_pubkey == caller's stored box_pubkey
//   - membership.invitee_sign_pubkey == caller's stored sign_pubkey
//   - inviter_signature is non-empty (the public-channel NULL-sig
//     carve-out applies only to the registration auto-add path).
func buildSelfBootstrapRow(
	channelID, caller string, callerBox, callerSign []byte,
	mb *inviteMembershipBlock, now time.Time,
) (repo.ChannelMember, []byte, time.Time, *wrapDecodeError) {
	if mb.InviterUserID != caller {
		return repo.ChannelMember{}, nil, time.Time{},
			&wrapDecodeError{Code: CodeBadRequest, Msg: "membership.inviter_user_id must equal caller (self-bootstrap)"}
	}
	signPub, err := decodeMembershipPubkey("inviter_sign_pubkey", mb.InviterSignPubkey)
	if err != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: err.Error()}
	}
	boxPub, err := decodeMembershipPubkey("invitee_box_pubkey", mb.InviteeBoxPubkey)
	if err != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: err.Error()}
	}
	inviteeSign, err := decodeMembershipPubkey("invitee_sign_pubkey", mb.InviteeSignPubkey)
	if err != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: err.Error()}
	}
	if !bytesEqualConstantTime(callerSign, signPub) {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "membership.inviter_sign_pubkey does not match caller's stored sign_pubkey",
		}
	}
	if !bytesEqualConstantTime(callerBox, boxPub) {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "membership.invitee_box_pubkey does not match caller's stored box_pubkey",
		}
	}
	if !bytesEqualConstantTime(callerSign, inviteeSign) {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "membership.invitee_sign_pubkey does not match caller's stored sign_pubkey (self-bootstrap)",
		}
	}
	added, terr := parseRFC3339Either(mb.AddedAt)
	if terr != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: terr.Error()}
	}
	sig, sigErr := decodeMembershipSignature(mb.InviterSignature, false)
	if sigErr != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: sigErr.Error()}
	}
	if len(sig) == 0 {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "inviter_signature is required for POST /api/channels self-bootstrap",
		}
	}
	row := repo.ChannelMember{
		ChannelID:         channelID,
		UserID:            caller,
		InviterUserID:     caller,
		InviterSignPubkey: signPub,
		InviterSignature:  sig,
		InviteeBoxPubkey:  boxPub,
		InviteeSignPubkey: inviteeSign,
		AddedAt:           added,
	}
	_ = now
	return row, sig, added, nil
}

// parseRFC3339Either accepts both the nano + integer-second forms of
// RFC3339 — Go's default time.Time.Format emits the integer form, but
// JS toISOString and most TS clients emit the nano form. Accepting
// both keeps the contract symmetric.
func parseRFC3339Either(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("membership.added_at must be RFC3339")
}

// bytesEqualConstantTime is a tiny constant-time equality wrapper for
// pubkey comparisons. crypto/subtle.ConstantTimeCompare returns int 0
// or 1 — wrap it so the call site reads as a bool predicate.
func bytesEqualConstantTime(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// isChannelNameTakenSQLite mirrors the package-private sniff in
// repo/channels.go. We can't import the unexported function and the
// repo's CreateChannel doesn't expose a Tx variant; the inline INSERT
// inside the L7 transaction is the path that keeps the channel,
// member, and wrap rows atomic.
func isChannelNameTakenSQLite(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, "channels.name")
}

// Rename handles PATCH /api/channels/{id}. Must be wrapped in
// auth.RequireJWT (and the per-user write limiter from wiring/channels.go).
//
// `#general` is non-renamable: the seeded channel is the default landing
// channel hard-coded in the smoke script and the README, so renaming it
// would silently break those entry points (PRD §10, spec
// 10-feature-channel-rename-endpoint.md). The check resolves the target
// by id then refuses by current name — so reseeding on a fresh DB still
// protects "the channel currently named general".
func (h *ChannelsHandlers) Rename(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPatch {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	rawID := r.PathValue("id")
	id, ok := ids.NormalizeChannelID(rawID)
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if !channelNameRe.MatchString(name) {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"channel name must be 1-40 chars: lowercase letters, digits, hyphens; must start with a letter or digit")
		return
	}
	current, err := h.deps.Repo.GetChannel(r.Context(), id)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if current == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}
	if current.Name == seed.GeneralChannelName {
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "the general channel cannot be renamed")
		return
	}
	updated, err := h.deps.Repo.RenameChannel(r.Context(), id, name, h.deps.Now())
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrChannelNotFound):
			WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		case errors.Is(err, repo.ErrChannelNameTaken):
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel name already taken")
		default:
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not rename channel")
		}
		return
	}
	h.broadcastChannelEvent(channelEventRename, *updated)
	WriteOK(w, stdhttp.StatusOK, updated)
}

// broadcastChannelEvent fans out a {type:"channel", data:{kind, channel}}
// frame to every connected WS client via Hub.BroadcastAll. Channel
// listings are a global concern — every connected client must update its
// sidebar — so the per-channel Broadcast path is the wrong fan-out (a
// rename to a channel a client is not subscribed to would not reach
// them).
//
// Hub may be nil in test fixtures that exercise the handler without a
// hub; the call is a no-op in that case.
func (h *ChannelsHandlers) broadcastChannelEvent(kind string, ch repo.Channel) {
	if h.deps.Hub == nil {
		return
	}
	frame := channelEventFrame(kind, ch)
	if frame == nil {
		return
	}
	h.deps.Hub.BroadcastAll(frame)
}

// Routes registers /api/channels and /api/channels/{id}/messages on mux,
// wrapping every handler in the supplied auth middleware. Path-pattern
// matching uses Go 1.22+ ServeMux placeholders (`{id}`); handlers read
// the channel id with r.PathValue("id").
//
// writeLimit, when non-nil, is applied to the channel-write surface
// (POST + PATCH /api/channels) inside the JWT chain so the per-user
// limiter sees the authenticated user id. PRD §9. Pass nil from tests
// that don't exercise rate-limiting; production wiring always supplies
// it.
func (h *ChannelsHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	writeLimit func(stdhttp.Handler) stdhttp.Handler,
	messages *MessagesHandlers,
) {
	wrapWrite := func(handler stdhttp.Handler) stdhttp.Handler {
		if writeLimit != nil {
			handler = writeLimit(handler)
		}
		return require(handler)
	}
	mux.Handle("GET /api/channels", require(stdhttp.HandlerFunc(h.List)))
	mux.Handle("POST /api/channels", wrapWrite(stdhttp.HandlerFunc(h.Create)))
	mux.Handle("PATCH /api/channels/{id}", wrapWrite(stdhttp.HandlerFunc(h.Rename)))
	mux.Handle("GET /api/channels/{id}/messages", require(stdhttp.HandlerFunc(messages.List)))
	mux.Handle("POST /api/channels/{id}/messages", require(stdhttp.HandlerFunc(messages.Create)))
}

// channelIDFromPath returns the id segment of /api/channels/{id}/messages,
// validated as a 26-char ULID-ish token. Tightly scoped so a typo in the
// route pattern surfaces as a 400 rather than a SQL lookup with untrusted
// input. Normalization (upper-fold + alphabet check) lives in
// `internal/ids` so the WS handler folds the same way (audit #78, info).
func channelIDFromPath(r *stdhttp.Request) (string, bool) {
	return ids.NormalizeChannelID(r.PathValue("id"))
}

// userFromContext returns the authenticated user id from RequireJWT's
// context values. Callers that also need the display name should call
// auth.UsernameFromContext directly.
func userFromContext(r *stdhttp.Request) (string, bool) {
	return auth.UserIDFromContext(r.Context())
}
