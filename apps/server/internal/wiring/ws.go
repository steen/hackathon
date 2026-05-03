package wiring

import (
	"net/http"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/wsapi"
)

// registerWS wires the WebSocket upgrade handler at /ws and the
// /debug/subs gauge. tickets is the store registerAuth filled; nil
// in the no-DB boot path means /ws will reject every upgrade with
// the wsapi handler's own "ticket store unavailable" branch (if any)
// — but the route is still registered so /debug/subs stays useful
// in smoke contexts that don't need auth.
func registerWS(mux *http.ServeMux, deps Deps, tickets *auth.TicketStore) {
	wsCfg := wsapi.Config{OriginPatterns: deps.AllowedOrigins}
	if deps.Repo != nil {
		wsCfg.ChannelLookup = deps.Repo.ChannelExists
	}

	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(deps.Hub))
	mux.HandleFunc("/ws", wsapi.Handler(deps.Hub, tickets, wsCfg))
}
