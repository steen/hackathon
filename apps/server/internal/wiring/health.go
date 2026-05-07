package wiring

import (
	"context"
	"net/http"
	"time"

	httpapi "hackathon/apps/server/internal/http"
)

// healthPingTimeout bounds the DB round-trip /readyz attempts. Held at
// 1s so a wedged DB returns 503 fast enough for an orchestrator's
// healthcheck cadence (Compose default interval is 30s, timeout 30s);
// a longer ceiling here would let one stuck readiness probe pile up
// behind the next.
const healthPingTimeout = time.Second

// registerHealth wires GET /healthz (always 200) and GET /readyz
// (DB-bound). Must be registered before registerWeb so the SPA "/"
// fallback never shadows the healthz/readyz patterns. ServeMux
// longest-prefix matching protects us either way for these explicit
// patterns, but the ordering keeps the dependency obvious.
//
// In the no-DB phase-0 boot path (Deps.Repo == nil) /readyz returns
// 200: there is no DB to be unready against, and a 503 there would
// break smoke.sh's no-DB modes.
func registerHealth(mux *http.ServeMux, deps Deps) {
	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteOK(w, http.StatusOK, map[string]any{"status": "ok"})
	}))

	mux.Handle("GET /readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Repo == nil {
			httpapi.WriteOK(w, http.StatusOK, map[string]any{"status": "ok"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
		defer cancel()
		if err := deps.Repo.DB().PingContext(ctx); err != nil {
			httpapi.WriteError(w, http.StatusServiceUnavailable, "not_ready", "database ping failed")
			return
		}
		httpapi.WriteOK(w, http.StatusOK, map[string]any{"status": "ok"})
	}))
}
