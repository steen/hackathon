package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// CodeInvalidPeer signals POST /api/dms got peer_user_id == caller, or
// a peer_user_id that does not exist. Decision-log §6 + dms.md
// "POST /api/dms" step 2.
const CodeInvalidPeer = "invalid_peer"

// WSEventDM is the {type} value on the {type:"dm"} self-sufficient
// frame (decision-log §8). The data shape is built inline in
// viewerSpecificFrame so the wire field names match
// specs/plans/phase-9/dms.md exactly.
const WSEventDM = "dm"

// readScopeDM is the data.scope value on the {type:"read"} frame for
// DM read marks. Channels use a separate "channel" scope value. The
// shared WSEventRead constant lives in channel_reads_handlers.go since
// the channel arm landed first; both arms use the same envelope type
// and discriminate via the data.scope field (decision-log §7).
const readScopeDM = "dm"

// DMsDeps wires the DM handlers. Mirrors ChannelsDeps so the wiring
// file can construct both with the same shape.
type DMsDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
	Now  func() time.Time
}

// DMsHandlers groups the http.HandlerFunc values for /api/dms and
// /api/dms/{id}/messages. Construct via NewDMsHandlers and wire each
// route via Routes.
type DMsHandlers struct {
	deps DMsDeps
}

// NewDMsHandlers wires the dependency bag. Defaults Now to time.Now
// when unset so production callers do not have to think about clocks.
func NewDMsHandlers(deps DMsDeps) *DMsHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &DMsHandlers{deps: deps}
}

// dmConversationView is the per-request projection the handlers return.
// Mirrors specs/plans/phase-9/dms.md Conversation: the persisted row
// fields plus the viewer-relative peer summary and unread count.
type dmConversationView struct {
	repo.DMConversation
	Peer        repo.UserSummary `json:"peer"`
	UnreadCount int              `json:"unread_count"`
}

// createDMRequest is the wire body for POST /api/dms (decision-log
// §7 + L6 + L12). Strict-decoded per L1.
//
// 201 path (new conversation): root_key_wraps MUST contain exactly
// 2 entries — one for self, one for the peer; each carries a
// crypto_box wrap of the conversation root key plus the wrapper's
// box_pubkey + nonce.
//
// 200 path (idempotent re-call): root_key_wraps MUST be empty. If the
// client supplies wraps for an existing conversation, the server
// returns 409 `wraps_already_set` per L6 (DMs never rotate; wraps
// are immutable post-create) — L12 server-side enforcement.
type createDMRequest struct {
	PeerUserID   string          `json:"peer_user_id"`
	RootKeyWraps []wrapEntryWire `json:"root_key_wraps,omitempty"`
}

