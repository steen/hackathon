package http

import (
	"context"
	"encoding/json"
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

// CreateOrGet handles POST /api/dms — idempotent find-or-create.
// 201 on create, 200 on existing per decision-log L18.
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
	var req struct {
		PeerUserID string `json:"peer_user_id"`
	}
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
	id := ids.NewULID()
	conv, created, err := h.deps.Repo.FindOrCreateDMConversation(r.Context(), id, userA, userB, h.deps.Now())
	if err != nil {
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

	status := stdhttp.StatusOK
	if created {
		status = stdhttp.StatusCreated
	}
	WriteOK(w, status, view)
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
		if _, ok := validULID(before); !ok {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "before must be a ULID")
			return
		}
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
