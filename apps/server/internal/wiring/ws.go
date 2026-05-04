package wiring

import (
	"net/http"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/wsapi"
)

// registerWS wires the WebSocket upgrade handler at /ws and the
// /debug/subs gauge. tickets is the store registerAuth filled; when
// nil (the no-DB boot path) the wsapi handler skips ticket validation
// and accepts anonymous upgrades (userID = ""). /debug/subs stays
// useful in smoke contexts that do not need auth.
func registerWS(mux *http.ServeMux, deps Deps, tickets *auth.TicketStore) {
	wsCfg := wsapi.Config{OriginPatterns: deps.AllowedOrigins}
	if deps.Repo != nil {
		wsCfg.ChannelLookup = deps.Repo.ChannelExists
	}

	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(deps.Hub))
	mux.HandleFunc("/ws", wsapi.Handler(deps.Hub, tickets, wsCfg))
}
