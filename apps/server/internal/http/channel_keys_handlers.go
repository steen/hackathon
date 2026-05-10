package http

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	stdhttp "net/http"
	"time"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// ChannelKeysDeps wires the standalone keys-RPC handlers
// (`POST /api/channels/{id}/keys` + `GET /api/channels/{id}/members/wraps-needed`).
// The bag matches the shape of MembersDeps + ChannelsDeps so the
// per-feature wiring file can pass the same trio.
type ChannelKeysDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
	Now  func() time.Time
}

// ChannelKeysHandlers groups the keys-RPC HandlerFunc values.
type ChannelKeysHandlers struct {
	deps ChannelKeysDeps
}

// NewChannelKeysHandlers wires the dependency bag. Defaults Now to
// time.Now so production callers don't have to think about clocks.
func NewChannelKeysHandlers(deps ChannelKeysDeps) *ChannelKeysHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &ChannelKeysHandlers{deps: deps}
}

// CodeInvalidGeneration — keys-RPC: generation_id matches no mode
// (bootstrap / fill-in / rotation). specs/plans/phase-10/keys.md.
const CodeInvalidGeneration = "invalid_generation"

// keysRequest is the wire body of POST /api/channels/{id}/keys. The
// shape is identical across the three modes per
// specs/plans/phase-10/keys.md — the server picks the mode based on
// generation_id's relationship to MaxChannelKeyGeneration. Strict-
// decoded per L1 via decodeJSON.
type keysRequest struct {
	GenerationID int64           `json:"generation_id"`
	Wraps        []wrapEntryWire `json:"wraps"`
}

// wrapsNeededRow mirrors one entry in the GET /wraps-needed response
// per L22. The convenience pubkey + username fields reflect the
// invitee's CURRENT users-row values, while `membership` carries the
// pinned-at-invite-time pubkeys + signature. The verifier-side flow
// in keys.md uses both: the `membership` block for §10 signature
// verification, the convenience fields for the wrap computation.
type wrapsNeededRow struct {
	UserID       string                `json:"user_id"`
	GenerationID int64                 `json:"generation_id"`
	Membership   wrapsNeededMembership `json:"membership"`
	Username     string                `json:"username,omitempty"`
	BoxPubkey    string                `json:"box_pubkey,omitempty"`
	SignPubkey   string                `json:"sign_pubkey,omitempty"`
	AddedAt      time.Time             `json:"added_at"`
}

type wrapsNeededMembership struct {
	InviterUserID     string  `json:"inviter_user_id"`
	InviterSignPubkey string  `json:"inviter_sign_pubkey"`
	InviteeBoxPubkey  string  `json:"invitee_box_pubkey"`
	InviteeSignPubkey string  `json:"invitee_sign_pubkey"`
	AddedAt           string  `json:"added_at"`
	InviterSignature  *string `json:"inviter_signature"`
}

type wrapsNeededResponse struct {
	ChannelID string           `json:"channel_id"`
	IsPublic  bool             `json:"is_public"`
	Missing   []wrapsNeededRow `json:"missing"`
}

