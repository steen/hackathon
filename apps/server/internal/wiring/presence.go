package wiring

import (
	"net/http"

	httpapi "hackathon/apps/server/internal/http"
)

// registerPresence wires GET /api/presence behind the JWT middleware.
func registerPresence(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	presence := httpapi.NewPresenceHandlers(httpapi.PresenceDeps{
		Hub: deps.Hub,
		DB:  deps.Repo.DB(),
	})
	mux.Handle("GET /api/presence", require(http.HandlerFunc(presence.List)))
}
