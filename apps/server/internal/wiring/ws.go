package wiring

import (
	"net/http"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/wsapi"
)

// registerWS wires the WebSocket upgrade handler at /ws and the
// /debug/subs gauge. tickets is the store registerAuth filled; nil
// in the no-DB boot path means /ws skips ticket enforcement (see
// wsapi.Handler's docstring: "When ts is nil, ticket enforcement is
// skipped. This branch exists for the phase-0 smoke wiring and for
// tests that exercise the hub fan-out without standing up the auth
// stack."). /debug/subs is registered unconditionally so smoke
// scripts can read it even without auth.
func registerWS(mux *http.ServeMux, deps Deps, tickets *auth.TicketStore) {
	wsCfg := wsapi.Config{OriginPatterns: deps.AllowedOrigins}
	if deps.Repo != nil {
		wsCfg.ChannelLookup = deps.Repo.ChannelExists
	}

	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(deps.Hub))
	mux.HandleFunc("/ws", wsapi.Handler(deps.Hub, tickets, wsCfg))
}
