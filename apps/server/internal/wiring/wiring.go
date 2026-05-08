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
	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/repo"
)

// Deps is the set of process-wide dependencies a feature may need.
// Built once in main.go and passed by value into Build. CHAT_DB_PATH,
// CHAT_JWT_SECRET, and CHAT_INVITE_CODE are all required at startup
// (config.Validate enforces them), so every field below is non-nil /
// non-empty by the time Build runs.
type Deps struct {
	// Hub is the in-process WebSocket fan-out.
	Hub *hub.Hub

	// Repo is the SQLite-backed repository.
	Repo *repo.Repo

	// JWTSecret is the HMAC signing key for issued JWTs.
	JWTSecret []byte

	// InviteCode gates POST /api/auth/register.
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

	// Read CHAT_TRUSTED_PROXY once per Build (process startup) rather
	// than on every request. Threaded into AccessLog (remote_ip field)
	// and into registerAuth (clientIP for audit + IP rate-limit key).
	trustedProxy := config.LoadTrustedProxy()

	registerSeed(deps)
	authFeature := registerAuth(mux, deps, trustedProxy)
	registerChannels(mux, deps, authFeature.Require)
	registerDMs(mux, deps, authFeature.Require)
	registerPresence(mux, deps, authFeature.Require)
	registerUsers(mux, deps, authFeature.Require)
	registerWS(mux, deps, authFeature.Tickets)
	registerPresenceUsername(deps)
	registerPanicProbe(mux)
	registerHealth(mux, deps)
	registerWeb(mux)

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
			httpapi.AccessLog(trustedProxy,
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
