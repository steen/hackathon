package wiring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/seed"
	"hackathon/apps/server/internal/wsapi"
)

// registerWS wires the WebSocket upgrade handler at /ws and the
// /debug/subs gauge. tickets is the store registerAuth filled.
//
// DefaultChannelResolver implements the L15 default-channel fallback
// from specs/plans/phase-9/ws-routing.md: an /ws upgrade with no
// ?channel= lands on the seeded `general` channel id. The id is
// looked up once via the repo's ListChannels, then cached because the
// seeded `general` channel cannot be deleted or renamed (the HTTP
// surface rejects rename of the `general` channel with 403, and there
// is no delete endpoint), so a single boot-time resolution is correct
// for the lifetime of the process.
func registerWS(mux *http.ServeMux, deps Deps, tickets *auth.TicketStore) {
	resolver := newDefaultChannelResolver(deps)

	wsCfg := wsapi.Config{
		OriginPatterns:         deps.AllowedOrigins,
		ChannelLookup:          deps.Repo.ChannelExists,
		DefaultChannelResolver: resolver,
	}

	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(deps.Hub))
	mux.HandleFunc("/ws", wsapi.Handler(deps.Hub, tickets, wsCfg))
}

// newDefaultChannelResolver returns a closure that resolves the seeded
// `general` channel id and caches it. The first call hits the DB; every
// subsequent call returns the cached id without a DB round-trip.
//
// Concurrency: double-checked locking on a sync.Mutex, with the cached
// id stored in an atomic.Pointer so the fast path takes no lock.
// Failures bail before the atomic.Store, so a transient DB error
// retries on the next upgrade rather than wedging the fallback for the
// whole process lifetime.
func newDefaultChannelResolver(deps Deps) func(ctx context.Context) (string, error) {
	var (
		cached atomic.Pointer[string]
		mu     sync.Mutex
	)
	return func(ctx context.Context) (string, error) {
		if id := cached.Load(); id != nil {
			return *id, nil
		}
		mu.Lock()
		defer mu.Unlock()
		if id := cached.Load(); id != nil {
			return *id, nil
		}
		channels, err := deps.Repo.ListChannels(ctx)
		if err != nil {
			return "", fmt.Errorf("default-channel resolver: list channels: %w", err)
		}
		for _, ch := range channels {
			if ch.Name == seed.GeneralChannelName {
				id := ch.ID
				cached.Store(&id)
				return id, nil
			}
		}
		return "", errors.New("default-channel resolver: seeded `general` channel not found")
	}
}
