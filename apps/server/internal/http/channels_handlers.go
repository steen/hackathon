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
)

// channelNameRe pins the friend-group-safe shape: 1-40 chars, ASCII
// lowercase letters, digits, hyphens. Lowercase-only avoids the
// Slack-style "is #General the same as #general?" foot-gun.
var channelNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

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

func NewChannelsHandlers(deps ChannelsDeps) *ChannelsHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &ChannelsHandlers{deps: deps}
}

// List handles GET /api/channels. Must be wrapped in auth.RequireJWT.
func (h *ChannelsHandlers) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	chans, err := h.deps.Repo.ListChannels(r.Context())
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not list channels")
		return
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"channels": chans})
}

// Create handles POST /api/channels. Must be wrapped in auth.RequireJWT.
func (h *ChannelsHandlers) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
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
	id := ids.NewULID()
	ch, err := h.deps.Repo.CreateChannel(r.Context(), id, name, h.deps.Now())
	if err != nil {
		if errors.Is(err, repo.ErrChannelNameTaken) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "channel name already taken")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create channel")
		return
	}
	WriteOK(w, stdhttp.StatusCreated, ch)
}

// Routes registers /api/channels and /api/channels/{id}/messages on mux,
// wrapping every handler in the supplied auth middleware. Path-pattern
// matching uses Go 1.22+ ServeMux placeholders (`{id}`); handlers read
// the channel id with r.PathValue("id").
func (h *ChannelsHandlers) Routes(
	mux *stdhttp.ServeMux,
	require func(stdhttp.Handler) stdhttp.Handler,
	messages *MessagesHandlers,
) {
	mux.Handle("GET /api/channels", require(stdhttp.HandlerFunc(h.List)))
	mux.Handle("POST /api/channels", require(stdhttp.HandlerFunc(h.Create)))
	mux.Handle("GET /api/channels/{id}/messages", require(stdhttp.HandlerFunc(messages.List)))
	mux.Handle("POST /api/channels/{id}/messages", require(stdhttp.HandlerFunc(messages.Create)))
}

// channelIDFromPath returns the id segment of /api/channels/{id}/messages,
// validated as a 26-char ULID-ish token. Tightly scoped here so a typo
// in the route pattern surfaces as a 400 rather than a SQL lookup with
// untrusted input.
func channelIDFromPath(r *stdhttp.Request) (string, bool) {
	id := r.PathValue("id")
	if len(id) != 26 {
		return "", false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			return "", false
		}
	}
	return id, true
}

// userFromContext is the small accessor that maps RequireJWT's context
// values onto the (id, username) pair the handlers need together.
func userFromContext(r *stdhttp.Request) (string, string, bool) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		return "", "", false
	}
	uname, _ := auth.UsernameFromContext(r.Context())
	return uid, uname, true
}
