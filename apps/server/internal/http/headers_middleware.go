// Package http hosts cross-cutting HTTP middleware for the chat server.
package http

import "net/http"

// CSP is the Content-Security-Policy applied to every response. Kept verbatim
// from PRD §9; do not edit without updating the spec.
const CSP = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// SecurityHeaders wraps next so every response — including errors written by
// inner handlers — carries the SEC-10 baseline headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
