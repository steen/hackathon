package wsapi

import (
	"fmt"
	"net"
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
// Access is restricted to loopback (127.0.0.1 / ::1) so a public-bound
// listener (CHAT_ALLOW_PUBLIC_BIND=1) cannot be used to enumerate channel
// presence. Non-loopback peers see 404 — same response as a missing route,
// so the endpoint's existence is not advertised to a port scan.
//
// Query parameter:
//
//	channel  — channel name to query. Required. Must be non-empty.
func DebugSubsHandler(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet) //nolint:gosec // G705 false positive: literal string, no taint reaches the header.
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			http.Error(w, "channel query parameter is required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "%d\n", h.SubscriberCount(channel)) //nolint:gosec // G705: %d renders an int; no taint can reach the output.
	}
}

// isLoopback reports whether remoteAddr's host portion is an IPv4 or IPv6
// loopback address. net.IP.IsLoopback handles 127.0.0.0/8, ::1, and the
// IPv4-mapped IPv6 form (e.g. "::ffff:127.0.0.1") uniformly.
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
