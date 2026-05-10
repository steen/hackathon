package http

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/seed"
)

// MembersDeps wires the channel-membership handlers. Held separately
// from ChannelsDeps so the membership feature owns its own register
// function (one wiring/<feature>.go file per CLAUDE.md).
type MembersDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
	Now  func() time.Time
}

// MembersHandlers groups the http.HandlerFunc values for the
// /api/channels/{id}/members surface. Constructed once via
// NewMembersHandlers and wired through Routes.
type MembersHandlers struct {
	deps MembersDeps
}

// NewMembersHandlers wires the dependency bag. Now defaults to time.Now
// so production callers do not have to think about clocks.
func NewMembersHandlers(deps MembersDeps) *MembersHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &MembersHandlers{deps: deps}
}

// memberWire is the JSON shape returned for one channel_members row.
// Pubkey blobs are base64-encoded so the same shape works for the wire
// and for log lines that may need the values without re-binding the DB.
type memberWire struct {
	UserID            string    `json:"user_id"`
	InviterUserID     string    `json:"inviter_user_id"`
	InviterSignPubkey string    `json:"inviter_sign_pubkey"`
	InviterSignature  string    `json:"inviter_signature,omitempty"`
	InviteeBoxPubkey  string    `json:"invitee_box_pubkey"`
	InviteeSignPubkey string    `json:"invitee_sign_pubkey"`
	AddedAt           time.Time `json:"added_at"`
	Username          string    `json:"username,omitempty"`
}

// ListMembers handles GET /api/channels/{id}/members. Returns the
// member list when the caller is a member of the channel; 403 otherwise
// (decision-log §6 — non-members must not see the membership graph).
func (h *MembersHandlers) ListMembers(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	channelID, ok := ids.NormalizeChannelID(r.PathValue("id"))
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	uid, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	exists, err := h.deps.Repo.ChannelExists(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if !exists {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}
	isMember, err := h.deps.Repo.IsMember(r.Context(), channelID, uid)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not check membership")
		return
	}
	if !isMember {
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "not a member of this channel")
		return
	}
	rows, err := h.deps.Repo.ListChannelMembers(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list members")
		return
	}
	usernames, err := lookupUsernamesForMembers(r.Context(), h.deps.Repo.DB(), rows)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load usernames")
		return
	}
	out := make([]memberWire, 0, len(rows))
	for _, m := range rows {
		out = append(out, encodeMember(m, usernames[m.UserID]))
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"members": out})
}

// inviteRequest is the body for POST /api/channels/{id}/members. The
// `membership` block carries the §10 inviter-signature payload; the
// `root_key_wrap` is the singleton WrapEntry for the invitee
// (recipient_user_id is implicit — the URL pins it). Strict-decoded
// per L1 (DisallowUnknownFields).
//
// Public-channel exception: when membership AND root_key_wrap are both
// omitted on a channel with is_public=TRUE, the server-auto-fill path
// runs (NULL signature, no wrap row inserted; lazy-wrap-on-online
// supplies the wrap later). Private channels require both blocks and
// reject with 400.
type inviteRequest struct {
	UserID      string                 `json:"user_id"`
	Membership  *inviteMembershipBlock `json:"membership,omitempty"`
	RootKeyWrap *wrapEntryWire         `json:"root_key_wrap,omitempty"`
}

type inviteMembershipBlock struct {
	InviterUserID     string `json:"inviter_user_id"`
	InviterSignPubkey string `json:"inviter_sign_pubkey"`
	InviteeBoxPubkey  string `json:"invitee_box_pubkey"`
	InviteeSignPubkey string `json:"invitee_sign_pubkey"`
	AddedAt           string `json:"added_at"`
	InviterSignature  string `json:"inviter_signature"`
}

