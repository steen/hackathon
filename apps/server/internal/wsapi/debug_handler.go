package wsapi

import (
	"fmt"
	"net/http"

	"hackathon/apps/server/internal/hub"
)

// DebugSubsHandler returns an http.HandlerFunc serving GET /debug/subs.
//
// It exists to remove a startup race in tests (e.g. scripts/smoke.sh) where
// publishers would send before subscribers had finished registering with the
// hub. Pollers can hit this endpoint until the count reaches the expected
// value rather than guessing with sleeps.
//
// Response shape is plain text — a single decimal integer followed by "\n".
// This deliberately diverges from the {ok,data,error} envelope used by
// product API endpoints: callers are CI scripts and tests, the body is
// trivially parseable with grep/awk, and there is no schema to evolve.
// The /debug/ prefix marks it as not part of the public surface.
//
// Query parameter:
//
//	channel  — channel name to query. Required. Must be non-empty.
func DebugSubsHandler(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			http.Error(w, "channel query parameter is required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%d\n", h.SubscriberCount(channel))
	}
}