// PostKeys handles POST /api/channels/{id}/keys. Three modes share
// one body shape; the server picks the mode based on generation_id
// vs. max(channel_keys.generation_id) per repo.DetectChannelKeyMode.
//
// Validation (specs/plans/phase-10/keys.md, decision-log §8 + L7 +
// L30 + L39):
//
//  1. JWT-required (handler is mounted behind `require`).
//  2. Caller MUST be a current channel_members row (else 403).
//  3. Channel exists (else 404).
//  4. Mode == bootstrap | fill-in | rotation (else 400 invalid_generation).
//  5. Each wrap passes L30 (sender_box_pubkey == caller's box_pubkey)
//     and L39 (wrapped_key == 48, nonce == 24, sender_box_pubkey == 32).
//  6. Mode-specific shape (bootstrap: 1 wrap to self; fill-in: 1 wrap
//     to a member without a current-gen row; rotation: cover every
//     current member exactly once).
//
// On success: insert(s) + WS frame fan-out. Bootstrap + fill-in fan
// the L9 key_received frame to each recipient's `user:<viewer>`
// inbox topic; rotation also fans members_changed to every channel
// subscriber so remaining clients refresh.
func (h *ChannelKeysHandlers) PostKeys(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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
	var req keysRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	if req.GenerationID <= 0 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"generation_id must be a positive integer")
		return
	}
	if len(req.Wraps) == 0 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"wraps must contain at least 1 entry")
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
	maxGen, hasMax, err := h.deps.Repo.MaxChannelKeyGeneration(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not resolve generation")
		return
	}
	mode := repo.DetectChannelKeyMode(req.GenerationID, maxGen, hasMax)
	if mode == repo.ChannelKeyModeInvalid {
		WriteError(w, stdhttp.StatusBadRequest, CodeInvalidGeneration,
			"generation_id matches no keys-RPC mode (bootstrap | fill-in | rotation)")
		return
	}
	_, callerBox, _, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), caller)
	if perr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load caller pubkeys")
		return
	}
	decoded, derr := decodeKeysWraps(req.Wraps, callerBox)
	if derr != nil {
		WriteError(w, stdhttp.StatusBadRequest, derr.Code, derr.Msg)
		return
	}

	switch mode {
	case repo.ChannelKeyModeBootstrap:
		h.runBootstrap(w, r, channelID, caller, decoded)
	case repo.ChannelKeyModeFillIn:
		h.runFillIn(w, r, channelID, caller, req.GenerationID, decoded)
	case repo.ChannelKeyModeRotation:
		h.runRotation(w, r, channelID, caller, req.GenerationID, decoded)
	case repo.ChannelKeyModeInvalid:
		// Already handled above; switch exhaustiveness lint guard.
		WriteError(w, stdhttp.StatusBadRequest, CodeInvalidGeneration,
			"generation_id matches no keys-RPC mode (bootstrap | fill-in | rotation)")
	}
}

// runBootstrap inserts the gen=1 wrap for the caller-as-sole-recipient
// when the channel has no wraps yet. specs/plans/phase-10/keys.md
// "Bootstrap mode" — caller is a member, len(wraps) == 1, recipient
// == caller, L30 + L39 already passed.
func (h *ChannelKeysHandlers) runBootstrap(
	w stdhttp.ResponseWriter, r *stdhttp.Request,
	channelID, caller string, decoded []decodedWrap,
) {
	if len(decoded) != 1 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"bootstrap mode requires exactly 1 wrap (creator wrap-to-self)")
		return
	}
	rid := decoded[0].RecipientUserID
	if rid != "" && rid != caller {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"bootstrap mode requires recipient_user_id == caller (wrap-to-self)")
		return
	}
	now := h.deps.Now()
	if err := h.deps.Repo.InsertChannelKey(r.Context(), repo.ChannelKey{
		ChannelID:       channelID,
		GenerationID:    1,
		MemberUserID:    caller,
		WrappedKey:      decoded[0].WrappedKey,
		SenderBoxPubkey: decoded[0].SenderBoxPubkey,
		Nonce:           decoded[0].Nonce,
		CreatedAt:       now,
	}); err != nil {
		if errors.Is(err, repo.ErrChannelKeyAlreadyExists) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict,
				"another caller already bootstrapped generation 1 for this channel")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert wrap")
		return
	}
	h.broadcastKeyReceived(caller, channelID, 1)
	WriteOK(w, stdhttp.StatusCreated, map[string]any{
		"mode":          "bootstrap",
		"generation_id": 1,
		"inserted":      1,
	})
}

// runFillIn inserts the wrap for one current channel_members row
// that lacks a channel_keys row at the channel's current generation.
// specs/plans/phase-10/keys.md "Fill-in mode": caller is a member,
// len(wraps) == 1, recipient is a different current member without
// a wrap row at generationID. The handler relies on the channel_keys
// PRIMARY KEY for atomic race-loss detection (returns 409 conflict).
func (h *ChannelKeysHandlers) runFillIn(
	w stdhttp.ResponseWriter, r *stdhttp.Request,
	channelID, _ string, generationID int64, decoded []decodedWrap,
) {
	if len(decoded) != 1 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"fill-in mode requires exactly 1 wrap")
		return
	}
	rid := decoded[0].RecipientUserID
	if rid == "" {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"fill-in mode requires wraps[0].recipient_user_id")
		return
	}
	isMember, err := h.deps.Repo.IsMember(r.Context(), channelID, rid)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not check recipient membership")
		return
	}
	if !isMember {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"fill-in recipient is not a current channel member")
		return
	}
	now := h.deps.Now()
	if err := h.deps.Repo.InsertChannelKey(r.Context(), repo.ChannelKey{
		ChannelID:       channelID,
		GenerationID:    generationID,
		MemberUserID:    rid,
		WrappedKey:      decoded[0].WrappedKey,
		SenderBoxPubkey: decoded[0].SenderBoxPubkey,
		Nonce:           decoded[0].Nonce,
		CreatedAt:       now,
	}); err != nil {
		if errors.Is(err, repo.ErrChannelKeyAlreadyExists) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict,
				"wrap already exists for recipient at this generation")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert wrap")
		return
	}
	h.broadcastKeyReceived(rid, channelID, generationID)
	WriteOK(w, stdhttp.StatusCreated, map[string]any{
		"mode":          "fill-in",
		"generation_id": generationID,
		"inserted":      1,
		"recipient":     rid,
	})
}