// Invite handles POST /api/channels/{id}/members. Inserts a
// `channel_members` row when the caller is a current member; for
// private channels the body must carry a §10-signed membership block
// AND a `root_key_wrap` (atomic invariant L7 — member ⇔ wrap). Public
// channels accept the bare `{user_id}` shape and run the
// server-auto-fill path (NULL signature, no wrap; lazy-wrap-on-online
// fills the wrap later).
//
// On a private-channel invite this handler:
//
//  1. Verifies caller is a member of the channel (403 otherwise).
//  2. Verifies invitee exists (404 otherwise).
//  3. Decodes the membership block (caller-supplied byte shape +
//     L39 byte-length checks on every pubkey + signature).
//  4. Validates the §10 cross-references (inviter_user_id == caller;
//     inviter_sign_pubkey == users.sign_pubkey WHERE id = caller;
//     invitee_*_pubkey == users.*_pubkey WHERE id = invitee).
//  5. Verifies the inviter_signature under the §10 scope.
//  6. Decodes + L30/L39-validates the root_key_wrap.
//  7. Inserts member + wrap rows in ONE transaction (L7).
func (h *MembersHandlers) Invite(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	channelID, ok := ids.NormalizeChannelID(r.PathValue("id"))
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	caller, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	var req inviteRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	if req.UserID == "" {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "user_id is required")
		return
	}
	channel, err := h.deps.Repo.GetChannel(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if channel == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}
	isCallerMember, err := h.deps.Repo.IsMember(r.Context(), channelID, caller)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not check membership")
		return
	}
	if !isCallerMember {
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "not a member of this channel")
		return
	}
	inviteeExists, inviteeBox, inviteeSign, err := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), req.UserID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load invitee")
		return
	}
	if !inviteeExists {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "invitee not found")
		return
	}
	channelIsPublic := channel.IsPublic != nil && *channel.IsPublic
	now := h.deps.Now()

	// Public-channel auto-fill path: the body omits both blocks. The
	// repo NULL-sig carve-out (L33) accepts; no wrap is inserted, the
	// L7 invariant is restored when lazy-wrap-on-online fills the wrap
	// later (#984). Private channels reject this shape.
	if req.Membership == nil && req.RootKeyWrap == nil {
		if !channelIsPublic {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"membership block + root_key_wrap are required for private channels")
			return
		}
		_, callerBoxPub, callerSignPub, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), caller)
		if perr != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load caller pubkeys")
			return
		}
		row := repo.ChannelMember{
			ChannelID:         channelID,
			UserID:            req.UserID,
			InviterUserID:     caller,
			InviterSignPubkey: zeroFillPubkey(callerSignPub),
			InviteeBoxPubkey:  zeroFillPubkey(inviteeBox),
			InviteeSignPubkey: zeroFillPubkey(inviteeSign),
			AddedAt:           now,
		}
		_ = callerBoxPub
		if err := h.deps.Repo.InsertChannelMember(r.Context(), row, true); err != nil {
			if errors.Is(err, repo.ErrChannelMemberAlreadyExists) {
				WriteError(w, stdhttp.StatusConflict, CodeConflict, "user is already a member of channel")
				return
			}
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert membership")
			return
		}
		h.broadcastMembersChanged(channelID)
		WriteOK(w, stdhttp.StatusCreated, encodeMember(row, ""))
		return
	}

	// Private (or signed-public) path: both blocks are required.
	if req.Membership == nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"membership block is required when root_key_wrap is provided")
		return
	}
	if req.RootKeyWrap == nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"root_key_wrap is required when membership block is provided")
		return
	}
	row, sigBytes, addedAt, derr := buildInviteMembershipRow(
		channelID, req.UserID, caller, inviteeBox, inviteeSign, req.Membership, now,
	)
	if derr != nil {
		WriteError(w, stdhttp.StatusBadRequest, derr.Code, derr.Msg)
		return
	}
	// Caller's stored sign_pubkey must match the membership pin.
	_, callerBoxPub, callerSignPub, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), caller)
	if perr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load caller pubkeys")
		return
	}
	if !bytesEqualConstantTime(callerSignPub, row.InviterSignPubkey) {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"membership.inviter_sign_pubkey does not match caller's stored sign_pubkey")
		return
	}
	if err := auth.VerifyMembershipSignature(
		row.InviterSignPubkey, sigBytes,
		channelID, caller, req.UserID,
		auth.InviteePubkeys{BoxPubkey: row.InviteeBoxPubkey, SignPubkey: row.InviteeSignPubkey},
		addedAt,
	); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeInvalidMembershipSignature,
			"membership.inviter_signature does not verify (§10 scope)")
		return
	}
	wrap, werr := decodeWrapEntry(*req.RootKeyWrap)
	if werr != nil {
		WriteError(w, stdhttp.StatusBadRequest, werr.Code, werr.Msg)
		return
	}
	if !bytesEqualConstantTime(callerBoxPub, wrap.SenderBoxPubkey) {
		WriteError(w, stdhttp.StatusBadRequest, CodeSenderPubkeyMismatch,
			"root_key_wrap.sender_box_pubkey does not match caller's box_pubkey (L30)")
		return
	}

	tx, err := h.deps.Repo.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not begin transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	currentGen, hasGen, err := h.deps.Repo.MaxChannelKeyGenerationTx(r.Context(), tx, channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not resolve generation")
		return
	}
	if !hasGen {
		// Channel was created via the legacy wraps-omitted bootstrap
		// path (membership-only, no creator wrap). Treat the first
		// wrap-carrying invite as the implicit gen 1; subsequent
		// invites stay at gen 1 because rotation only fires on
		// member removal (L16). #984's keys-RPC will tighten this
		// when the bootstrap mode lands; this fallback keeps L7
		// recoverable for channels created before the wrap loop.
		currentGen = creatorBootstrapGenID
	}
	if err := h.deps.Repo.InsertChannelMemberTx(r.Context(), tx, row, channelIsPublic); err != nil {
		switch {
		case errors.Is(err, repo.ErrPrivateChannelNullSignature):
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"membership block with non-empty inviter_signature is required for private channels")
		case errors.Is(err, repo.ErrChannelMemberAlreadyExists):
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "user is already a member of channel")
		default:
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert membership")
		}
		return
	}
	if err := h.deps.Repo.InsertChannelKeyTx(r.Context(), tx, repo.ChannelKey{
		ChannelID:       channelID,
		GenerationID:    currentGen,
		MemberUserID:    req.UserID,
		WrappedKey:      wrap.WrappedKey,
		SenderBoxPubkey: wrap.SenderBoxPubkey,
		Nonce:           wrap.Nonce,
		CreatedAt:       now,
	}); err != nil {
		if errors.Is(err, repo.ErrChannelKeyAlreadyExists) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "wrap already exists for invitee at current generation")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert wrap")
		return
	}
	if err := tx.Commit(); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not commit transaction")
		return
	}
	h.broadcastMembersChanged(channelID)
	WriteOK(w, stdhttp.StatusCreated, encodeMember(row, ""))
}