// CreateOrGet handles POST /api/dms — idempotent find-or-create.
// 201 on create, 200 on existing per decision-log L18.
//
// Phase-10 (decision-log §7 + L6 + L7 + L12):
//   - 201 path: body MUST carry `root_key_wraps` with exactly 2
//     entries (recipient_user_id ∈ {self, peer}); server validates
//     L30 + L39 on each, then inserts the conversation row + both
//     wrap rows in ONE transaction.
//   - 200 path: body MUST omit `root_key_wraps`. If wraps are
//     present, server returns 409 `wraps_already_set` per L6/L12.
func (h *DMsHandlers) CreateOrGet(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	viewerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	var req createDMRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	peerID, ok := validULID(strings.TrimSpace(req.PeerUserID))
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "peer_user_id must be a ULID")
		return
	}
	if peerID == viewerID {
		WriteError(w, stdhttp.StatusBadRequest, CodeInvalidPeer, "cannot start a conversation with yourself")
		return
	}
	peer, err := h.deps.Repo.LookupUserSummary(r.Context(), peerID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not look up peer")
		return
	}
	if peer == nil {
		WriteError(w, stdhttp.StatusNotFound, CodeNotFound, "peer user not found")
		return
	}

	userA, userB := repo.CanonicalDMUserOrder(viewerID, peerID)

	// Probe whether the conversation already exists so the L6 rule
	// fires BEFORE we mutate state. The find-or-create path inside
	// the repo can't tell us that "wraps were supplied for an
	// existing conversation" — it only reports created vs. existed.
	existing, err := h.findExistingDMConversation(r.Context(), userA, userB)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not query conversation")
		return
	}
	if existing != nil {
		// 200 idempotent re-call. L6/L12 — wraps must be empty.
		if len(req.RootKeyWraps) > 0 {
			WriteError(w, stdhttp.StatusConflict, CodeWrapsAlreadySet,
				"root_key_wraps must be omitted for an existing conversation (DMs never rotate)")
			return
		}
		unread, err := h.unreadCountForViewer(r.Context(), viewerID, existing.ID)
		if err != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load unread count")
			return
		}
		view := dmConversationView{
			DMConversation: *existing,
			Peer:           *peer,
			UnreadCount:    unread,
		}
		WriteOK(w, stdhttp.StatusOK, view)
		return
	}

	// 201 path. Wraps are optional under the L26 optional-first
	// transition rule: a body that omits root_key_wraps falls through
	// to the legacy create path so phase-9 harnesses keep
	// round-tripping. When supplied, the wrap-list MUST contain
	// exactly two entries (one per participant) and the L30/L39
	// invariants apply per entry.
	id := ids.NewULID()
	now := h.deps.Now()
	if len(req.RootKeyWraps) == 0 {
		conv, _, ferr := h.deps.Repo.FindOrCreateDMConversation(r.Context(), id, userA, userB, now)
		if ferr != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create conversation")
			return
		}
		unread, uerr := h.unreadCountForViewer(r.Context(), viewerID, conv.ID)
		if uerr != nil {
			WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load unread count")
			return
		}
		WriteOK(w, stdhttp.StatusCreated, dmConversationView{
			DMConversation: *conv,
			Peer:           *peer,
			UnreadCount:    unread,
		})
		return
	}
	if len(req.RootKeyWraps) != 2 {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"root_key_wraps must contain exactly 2 entries on POST /api/dms (one per participant)")
		return
	}
	wraps, werr := decodeDMWraps(req.RootKeyWraps, viewerID, peerID)
	if werr != nil {
		WriteError(w, stdhttp.StatusBadRequest, werr.Code, werr.Msg)
		return
	}
	// L30 — caller's box_pubkey must match every wrap's sender claim.
	_, callerBoxPub, _, perr := lookupUserPubkeys(r.Context(), h.deps.Repo.DB(), viewerID)
	if perr != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load caller pubkeys")
		return
	}
	for _, wrap := range wraps {
		if !bytesEqualConstantTime(callerBoxPub, wrap.SenderBoxPubkey) {
			WriteError(w, stdhttp.StatusBadRequest, CodeSenderPubkeyMismatch,
				"root_key_wraps entry sender_box_pubkey does not match caller's box_pubkey (L30)")
			return
		}
	}

	conv, err := h.createDMConversationWithWrapsTx(r.Context(), id, userA, userB, wraps, now)
	if err != nil {
		// FindOrCreate-style race: another tx beat us to the INSERT.
		// Fall through to the 200 idempotent shape so the caller
		// sees a stable contract; the wraps they posted lost the
		// race and the L6 rule kicks in on retry.
		if errors.Is(err, errDMConversationRace) {
			existing, qerr := h.findExistingDMConversation(r.Context(), userA, userB)
			if qerr != nil || existing == nil {
				WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not query conversation")
				return
			}
			WriteError(w, stdhttp.StatusConflict, CodeWrapsAlreadySet,
				"conversation was created concurrently — re-issue without root_key_wraps")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create conversation")
		return
	}

	unread, err := h.unreadCountForViewer(r.Context(), viewerID, conv.ID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not load unread count")
		return
	}
	view := dmConversationView{
		DMConversation: *conv,
		Peer:           *peer,
		UnreadCount:    unread,
	}
	WriteOK(w, stdhttp.StatusCreated, view)
}

