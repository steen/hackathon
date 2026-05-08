package wiring

import (
	"net/http"

	httpapi "hackathon/apps/server/internal/http"
)

// registerUsers wires GET /api/users behind the JWT middleware. The
// frontend uses it to resolve sender_user_id -> username for senders
// who are not currently online (and therefore absent from
// /api/presence).
func registerUsers(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	users := httpapi.NewUsersHandlers(httpapi.UsersDeps{
		DB: deps.Repo.DB(),
	})
	mux.Handle("GET /api/users", require(http.HandlerFunc(users.List)))
}
