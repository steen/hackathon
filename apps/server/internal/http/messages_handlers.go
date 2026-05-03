package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// MaxMessageBodyBytes is the hard cap from PRD §9 (4 KiB).
const MaxMessageBodyBytes = 4 * 1024

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

// NewMessagesHandlers wires the dependencies the messages handlers need.
// Defaults Now to time.Now when unset so production callers do not have to
// think about clocks.
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
		if _, ok := validULID(before); !ok {
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "before must be a ULID")
			return
		}
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
	uid, _, ok := userFromContext(r)
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
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "message body exceeds 4 KiB")
		return
	}
	id := ids.NewULID()
	msg, err := h.deps.Repo.InsertMessage(r.Context(), id, channelID, uid, body, h.deps.Now())
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

// validULID is a cheap shape check (Crockford base32, 26 chars). We
// accept lowercase here because some clients normalize URLs to lower.
func validULID(s string) (string, bool) {
	if len(s) != 26 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !ok {
			return "", false
		}
	}
	return s, true
}
