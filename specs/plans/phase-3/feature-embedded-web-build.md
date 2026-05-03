# Feature: Embedded web build into Go binary

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** planned

## Requirements covered
- (supports US-10; see `feature-single-binary-demo-verified.md` for the full single-binary verification)

## Acceptance criteria
- The Vite production build of `apps/web` is embedded into the Go server binary using `embed.FS`.
- When the server runs, requests for non-API paths serve the embedded SPA (with SPA fallback to `index.html`).
- The build script (`pnpm build`) produces `apps/web/dist`, then `go build` of the server picks up those assets via `//go:embed`.
- The CSP set in `feature-file-perms-and-headers.md` is compatible with the embedded assets (same-origin scripts/styles).

## Implementation steps
1. Add `apps/server/internal/web/embed.go` with `//go:embed all:dist` referencing the built web app's dist directory (via a relative path or a make-step copy into `apps/server/internal/web/dist`).
2. Implement an `http.FileServer` over the embedded FS with SPA fallback (any unmatched path returns `index.html`).
3. Mount the static handler last (after `/api` and `/ws`).
4. Add a build orchestration step (root `package.json` `build` or a Makefile target) that runs `pnpm --filter web build` then `go build ./apps/server`.
5. Verify the CSP allows `'self'` for scripts and styles needed by the SPA.

## Test plan
- `test_root_path_serves_embedded_index_html` — covers single-binary serving.
- `test_unknown_spa_path_falls_back_to_index_html` — covers SPA routing.
- `test_api_paths_are_not_shadowed_by_static_handler` — covers handler precedence.
- Manual: build the binary, run it, browse to `/`, confirm the chat UI loads.

## Files expected to be touched or created
- `apps/server/internal/web/embed.go`
- `apps/server/internal/web/embed_test.go`
- Build glue in `package.json` or `Makefile`
- `apps/server/main.go` (mount static handler)

## Risks
- `embed.FS` requires the dist directory to exist at compile time; mitigated by the orchestrated build step ordering (`web build` before `go build`).
