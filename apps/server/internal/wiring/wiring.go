// Package wiring composes the server's HTTP handler from its feature
// packages. main.go calls Build(deps) once at startup; each feature
// owns one file in this package that registers its routes on the mux.
//
// The package exists to keep apps/server/main.go off the conflict-magnet
// list. New features add a wiring/<feature>.go file plus one line in
// Build — no edits to main.go or to other features' wiring files.
package wiring

import (
	"net/http"

	"hackathon/apps/server/internal/auth"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/repo"
)

// Deps is the set of process-wide dependencies a feature may need.
// Built once in main.go and passed by value into Build. Optional
// dependencies (anything that is nil in the phase-0 boot path with
// no DB) are documented per field.
type Deps struct {
	// Hub is the in-process WebSocket fan-out. Always non-nil.
	Hub *hub.Hub

	// Repo is the SQLite-backed repository. Nil when CHAT_DB_PATH is
	// unset (phase-0 boot path, scripts/smoke.sh's no-DB modes); auth
	// and channels features skip their registrations in that case.
	Repo *repo.Repo

	// JWTSecret is the HMAC signing key for issued JWTs. Empty when
	// Repo is nil; main.go validates the pair before constructing
	// Deps so wiring code can treat (Repo != nil) == (len(JWTSecret) > 0).
	JWTSecret []byte

	// InviteCode gates POST /api/auth/register. May be empty when
	// Repo is nil.
	InviteCode string

	// AllowedOrigins is the parsed CHAT_ALLOWED_ORIGINS list, passed
	// through to the WebSocket origin check. Nil means "same-origin
	// only" per coder/websocket defaults.
	AllowedOrigins []string
}

// Build constructs the http.Handler the server listens with. The
// returned handler is the full middleware chain wrapping the mux;
// callers attach it to an http.Server without further decoration.
//
// Order of wiring (do not reorder casually):
//  1. Auth feature runs first because it constructs the RequireJWT
//     middleware everything else (channels, presence) wraps around.
//  2. Channels and presence depend on Repo + the Require middleware.
//  3. WS registers last so its ticket store is the one auth created.
//
// Each feature file (auth.go, channels.go, ...) exposes one register
// function that takes the mux and the deps it needs. Adding a new
// feature is: drop a file, add one line below.
func Build(deps Deps) http.Handler {
	mux := http.NewServeMux()

	authFeature := registerAuth(mux, deps)
	registerChannels(mux, deps, authFeature.Require)
	registerPresence(mux, deps, authFeature.Require)
	registerWS(mux, deps, authFeature.Tickets)

	// Middleware order (outer → inner):
	//   SecurityHeaders      → SEC-10 baseline headers on every response
	//   RequestIDMiddleware  → assigns X-Request-Id, plumbs ctx
	//   AccessLog            → emits one log line per request (uses ctx)
	//   Recover              → catches panics, writes generic 500
	//   BodyCap              → caps request body to RESTBodyLimit
	//   mux                  → handler dispatch
	// SecurityHeaders is outermost so even error responses written by
	// inner middleware (Recover's 500, BodyCap's 413) carry the headers.
	// statusRecorder.Hijack forwards the Hijacker interface so /ws
	// upgrade still works through this chain.
	return httpapi.SecurityHeaders(
		httpapi.RequestIDMiddleware(
			httpapi.AccessLog(
				httpapi.Recover(
					httpapi.BodyCap(mux),
				),
			),
		),
	)
}

// authBundle is the auth feature's return value: the JWT middleware
// other features wrap around, and the ticket store the WS feature
// validates upgrades against. Returned (rather than stashed in Deps)
// so the auth feature owns its lifetime.
type authBundle struct {
	Require func(http.Handler) http.Handler
	Tickets *auth.TicketStore
}
