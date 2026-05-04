//go:build panicprobe

package wiring

import "net/http"

// registerPanicProbe registers GET /debug/panic, a route whose handler
// always panics. It exists so e2e tests can exercise the Recover
// middleware's 500 response and assert AC-1 of
// specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md
// (the four SEC-10 headers must ride panic-recovered responses).
//
// Gated behind the panicprobe build tag so it never compiles into a
// default chat-server binary. Tests that need it pass -tags=panicprobe
// to go build.
func registerPanicProbe(mux *http.ServeMux) {
	mux.Handle("GET /debug/panic", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("panicprobe: deliberate panic for AC-1 Recover coverage")
	}))
}