// zeroFillPubkey returns p when it is 32 bytes; otherwise a 32-byte
// zero buffer. The schema requires NOT NULL on the pubkey columns;
// pre-Phase-10 rows may have NULL/empty pubkeys (decision-log L26)
// and substituting a zero buffer keeps the constraint satisfied for
// the public-channel auto-fill path. The wrap loop in #984 looks up
// pubkeys live (not from this row) so the placeholder is structural,
// not a key claim.
func zeroFillPubkey(p []byte) []byte {
	if len(p) == pubkeyByteLen {
		return p
	}
	return make([]byte, pubkeyByteLen)
}

// Kick handles DELETE /api/channels/{id}/members/{user_id}. The caller
// must be a current member of the channel (or the user themselves on
// the self-leave path). #general is immutable per L8 — the handler
// returns 403 when channel.name == seed.GeneralChannelName, regardless
// of whether the request is a kick or a self-leave.
func (h *MembersHandlers) Kick(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodDelete {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	channelID, ok := ids.NormalizeChannelID(r.PathValue("id"))
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid channel id")
		return
	}
	target := r.PathValue("user_id")
	if target == "" {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "missing user_id path segment")
		return
	}
	caller, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	channel, err := h.deps.Repo.GetChannel(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load channel")
		return
	}
	if channel == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "channel not found")
		return
	}
	if channel.Name == seed.GeneralChannelName {
		// L8 — #general membership is immutable. Self-leave is also
		// blocked so the channel never loses its baseline contract.
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "the general channel membership cannot be modified")
		return
	}
	if caller != target {
		// Only members can kick. Self-leave is allowed for any
		// authenticated user (their own row).
		isCallerMember, err := h.deps.Repo.IsMember(r.Context(), channelID, caller)
		if err != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not check membership")
			return
		}
		if !isCallerMember {
			WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "not a member of this channel")
			return
		}
	}
	deleted, err := h.deps.Repo.DeleteChannelMember(r.Context(), channelID, target)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not delete membership")
		return
	}
	if !deleted {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "membership not found")
		return
	}
	h.broadcastMembersChanged(channelID)
	w.WriteHeader(stdhttp.StatusNoContent)
}

