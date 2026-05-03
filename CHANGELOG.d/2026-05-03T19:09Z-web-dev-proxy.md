### Changed

- `apps/web` (vite dev server): proxy `/api` and `/ws` to `127.0.0.1:8080` so the SPA stays same-origin in dev. Without this, browsers running the dev server on `:5173` hit the API on `:8080` cross-origin and fail CORS preflight (the Go server has no HTTP CORS middleware — only the WS upgrade enforces an origin allowlist via `CHAT_ALLOWED_ORIGINS`). Production behavior is unchanged: vite proxies are dev-only. (2026-05-03T19:09Z)
