package wiring

import (
	"net/http"

	httpapi "hackathon/apps/server/internal/http"
)

// registerChannels wires /api/channels and /api/channels/{id}/messages.
// require is the JWT middleware constructed by registerAuth — every
// channel + message route is gated through it.
//
// Skips registration when Deps.Repo or require is nil; both come from
// the auth feature which itself skips when there is no DB.
func registerChannels(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	if deps.Repo == nil || require == nil {
		return
	}

	ch := httpapi.NewChannelsHandlers(httpapi.ChannelsDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})
	msg := httpapi.NewMessagesHandlers(httpapi.MessagesDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})
	ch.Routes(mux, require, msg)
}