// runRotation inserts a fresh wrap row for every current member at
// the new generation. specs/plans/phase-10/keys.md "Rotation mode":
// the wrap-list MUST cover every current channel_members row exactly
// once (no duplicates, no extras, no missing). Inserts run inside one
// transaction; race-losers (someone else already rotated to
// MaxGen + 1) get 409.
func (h *ChannelKeysHandlers) runRotation(
	w stdhttp.ResponseWriter, r *stdhttp.Request,
	channelID, _ string, generationID int64, decoded []decodedWrap,
) {
	members, err := h.deps.Repo.ListChannelMembers(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list members")
		return
	}
	wantSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		wantSet[m.UserID] = struct{}{}
	}
	if len(decoded) != len(wantSet) {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"rotation wraps must cover every current member exactly once")
		return
	}
	seen := make(map[string]struct{}, len(decoded))
	for _, dw := range decoded {
		if dw.RecipientUserID == "" {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"rotation mode requires recipient_user_id on every wrap")
			return
		}
		if _, dup := seen[dw.RecipientUserID]; dup {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"rotation wraps must not repeat the same recipient_user_id")
			return
		}
		seen[dw.RecipientUserID] = struct{}{}
		if _, member := wantSet[dw.RecipientUserID]; !member {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"rotation wrap targets a non-member recipient")
			return
		}
	}
	now := h.deps.Now()
	tx, err := h.deps.Repo.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not begin transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()
	for _, dw := range decoded {
		if err := h.deps.Repo.InsertChannelKeyTx(r.Context(), tx, repo.ChannelKey{
			ChannelID:       channelID,
			GenerationID:    generationID,
			MemberUserID:    dw.RecipientUserID,
			WrappedKey:      dw.WrappedKey,
			SenderBoxPubkey: dw.SenderBoxPubkey,
			Nonce:           dw.Nonce,
			CreatedAt:       now,
		}); err != nil {
			if errors.Is(err, repo.ErrChannelKeyAlreadyExists) {
				WriteError(w, stdhttp.StatusConflict, CodeConflict,
					"another caller already rotated to this generation")
				return
			}
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert wrap")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not commit transaction")
		return
	}
	users, lookupErr := lookupUsernamesByIDs(r.Context(), h.deps.Repo.DB(), keysFromMemberSet(wantSet))
	if lookupErr != nil {
		users = map[string]string{}
	}
	atRotation := make([]MembersChangedUser, 0, len(members))
	for _, m := range members {
		atRotation = append(atRotation, MembersChangedUser{ID: m.UserID, Username: users[m.UserID]})
	}
	h.broadcastMembersChanged(channelID, generationID, atRotation)
	for _, dw := range decoded {
		h.broadcastKeyReceived(dw.RecipientUserID, channelID, generationID)
	}
	WriteOK(w, stdhttp.StatusCreated, map[string]any{
		"mode":          "rotation",
		"generation_id": generationID,
		"inserted":      len(decoded),
	})
}

