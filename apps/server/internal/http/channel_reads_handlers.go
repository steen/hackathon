package http

import (
	"context"
	"encoding/json"
	"log/slog"
	stdhttp "net/http"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// WSEventRead is the outbound WS frame type for read-pointer advances.
// Routed only to the originating viewer's `user:<viewer>` topic so peers
// don't see read receipts (decision log L10 / specs/plans/phase-9/read-state.md).
const WSEventRead = "read"

// readScopeChannel marks a {type:"read"} frame as advancing a channel
// read pointer. The scope discriminator lets one envelope shape carry
// both channel-reads and dm-reads — the DM emitter in dms_handlers.go
// uses readScopeDM for the same envelope.
const readScopeChannel = "channel"

// ChannelReadsDeps groups the dependencies the channel-read handler
// needs. Held separately from ChannelsDeps so the read-mark feature
// can be wired without forcing a single fat constructor.
type ChannelReadsDeps struct {
	Repo *repo.Repo
	Hub  *hub.Hub
}

// ChannelReadsHandlers exposes the POST /api/channels/{id}/read
// handler.
type ChannelReadsHandlers struct {
	deps ChannelReadsDeps
}

// NewChannelReadsHandlers wires the dependency bag.
func NewChannelReadsHandlers(deps ChannelReadsDeps) *ChannelReadsHandlers {
	return &ChannelReadsHandlers{deps: deps}
}

// Mark handles POST /api/channels/{id}/read. Must be wrapped in
// auth.RequireJWT and the per-user read-mark token bucket
// (ratelimit.ReadMarkUserConfig — decision log L17 / sub-issue G0).
//
// Request body: {"message_id": "01HK...MSG"}.
//
// On success the handler:
//  1. Validates the channel id and message_id are 26-char ULID-ish.
//  2. Verifies the channel exists (404 otherwise).
//  3. Calls UpsertChannelRead, which is advance-only (older or equal
//     message_id is a silent no-op per decision log L5).
//  4. Publishes a {type:"read", data:{scope:"channel", target_id,
//     last_read_message_id, unread_count}} frame to topic
//     `user:<viewer>` for cross-device sync (decision log §7 / L10 —
//     peers do not see read receipts; envelope shape per
//     specs/plans/phase-9/read-state.md).
//  5. Returns 204 No Content.
//
// The 204 (rather than 200 with an envelope) matches the issue AC and
// the decision-log L13 contract: the client already knows the
// message_id it sent and the WS frame carries the post-advance state
// for cross-device sync, so there is nothing additional to return.
func (h *ChannelReadsHandlers) Mark(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
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
	var req struct {
		MessageID string `json:"message_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	messageID, ok := validULID(req.MessageID)
	if !ok {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "message_id must be a ULID")
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
	if err := h.deps.Repo.UpsertChannelRead(r.Context(), channelID, uid, messageID); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not mark channel read")
		return
	}
	h.publishReadFrame(r.Context(), uid, channelID)
	w.WriteHeader(stdhttp.StatusNoContent)
}

// publishReadFrame fans out the {type:"read"} envelope to the
// originating viewer's `user:<viewer>` inbox topic. Hub may be nil in
// test fixtures that exercise the handler without a hub; the call is
// a no-op in that case.
//
// The envelope shape matches specs/plans/phase-9/read-state.md
// `{type:"read"}` and the DM arm in dms_handlers.go:broadcastDMRead:
//
//	{type:"read", data:{scope, target_id, last_read_message_id, unread_count}}
//
// last_read_message_id reads back the persisted cursor (not the
// caller-supplied id) so a same-tx no-op (advance-only L5: posting an
// older id) still emits the CURRENT pointer, keeping cross-device
// clients consistent without leaking the older id.
//
// A read or marshal failure is logged and dropped — a missed
// cross-device frame self-corrects on the next listing fetch per the
// §12 reconcile rule.
func (h *ChannelReadsHandlers) publishReadFrame(ctx context.Context, viewerID, channelID string) {
	if h.deps.Hub == nil {
		return
	}
	cursor, err := h.persistedChannelReadCursor(ctx, viewerID, channelID)
	if err != nil {
		slog.Error("read frame cursor", "err", err, "channel_id", channelID)
		return
	}
	unread, err := h.unreadCountForViewer(ctx, viewerID, channelID)
	if err != nil {
		// Drop the frame rather than emitting a misleading 0 — the
		// next listing fetch will OVERWRITE the client's badge per
		// the §12 reconcile rule, so a missed frame is recoverable.
		slog.Error("read frame unread", "err", err, "channel_id", channelID)
		return
	}
	frame, err := json.Marshal(map[string]interface{}{
		"type": WSEventRead,
		"data": map[string]interface{}{
			"scope":                readScopeChannel,
			"target_id":            channelID,
			"last_read_message_id": cursor,
			"unread_count":         unread,
		},
	})
	if err != nil {
		slog.Error("read frame marshal", "err", err, "channel_id", channelID)
		return
	}
	h.deps.Hub.Broadcast("user:"+viewerID, frame)
}

// persistedChannelReadCursor returns the viewer's current
// last_read_message_id for the channel. The channel_reads schema
// (migration 0005) marks the column NOT NULL — the row is created by
// the UpsertChannelRead call in Mark, so by the time this runs there
// is always a row. Returns the empty string only if the row vanishes
// between the UPSERT and the SELECT (no production path does this).
func (h *ChannelReadsHandlers) persistedChannelReadCursor(ctx context.Context, viewerID, channelID string) (string, error) {
	row := h.deps.Repo.DB().QueryRowContext(ctx,
		`SELECT last_read_message_id FROM channel_reads
		   WHERE channel_id = ? AND user_id = ?`,
		channelID, viewerID,
	)
	var s string
	if err := row.Scan(&s); err != nil {
		return "", err
	}
	return s, nil
}

// unreadCountForViewer returns COUNT(messages) where m.id > the
// viewer's last_read_message_id. Mirrors the per-channel formula in
// repo.ListChannelsWithReadState (decision-log L6 channel formula) so
// a recipient's badge stays consistent across the WS frame and the
// next GET /api/channels reconcile. The COALESCE to empty string is
// belt-and-braces — channel_reads.last_read_message_id is NOT NULL
// after migration 0005, so the join row is present here.
func (h *ChannelReadsHandlers) unreadCountForViewer(ctx context.Context, viewerID, channelID string) (int, error) {
	row := h.deps.Repo.DB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages m
		 WHERE m.channel_id = ?
		   AND m.id > COALESCE(
		        (SELECT last_read_message_id FROM channel_reads
		          WHERE channel_id = ? AND user_id = ?), '')`,
		channelID, channelID, viewerID,
	)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Routes registers POST /api/channels/{id}/read on mux. require is the
// JWT middleware constructed by registerAuth; readMark is the per-user
// read-mark token bucket. Order: JWT → read-mark → handler so the
// limiter reads the user id from the context the JWT middleware set.
func (h *ChannelReadsHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	readMark func(stdhttp.Handler) stdhttp.Handler,
) {
	chain := stdhttp.Handler(stdhttp.HandlerFunc(h.Mark))
	if readMark != nil {
		chain = readMark(chain)
	}
	mux.Handle("POST /api/channels/{id}/read", require(chain))
}
