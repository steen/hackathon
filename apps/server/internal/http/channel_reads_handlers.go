package http

import (
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
// both channel-reads and dm-reads (the DM emitter is owned by the H
// sub-issue; this PR ships only the channel arm).
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
//  4. Publishes a {type:"read", scope:"channel", scope_id, last_read_message_id}
//     frame to topic `user:<viewer>` for cross-device sync (decision
//     log §7 / L10 — peers do not see read receipts).
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
	uid, _, ok := userFromContext(r)
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
	if _, ok := validULID(req.MessageID); !ok {
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
	if err := h.deps.Repo.UpsertChannelRead(r.Context(), channelID, uid, req.MessageID); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not mark channel read")
		return
	}
	h.publishReadFrame(uid, channelID, req.MessageID)
	w.WriteHeader(stdhttp.StatusNoContent)
}

// publishReadFrame fans out the {type:"read"} envelope to the
// originating viewer's `user:<viewer>` inbox topic. Hub may be nil in
// test fixtures that exercise the handler without a hub; the call is
// a no-op in that case. A marshal failure is logged and dropped — a
// missed cross-device frame self-corrects on the next listing fetch
// per the §12 reconcile rule, so this is the right failure mode.
func (h *ChannelReadsHandlers) publishReadFrame(viewerID, channelID, messageID string) {
	if h.deps.Hub == nil {
		return
	}
	frame, err := json.Marshal(struct {
		Type              string `json:"type"`
		Scope             string `json:"scope"`
		ScopeID           string `json:"scope_id"`
		LastReadMessageID string `json:"last_read_message_id"`
	}{
		Type:              WSEventRead,
		Scope:             readScopeChannel,
		ScopeID:           channelID,
		LastReadMessageID: messageID,
	})
	if err != nil {
		slog.Error("read frame marshal", "err", err, "channel_id", channelID)
		return
	}
	h.deps.Hub.Broadcast("user:"+viewerID, frame)
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