// Routes registers the membership endpoints. require is the JWT
// middleware; writeLimit, when non-nil, throttles invite + kick under
// the same per-user bucket as channel-write so a flood of invites
// shares the bucket with a flood of channel renames.
func (h *MembersHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	writeLimit func(stdhttp.Handler) stdhttp.Handler,
) {
	wrapWrite := func(handler stdhttp.Handler) stdhttp.Handler {
		if writeLimit != nil {
			handler = writeLimit(handler)
		}
		return require(handler)
	}
	mux.Handle("GET /api/channels/{id}/members", require(stdhttp.HandlerFunc(h.ListMembers)))
	mux.Handle("POST /api/channels/{id}/members", wrapWrite(stdhttp.HandlerFunc(h.Invite)))
	mux.Handle("DELETE /api/channels/{id}/members/{user_id}", wrapWrite(stdhttp.HandlerFunc(h.Kick)))
}

// broadcastMembersChanged emits the L9 {type:"channel",
// data:{kind:"members_changed", channel_id, current_generation_id,
// members_at_rotation}} frame on every successful invite + kick.
// current_generation_id is hardcoded to 1 in this PR; #984 wires it to
// the actual channel_keys generation. Hub may be nil in tests.
func (h *MembersHandlers) broadcastMembersChanged(channelID string) {
	if h.deps.Hub == nil {
		return
	}
	frame, err := json.Marshal(map[string]interface{}{
		"type": WSEventChannel,
		"data": map[string]interface{}{
			"kind":                  "members_changed",
			"channel_id":            channelID,
			"current_generation_id": 1,
			"members_at_rotation":   []interface{}{},
		},
	})
	if err != nil {
		return
	}
	h.deps.Hub.BroadcastAll(frame)
}

// buildInviteMembershipRow validates the §10 invite shape and returns
// the row + the raw signature bytes + the parsed added_at. The
// caller cross-references inviter_sign_pubkey against the live users
// table; this helper covers everything that's safely available from
// the request body alone (byte-length, base64 decode, invitee-pubkey
// equality with the live users table).
func buildInviteMembershipRow(
	channelID, inviteeUserID, caller string,
	inviteeStoredBox, inviteeStoredSign []byte,
	mb *inviteMembershipBlock, now time.Time,
) (repo.ChannelMember, []byte, time.Time, *wrapDecodeError) {
	if mb.InviterUserID != caller {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest, Msg: "membership.inviter_user_id must equal caller user id",
		}
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
	if len(inviteeStoredBox) == pubkeyByteLen && !bytesEqualConstantTime(inviteeStoredBox, boxPub) {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "membership.invitee_box_pubkey does not match invitee's stored box_pubkey",
		}
	}
	if len(inviteeStoredSign) == pubkeyByteLen && !bytesEqualConstantTime(inviteeStoredSign, inviteeSign) {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "membership.invitee_sign_pubkey does not match invitee's stored sign_pubkey",
		}
	}
	added, terr := parseRFC3339Either(mb.AddedAt)
	if terr != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: terr.Error()}
	}
	sigBytes, sigErr := decodeMembershipSignature(mb.InviterSignature, false)
	if sigErr != nil {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{Code: CodeBadRequest, Msg: sigErr.Error()}
	}
	if len(sigBytes) == 0 {
		return repo.ChannelMember{}, nil, time.Time{}, &wrapDecodeError{
			Code: CodeBadRequest,
			Msg:  "inviter_signature is required for invite",
		}
	}
	_ = now
	row := repo.ChannelMember{
		ChannelID:         channelID,
		UserID:            inviteeUserID,
		InviterUserID:     caller,
		InviterSignPubkey: signPub,
		InviterSignature:  sigBytes,
		InviteeBoxPubkey:  boxPub,
		InviteeSignPubkey: inviteeSign,
		AddedAt:           added,
	}
	return row, sigBytes, added, nil
}