// GetWrapsNeeded handles GET /api/channels/{id}/members/wraps-needed.
// Returns the L22 shape: per-row MembershipBlock with pinned pubkeys
// + signature, plus convenience username/box_pubkey/sign_pubkey from
// the live users table. Caller MUST be a current member (403 else).
//
// `is_public` is server-resolved from channels.is_public per L38 so
// the verifier doesn't depend on a stale channel-listing cache.
func (h *ChannelKeysHandlers) GetWrapsNeeded(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
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
	missing, _, hasGen, err := h.deps.Repo.ListMissingWrapsForCurrentGeneration(r.Context(), channelID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list missing wraps")
		return
	}
	flag := false
	if channel.IsPublic != nil {
		flag = *channel.IsPublic
	}
	resp := wrapsNeededResponse{
		ChannelID: channelID,
		IsPublic:  flag,
		Missing:   []wrapsNeededRow{},
	}
	if !hasGen {
		// No generation yet — caller (or any member) should bootstrap.
		// The lazy-wrap loop reads `missing == [] && generation == 0`
		// as "I need to bootstrap myself" only when caller is the
		// channel's first member; otherwise it waits.
		WriteOK(w, stdhttp.StatusOK, resp)
		return
	}
	if len(missing) == 0 {
		WriteOK(w, stdhttp.StatusOK, resp)
		return
	}
	rows := make([]wrapsNeededRow, 0, len(missing))
	livePubkeys, lookupErr := lookupUsersForWraps(r.Context(), h.deps.Repo.DB(), missingUserIDs(missing))
	if lookupErr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load invitee pubkeys")
		return
	}
	for _, miss := range missing {
		mem, mErr := h.deps.Repo.GetChannelMember(r.Context(), channelID, miss.UserID)
		if mErr != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load member row")
			return
		}
		if mem == nil {
			continue
		}
		row := buildWrapsNeededRow(miss, mem, livePubkeys[miss.UserID])
		rows = append(rows, row)
	}
	resp.Missing = rows
	WriteOK(w, stdhttp.StatusOK, resp)
}

// Routes wires the keys-RPC paths onto mux behind the JWT
// middleware. wrapsNeededLimit is applied to the GET endpoint per
// L31 + L36 (per-user wraps-needed-read bucket); keysWriteLimit is
// applied to the POST endpoint so a misbehaving client cannot loop
// the wrap-insert path. Either limiter may be nil (test path).
func (h *ChannelKeysHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	wrapsNeededLimit func(stdhttp.Handler) stdhttp.Handler,
	keysWriteLimit func(stdhttp.Handler) stdhttp.Handler,
) {
	wrapPost := func(handler stdhttp.Handler) stdhttp.Handler {
		if keysWriteLimit != nil {
			handler = keysWriteLimit(handler)
		}
		return require(handler)
	}
	wrapGet := func(handler stdhttp.Handler) stdhttp.Handler {
		if wrapsNeededLimit != nil {
			handler = wrapsNeededLimit(handler)
		}
		return require(handler)
	}
	mux.Handle("POST /api/channels/{id}/keys", wrapPost(stdhttp.HandlerFunc(h.PostKeys)))
	mux.Handle("GET /api/channels/{id}/members/wraps-needed",
		wrapGet(stdhttp.HandlerFunc(h.GetWrapsNeeded)))
}

// broadcastKeyReceived fans out the L9 key_received frame to the
// recipient's `user:<viewer>` topic. Hub may be nil in tests.
func (h *ChannelKeysHandlers) broadcastKeyReceived(recipientUserID, channelID string, generation int64) {
	if h.deps.Hub == nil || recipientUserID == "" {
		return
	}
	frame := keyReceivedFrame(channelID, generation)
	if frame == nil {
		return
	}
	h.deps.Hub.Broadcast("user:"+recipientUserID, frame)
}

// broadcastMembersChanged fans out the L9 members_changed frame to
// every connected client (BroadcastAll matches the existing
// channel-create/rename pattern; channel listings are a global
// concern). Hub may be nil in tests.
func (h *ChannelKeysHandlers) broadcastMembersChanged(channelID string, generation int64, members []MembersChangedUser) {
	if h.deps.Hub == nil {
		return
	}
	frame := membersChangedFrame(channelID, generation, members)
	if frame == nil {
		return
	}
	h.deps.Hub.BroadcastAll(frame)
}

// decodeKeysWraps decodes every WrapEntry in a keys-RPC body, applies
// the L30 sender_box_pubkey check (matches caller's stored box pubkey),
// and surfaces the first failure as a wrapDecodeError. Empty
// callerBox means "caller's identity is missing pubkeys" (pre-Phase-10
// account); the L30 check still fires (mismatch on len(callerBox) ==
// 0 vs. 32-byte sender pubkey).
func decodeKeysWraps(in []wrapEntryWire, callerBox []byte) ([]decodedWrap, *wrapDecodeError) {
	out := make([]decodedWrap, 0, len(in))
	for _, w := range in {
		dw, err := decodeWrapEntry(w)
		if err != nil {
			return nil, err
		}
		if !bytesEqualConstantTime(callerBox, dw.SenderBoxPubkey) {
			return nil, &wrapDecodeError{
				Code: CodeSenderPubkeyMismatch,
				Msg:  "wraps[*].sender_box_pubkey does not match caller's box_pubkey (L30)",
			}
		}
		out = append(out, dw)
	}
	return out, nil
}

