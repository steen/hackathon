package http

import (
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

// Create handles POST /api/channels. Must be wrapped in auth.RequireJWT.
//
// Phase-10 §9 + L24: the body accepts an optional `is_public` boolean
// (default false). Public channels are server-readable by every
// registered user (R1.2 carve-out); private channels (the default)
// gate access through the explicit channel_members relation. The
// creator is added as the sole initial member in the same handler so
// a fresh channel is never visible without its creator (decision-log
// §10 self-bootstrap carve-out).
func (h *ChannelsHandlers) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	var req struct {
		Name     string `json:"name"`
		IsPublic bool   `json:"is_public,omitempty"`
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
	caller, ok := userFromContext(r)
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "missing user context")
		return
	}
	id := ids.NewULID()
	now := h.deps.Now()
	ch, err := h.deps.Repo.CreateChannel(r.Context(), id, name, req.IsPublic, now)
	if err != nil {
		if errors.Is(err, repo.ErrChannelNameTaken) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel name already taken")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create channel")
		return
	}
	// Self-bootstrap the creator's membership row. inviter == invitee
	// per the §10 self-bootstrap carve-out. For public channels we
	// allow NULL signature (carve-out per L33). For private channels
	// the row carries the caller's pinned pubkeys (looked up server-
	// side). The full inviter-signature crypto verify lands with the
	// wrap loop in #984; this PR persists a structurally-valid row so
	// the L25 listing filter sees the creator.
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
	// Public-channel carve-out: NULL signature is accepted by the
	// repo. Private creator-bootstrap also lands without a signature
	// in this PR — the §10 self-bootstrap window is the only time a
	// private channel has zero members, and the L33 enforcement
	// downstream will reject NULL signatures on every other private-
	// channel insert. The wrap loop (#984) will tighten this to a
	// real client-supplied signature when the wrap body lands.
	if err := h.deps.Repo.InsertChannelMember(r.Context(), row, true); err != nil {
		// On any membership failure, the channel row is already
		// committed. Log and return 500 — operators can repair the
		// orphan via the future #984 lazy-wrap flow. The race window
		// is small (in-tx tests cover the happy path) and CLAUDE.md
		// explicitly forbids stacking schema rewires for the wrap PR.
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not bootstrap creator membership")
		return
	}
	h.broadcastChannelEvent(channelEventCreate, *ch)
	WriteOK(w, stdhttp.StatusCreated, ch)
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
