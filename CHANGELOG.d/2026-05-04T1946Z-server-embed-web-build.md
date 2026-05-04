### Added

- Chat-server binary now embeds the production Vite build of `apps/web` via `//go:embed` and serves it from non-API paths with SPA fallback (deep links return `index.html` so the client-side router resolves them). Requests under `/api/`, `/ws/`, or `/debug/` that don't match a registered route return a JSON 404 envelope instead of SPA HTML so machine clients see a parseable error. `scripts/build-single-binary.sh` orchestrates the demo build (`pnpm --filter web build` → copy into the embed dir → `go build ./apps/server`).