// errDMConversationRace surfaces a race-loss against another tx that
// created the same canonical (user_a, user_b) row between our probe
// and our INSERT. CreateOrGet maps this to a 409 wraps_already_set.
var errDMConversationRace = errors.New("dm conversation created concurrently")

// findExistingDMConversation reads the dm_conversations row by the
// canonical (user_a, user_b) pair, returning (nil, nil) when no row
// exists. Used for the L6/L12 probe + race-loss recovery.
func (h *DMsHandlers) findExistingDMConversation(ctx context.Context, userA, userB string) (*repo.DMConversation, error) {
	row := h.deps.Repo.DB().QueryRowContext(ctx,
		`SELECT id, user_a_id, user_b_id, last_message_id, last_message_at, created_at
		   FROM dm_conversations
		  WHERE user_a_id = ? AND user_b_id = ?`,
		userA, userB,
	)
	var c repo.DMConversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.LastMessageID, &c.LastMessageAt, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// createDMConversationWithWrapsTx atomically inserts the conversation
// row and both wrap rows. Returns errDMConversationRace when the
// INSERT loses to another concurrent create on the canonical
// (user_a, user_b) pair (UNIQUE constraint on the table — see
// migrations/0004_dms.sql). The dm_conversations table uses INSERT OR
// IGNORE in repo.FindOrCreateDMConversation; here we use plain INSERT
// so the constraint failure surfaces as a race-loss we can map.
func (h *DMsHandlers) createDMConversationWithWrapsTx(
	ctx context.Context, id, userA, userB string, wraps []decodedWrap, now time.Time,
) (*repo.DMConversation, error) {
	created := now.UTC()
	tx, err := h.deps.Repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO dm_conversations
		   (id, user_a_id, user_b_id, last_message_id, last_message_at, created_at)
		   VALUES (?, ?, ?, NULL, NULL, ?)`,
		id, userA, userB, created,
	)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected != 1 {
		// Another tx won the race — the (user_a, user_b) row already
		// exists. Map to errDMConversationRace so the caller falls
		// through to the L6 idempotent path.
		return nil, errDMConversationRace
	}
	row := tx.QueryRowContext(ctx,
		`SELECT id, user_a_id, user_b_id, last_message_id, last_message_at, created_at
		   FROM dm_conversations
		  WHERE id = ?`,
		id,
	)
	var c repo.DMConversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.LastMessageID, &c.LastMessageAt, &c.CreatedAt); err != nil {
		return nil, err
	}
	for _, wrap := range wraps {
		if err := h.deps.Repo.InsertDMConversationKeyTx(ctx, tx, repo.DMConversationKey{
			ConversationID:  id,
			MemberUserID:    wrap.RecipientUserID,
			WrappedKey:      wrap.WrappedKey,
			SenderBoxPubkey: wrap.SenderBoxPubkey,
			Nonce:           wrap.Nonce,
			CreatedAt:       now,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// decodeDMWraps validates the 2-entry wrap-list shape required on the
// POST /api/dms 201 path. Both recipient_user_ids must be present and
// must form the set {viewerID, peerID} exactly. Returns the decoded
// wraps in the same order they appeared in the request body.
func decodeDMWraps(in []wrapEntryWire, viewerID, peerID string) ([]decodedWrap, *wrapDecodeError) {
	if len(in) != 2 {
		return nil, &wrapDecodeError{Code: CodeBadRequest,
			Msg: "root_key_wraps must contain exactly 2 entries"}
	}
	out := make([]decodedWrap, 0, 2)
	seen := make(map[string]struct{}, 2)
	for i, w := range in {
		rid := w.RecipientUserID
		if rid == "" {
			return nil, &wrapDecodeError{Code: CodeBadRequest,
				Msg: "root_key_wraps[" + strconv.Itoa(i) + "].recipient_user_id is required"}
		}
		if rid != viewerID && rid != peerID {
			return nil, &wrapDecodeError{Code: CodeBadRequest,
				Msg: "root_key_wraps[" + strconv.Itoa(i) + "].recipient_user_id must be one of {self, peer}"}
		}
		if _, dup := seen[rid]; dup {
			return nil, &wrapDecodeError{Code: CodeBadRequest,
				Msg: "root_key_wraps duplicate recipient_user_id"}
		}
		seen[rid] = struct{}{}
		decoded, werr := decodeWrapEntry(w)
		if werr != nil {
			return nil, werr
		}
		out = append(out, decoded)
	}
	if _, hasViewer := seen[viewerID]; !hasViewer {
		return nil, &wrapDecodeError{Code: CodeBadRequest,
			Msg: "root_key_wraps must include the caller as a recipient"}
	}
	if _, hasPeer := seen[peerID]; !hasPeer {
		return nil, &wrapDecodeError{Code: CodeBadRequest,
			Msg: "root_key_wraps must include the peer as a recipient"}
	}
	return out, nil
}

// List handles GET /api/dms — viewer's conversations, hiding empty ones.
func (h *DMsHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	viewerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	convs, err := h.deps.Repo.ListDMConversations(r.Context(), viewerID)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list conversations")
		return
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"conversations": convs})
}

// SendMessage handles POST /api/dms/{id}/messages. Validates body cap
// (L16), enforces the participant ACL (L8 — 404 on non-participation),
// inserts atomically via InsertDMMessageTx (decision-log §11), and
// fans out a self-sufficient {type:"dm"} frame to BOTH participants'
// user:<viewer> topics (decision-log §4 / §8).
func (h *DMsHandlers) SendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	conv, viewerID, status, code, msg := h.resolveConversationFromPath(r)
	if status != 0 {
		WriteError(w, status, code, msg)
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "message body must not be empty")
		return
	}
	if len(body) > MaxMessageBodyBytes {
		WriteMessageTooLarge(w)
		return
	}

	id := ids.NewULID()
	dmMsg, updatedConv, err := h.deps.Repo.InsertDMMessageTx(r.Context(), id, conv.ID, viewerID, body, h.deps.Now())
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not insert message")
		return
	}

	h.broadcastDM(updatedConv, dmMsg)
	WriteOK(w, stdhttp.StatusCreated, dmMsg)
}

// ListMessages handles GET /api/dms/{id}/messages?limit=&before=.
// Mirrors GET /api/channels/{id}/messages — same ULID-cursor newest-
// first paging, different table.
func (h *DMsHandlers) ListMessages(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	conv, _, status, code, msg := h.resolveConversationFromPath(r)
	if status != 0 {
		WriteError(w, status, code, msg)
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
	msgs, err := h.deps.Repo.ListDMMessages(r.Context(), conv.ID, before, limit)
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list messages")
		return
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"messages": msgs})
}

// resolveConversationFromPath validates the {id} path segment, loads the
// conversation row, and applies the L8 participant guard (404 on non-
// participation). Returns (conv, viewerID, 0, "", "") on success.
// On failure the (status, code, msg) triple matches the envelope the
// caller should write.
func (h *DMsHandlers) resolveConversationFromPath(r *stdhttp.Request) (*repo.DMConversation, string, int, string, string) {
	rawID := r.PathValue("id")
	id, ok := ids.NormalizeChannelID(rawID)
	if !ok {
		return nil, "", stdhttp.StatusBadRequest, CodeBadRequest, "invalid conversation id"
	}
	viewerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		return nil, "", stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context"
	}
	conv, err := h.deps.Repo.GetDMConversation(r.Context(), id)
	if err != nil {
		return nil, "", stdhttp.StatusInternalServerError, CodeInternal, "could not load conversation"
	}
	// L8: 404 on both unknown id AND non-participation. Same status code
	// keeps a non-participant from probing whether a conversation exists.
	if conv == nil || (viewerID != conv.UserAID && viewerID != conv.UserBID) {
		return nil, "", stdhttp.StatusNotFound, CodeNotFound, "conversation not found"
	}
	return conv, viewerID, 0, "", ""
}

// broadcastDM emits a {type:"dm"} self-sufficient frame to BOTH
// participants' user:<viewer> topics (decision-log §4 / §8). The
// embedded conversation block is rendered relative to the SENDER on
// the sender's frame and relative to the RECIPIENT on the recipient's
// frame so each side's `peer` and `unread_count` are correct without
// a follow-up GET /api/dms (decision-log §8 self-sufficiency).
//
// The hub may be nil in unit tests that exercise the handler without
// fan-out; the call is a no-op in that case.
//
// The broadcast runs after the HTTP response has been written, so
// frame construction uses context.Background — inheriting the request
// context would race the fan-out against the request being torn down.
func (h *DMsHandlers) broadcastDM(conv *repo.DMConversation, msg *repo.DMMessage) {
	if h.deps.Hub == nil {
		return
	}
	ctx := context.Background()
	for _, viewer := range []string{conv.UserAID, conv.UserBID} {
		frame, err := h.viewerSpecificFrame(ctx, viewer, conv, msg)
		if err != nil {
			continue
		}
		h.deps.Hub.Broadcast("user:"+viewer, frame)
	}
}

// viewerSpecificFrame builds the `{type:"dm", data:{conversation, dm_message}}`
// JSON payload for a specific viewer. Reads the peer username + the
// viewer's unread count from the DB so the recipient's first-DM frame
// can render a sidebar entry without a GET /api/dms round-trip.
func (h *DMsHandlers) viewerSpecificFrame(ctx context.Context, viewerID string, conv *repo.DMConversation, msg *repo.DMMessage) ([]byte, error) {
	peerID := conv.UserBID
	if viewerID == conv.UserBID {
		peerID = conv.UserAID
	}
	peer, err := h.deps.Repo.LookupUserSummary(ctx, peerID)
	if err != nil || peer == nil {
		// On lookup failure emit the frame with an empty username; the
		// recipient client can recover via GET /api/dms. A degraded
		// frame is preferable to a dropped one.
		peer = &repo.UserSummary{ID: peerID}
	}

	unread, err := h.unreadCountForViewer(ctx, viewerID, conv.ID)
	if err != nil {
		unread = 0
	}

	view := dmConversationView{
		DMConversation: *conv,
		Peer:           *peer,
		UnreadCount:    unread,
	}
	frame := map[string]interface{}{
		"type": WSEventDM,
		"data": map[string]interface{}{
			"conversation": view,
			"dm_message":   msg,
		},
	}
	return json.Marshal(frame)
}

// unreadCountForViewer returns COUNT(peer messages) where m.id >
// COALESCE(viewer's last_read, ”). Matches the ListDMConversations
// formula so a recipient's badge stays consistent across the WS
// frame and the next GET /api/dms reconcile.
func (h *DMsHandlers) unreadCountForViewer(ctx context.Context, viewerID, conversationID string) (int, error) {
	row := h.deps.Repo.DB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dm_messages m
		 WHERE m.conversation_id = ?
		   AND m.sender_user_id != ?
		   AND m.id > COALESCE(
		        (SELECT last_read_dm_message_id FROM dm_reads
		          WHERE conversation_id = ? AND user_id = ?), '')`,
		conversationID, viewerID, conversationID, viewerID,
	)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// MarkRead handles POST /api/dms/{id}/read. Decision-log L5 (advance-
// only UPSERT), L8 (404 on non-participation reuses
// resolveConversationFromPath), §7 (emit {type:"read"} to caller's
// user:<viewer> topic for cross-device sync), L17 (read-mark bucket is
// applied by the wiring layer). 204 on success per the issue's AC.
func (h *DMsHandlers) MarkRead(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	conv, viewerID, status, code, msg := h.resolveConversationFromPath(r)
	if status != 0 {
		WriteError(w, status, code, msg)
		return
	}

	var req struct {
		MessageID string `json:"message_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	messageID, ok := validULID(strings.TrimSpace(req.MessageID))
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "message_id must be a ULID")
		return
	}

	if err := h.deps.Repo.UpsertDMRead(r.Context(), conv.ID, viewerID, messageID); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not mark read")
		return
	}

	h.broadcastDMRead(r.Context(), viewerID, conv.ID)
	w.WriteHeader(stdhttp.StatusNoContent)
}

// broadcastDMRead emits a {type:"read", scope:"dm"} frame to the
// caller's user:<viewer> topic only (decision-log §7 / L10 — no peer
// fan-out). The frame's last_read_message_id reads back the persisted
// pointer so a same-tx-no-op (advance-only L5: posting an older id)
// still emits the CURRENT pointer, keeping cross-device clients
// consistent without leaking the older id.
//
// The hub may be nil in tests that exercise the handler without
// fan-out; the call is then a no-op.
func (h *DMsHandlers) broadcastDMRead(ctx context.Context, viewerID, conversationID string) {
	if h.deps.Hub == nil {
		return
	}
	cursor, err := h.persistedDMReadCursor(ctx, viewerID, conversationID)
	if err != nil {
		return
	}
	unread, err := h.unreadCountForViewer(ctx, viewerID, conversationID)
	if err != nil {
		unread = 0
	}
	frame, err := json.Marshal(map[string]interface{}{
		"type": WSEventRead,
		"data": map[string]interface{}{
			"scope":                readScopeDM,
			"target_id":            conversationID,
			"last_read_message_id": cursor,
			"unread_count":         unread,
		},
	})
	if err != nil {
		return
	}
	h.deps.Hub.Broadcast("user:"+viewerID, frame)
}

// persistedDMReadCursor returns the viewer's current
// last_read_dm_message_id for the conversation. Empty string when no
// row exists (legitimate post-NULL state) — callers that need to
// distinguish absent from empty should check the dm_reads schema
// directly. Used by broadcastDMRead so the WS frame carries the
// post-UPSERT pointer even when the UPSERT was a silent no-op.
func (h *DMsHandlers) persistedDMReadCursor(ctx context.Context, viewerID, conversationID string) (string, error) {
	row := h.deps.Repo.DB().QueryRowContext(ctx,
		`SELECT COALESCE(last_read_dm_message_id, '') FROM dm_reads
		   WHERE conversation_id = ? AND user_id = ?`,
		conversationID, viewerID,
	)
	var s string
	if err := row.Scan(&s); err != nil {
		return "", err
	}
	return s, nil
}

// Routes registers /api/dms and /api/dms/{id}/... on mux. require is
// the JWT middleware constructed by registerAuth — every DM route is
// gated through it. writeLimit, when non-nil, applies the per-user
// dm-write limiter to POST /api/dms/{id}/messages (decision-log L17).
// readLimit, when non-nil, applies the per-user read-mark limiter to
// POST /api/dms/{id}/read (decision-log L17). Callers that don't
// exercise rate-limiting pass nil.
func (h *DMsHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	writeLimit func(stdhttp.Handler) stdhttp.Handler,
	readLimit func(stdhttp.Handler) stdhttp.Handler,
) {
	wrapWrite := func(handler stdhttp.Handler) stdhttp.Handler {
		if writeLimit != nil {
			handler = writeLimit(handler)
		}
		return require(handler)
	}
	wrapRead := func(handler stdhttp.Handler) stdhttp.Handler {
		if readLimit != nil {
			handler = readLimit(handler)
		}
		return require(handler)
	}
	mux.Handle("POST /api/dms", require(stdhttp.HandlerFunc(h.CreateOrGet)))
	mux.Handle("GET /api/dms", require(stdhttp.HandlerFunc(h.List)))
	mux.Handle("POST /api/dms/{id}/messages", wrapWrite(stdhttp.HandlerFunc(h.SendMessage)))
	mux.Handle("GET /api/dms/{id}/messages", require(stdhttp.HandlerFunc(h.ListMessages)))
	mux.Handle("POST /api/dms/{id}/read", wrapRead(stdhttp.HandlerFunc(h.MarkRead)))
}
