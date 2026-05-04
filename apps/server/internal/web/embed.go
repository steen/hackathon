// Package web embeds the production build of apps/web (the Vite SPA)
// into the chat-server binary so the single-binary demo can serve the
// UI without an external static-file host.
//
// Build flow:
//
//  1. `pnpm --filter web build` produces apps/web/dist/.
//  2. The single-binary build script copies apps/web/dist/* into
//     apps/server/internal/web/dist/ (the directory this package
//     embeds). The repo's root .gitignore exempts that path so the
//     committed placeholder index.html stays tracked; the populated
//     dist contents stay untracked.
//  3. `go build ./apps/server` picks up whatever is in
//     apps/server/internal/web/dist/ at compile time via //go:embed.
//
// In CI's plain `go build ./...` job (no pnpm step), only the committed
// placeholder index.html is embedded, which keeps the build green and
// the embed.FS non-empty. Real demo binaries are produced via the
// orchestration script that runs the web build first.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded SPA filesystem rooted at dist/, so callers
// see paths like "index.html" rather than "dist/index.html".
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: the dist/ directory is part of the embed pattern
		// and the embed step would have failed at compile time if it
		// were missing. Panic so a future refactor that breaks the
		// invariant fails loudly rather than silently serving an empty
		// file system.
		panic("web: embed root dist/ missing: " + err.Error())
	}
	return sub
}