// missingUserIDs extracts the user ids from a slice of MissingWrapMember.
// Tiny helper kept here because the only caller is the wraps-needed
// handler and the loop wants to live where the rest of the row-build
// logic does.
func missingUserIDs(in []repo.MissingWrapMember) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		out = append(out, m.UserID)
	}
	return out
}

// keysFromMemberSet returns the keys of a string set as a slice. Used
// by runRotation to seed lookupUsernamesByIDs.
func keysFromMemberSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// liveInviteePubkeys carries the CURRENT (live-from-users-row)
// pubkeys + username for one missing-wrap recipient. The wraps-needed
// response copies these into the convenience fields of each row so
// the verifier can compute the wrap without a second /api/users call.
type liveInviteePubkeys struct {
	Username   string
	BoxPubkey  []byte
	SignPubkey []byte
}

// lookupUsersForWraps returns a {user_id -> liveInviteePubkeys} map
// for one IN-list query. Mirrors lookupUsernamesForMembers in
// members_handlers.go but additionally returns the box + sign pubkey
// so the row builder can populate the convenience fields.
func lookupUsersForWraps(ctx context.Context, db *sql.DB, ids []string) (map[string]liveInviteePubkeys, error) {
	out := map[string]liveInviteePubkeys{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	placeholders := ""
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	q := "SELECT id, username, box_pubkey, sign_pubkey FROM users WHERE id IN (" + placeholders + ")"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, name string
		var box, sign []byte
		if err := rows.Scan(&id, &name, &box, &sign); err != nil {
			return nil, err
		}
		out[id] = liveInviteePubkeys{Username: name, BoxPubkey: box, SignPubkey: sign}
	}
	return out, rows.Err()
}

// lookupUsernamesByIDs is the username-only variant of
// lookupUsersForWraps. Used by runRotation to populate the
// members_at_rotation array's username field without paying for the
// pubkey columns.
func lookupUsernamesByIDs(ctx context.Context, db *sql.DB, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	placeholders := ""
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	q := "SELECT id, username FROM users WHERE id IN (" + placeholders + ")"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// buildWrapsNeededRow projects a (MissingWrapMember + ChannelMember +
// liveInviteePubkeys) triple into the wire shape per L22.
//
// inviter_signature is null on the wire when the channel_members row
// has an empty signature (public-channel R1.2 carve-out). Pinned
// pubkey fields (everything under `membership`) come from the
// channel_members row; convenience fields (`username`, `box_pubkey`,
// `sign_pubkey`) come from the live users row.
func buildWrapsNeededRow(miss repo.MissingWrapMember, mem *repo.ChannelMember, live liveInviteePubkeys) wrapsNeededRow {
	enc := base64.StdEncoding
	row := wrapsNeededRow{
		UserID:       miss.UserID,
		GenerationID: miss.GenerationID,
		Username:     live.Username,
		AddedAt:      mem.AddedAt,
		Membership: wrapsNeededMembership{
			InviterUserID:     mem.InviterUserID,
			InviterSignPubkey: enc.EncodeToString(mem.InviterSignPubkey),
			InviteeBoxPubkey:  enc.EncodeToString(mem.InviteeBoxPubkey),
			InviteeSignPubkey: enc.EncodeToString(mem.InviteeSignPubkey),
			AddedAt:           mem.AddedAt.UTC().Format(time.RFC3339),
		},
	}
	if len(mem.InviterSignature) > 0 {
		s := enc.EncodeToString(mem.InviterSignature)
		row.Membership.InviterSignature = &s
	}
	if len(live.BoxPubkey) == pubkeyByteLen {
		row.BoxPubkey = enc.EncodeToString(live.BoxPubkey)
	}
	if len(live.SignPubkey) == pubkeyByteLen {
		row.SignPubkey = enc.EncodeToString(live.SignPubkey)
	}
	return row
}