const membershipSignatureByteLen = 64

func decodeMembershipPubkey(field, value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New(field + " is required")
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New(field + " must be base64-encoded")
	}
	if len(raw) != pubkeyByteLen {
		return nil, errors.New(field + " must decode to 32 bytes")
	}
	return raw, nil
}

func decodeMembershipSignature(value string, channelIsPublic bool) ([]byte, error) {
	if value == "" {
		if channelIsPublic {
			return nil, nil
		}
		return nil, errors.New("inviter_signature is required for private channels")
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("inviter_signature must be base64-encoded")
	}
	if len(raw) != membershipSignatureByteLen {
		return nil, errors.New("inviter_signature must decode to 64 bytes")
	}
	return raw, nil
}

// encodeMember turns a repo.ChannelMember into the wire shape the
// /members endpoints return. Pubkey + signature blobs are base64-
// encoded; an empty signature stays omitted (NULL on the wire).
func encodeMember(m repo.ChannelMember, username string) memberWire {
	out := memberWire{
		UserID:            m.UserID,
		InviterUserID:     m.InviterUserID,
		InviterSignPubkey: base64.StdEncoding.EncodeToString(m.InviterSignPubkey),
		InviteeBoxPubkey:  base64.StdEncoding.EncodeToString(m.InviteeBoxPubkey),
		InviteeSignPubkey: base64.StdEncoding.EncodeToString(m.InviteeSignPubkey),
		AddedAt:           m.AddedAt,
		Username:          username,
	}
	if len(m.InviterSignature) > 0 {
		out.InviterSignature = base64.StdEncoding.EncodeToString(m.InviterSignature)
	}
	return out
}

// lookupUsernamesForMembers resolves the {user_id -> username} map for
// a slice of members via one IN-list query. The presence file owns
// `lookupUsernames` (returns []PresenceUser); this one is a map
// because the caller indexes by user_id. Pre-Phase-10 rows may have
// empty usernames; empty strings flow through unchanged.
func lookupUsernamesForMembers(ctx context.Context, db *sql.DB, members []repo.ChannelMember) (map[string]string, error) {
	if len(members) == 0 {
		return map[string]string{}, nil
	}
	ids := make([]interface{}, 0, len(members))
	placeholders := ""
	for i, m := range members {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		ids = append(ids, m.UserID)
	}
	q := "SELECT id, username FROM users WHERE id IN (" + placeholders + ")"
	rows, err := db.QueryContext(ctx, q, ids...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string, len(members))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// lookupUserPubkeys returns (exists, box_pubkey, sign_pubkey, err) for
// userID. Pubkeys may be empty for pre-Phase-10 rows; callers branch
// on `exists` first. The pubkeys are returned as raw bytes (no base64).
func lookupUserPubkeys(ctx context.Context, db *sql.DB, userID string) (bool, []byte, []byte, error) {
	row := db.QueryRowContext(ctx,
		`SELECT box_pubkey, sign_pubkey FROM users WHERE id = ?`, userID)
	var box, sign []byte
	if err := row.Scan(&box, &sign); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, nil, nil
		}
		return false, nil, nil, err
	}
	return true, box, sign, nil
}
