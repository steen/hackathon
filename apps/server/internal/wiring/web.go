package wiring

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/web"
)

// registerWeb mounts the embedded SPA so the chat-server binary serves
// the production Vite build at non-API paths. Requests for files that
// exist in the embedded FS (e.g. /assets/index-*.js) stream from
// http.FileServer; everything else falls back to index.html so the
// SPA's client-side router resolves the path.
//
// Routing precedence relies on Go 1.22's longest-prefix match: the
// "/" pattern this function registers is the lowest-priority route,
// so any /api/auth/login, /ws, /debug/subs, etc. registered by other
// features wins ahead of the SPA fallback. To prevent unmatched paths
// under /api/, /ws/, or /debug/ from rendering the SPA HTML (which
// would be a confusing UX for a typo'd API call and would break
// machine clients that expect JSON envelopes), the SPA handler emits
// a JSON 404 envelope when those prefixes are seen.
//
// The 405 method-not-allowed behavior of method-specific routes
// (e.g. GET /api/presence) is preserved: those routes match the path
// at the mux level even when the method is wrong, so the mux returns
// 405 before this handler ever runs.
//
// Must be registered last in Build so the catch-all "/" pattern does
// not shadow features whose registration depends on Deps.Repo (which
// can be nil in the no-DB boot path).
func registerWeb(mux *http.ServeMux) {
	spaFS := web.FS()

	// Read index.html once at startup. The embedded FS is fixed at
	// compile time, so failure here means the build is broken; panic
	// is the right loud signal — production binaries never reach
	// run() without a usable SPA.
	indexBytes, err := fs.ReadFile(spaFS, "index.html")
	if err != nil {
		panic("web: read embedded index.html: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(spaFS))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reserve API/ws/debug subtrees from SPA fallback. These
		// prefixes only reach the catch-all when no specific route
		// matched, which means the path or method was wrong; emit a
		// JSON 404 envelope rather than the SPA HTML so machine
		// clients see a parseable error.
		if isReservedAPIPath(r.URL.Path) {
			httpapi.WriteError(w, http.StatusNotFound, httpapi.CodeNotFound, "not found")
			return
		}

		clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if clean == "" || clean == "." {
			writeSPAIndex(w, indexBytes)
			return
		}

		if _, err := fs.Stat(spaFS, clean); err != nil {
			// Path is not a built asset — SPA fallback so the
			// client-side router can resolve deep links like
			// /c/general or /login.
			writeSPAIndex(w, indexBytes)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
}

// reservedAPITopLevelPrefixes is the closed set of top-level URL
// prefixes that must NOT fall through to the SPA handler — anything
// under these belongs to a machine client (REST envelope, WS upgrade,
// debug gauge) and a miss should be a JSON 404, not SPA HTML.
//
// Contract: when a feature registers a brand-new top-level prefix on
// the wiring mux (e.g. /metrics, /healthz), append it here in the same
// PR. TestReservedPrefixesCoverWiringMux walks every mux.Handle/HandleFunc
// call site in this package and fails if a registered top-level prefix
// is missing from this list.
var reservedAPITopLevelPrefixes = []string{"/api", "/ws", "/debug", "/healthz", "/readyz"}

func isReservedAPIPath(p string) bool {
	for _, prefix := range reservedAPITopLevelPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func writeSPAIndex(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// no-cache so a stale browser cache doesn't trap users on an old
	// SPA after a deploy. Hashed asset bundles under /assets/ are
	// served by http.FileServer, which sets Last-Modified from the
	// embed.FS modtime and emits no Cache-Control header at all —
	// caching there relies on Vite's content-hashed filenames changing
	// the URL between deploys, not on a framework-set immutable hint.
	// This header only loosens caching for index.html.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
