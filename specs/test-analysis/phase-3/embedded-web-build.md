---
feature: embedded-web-build
phase: phase-3
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: stub
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# E2E test analysis: Embedded web build into Go binary

**Spec:** `specs/plans/phase-3/20-feature-embedded-web-build.md`
**Implementation status:** stub — `apps/server/main.go` has no `//go:embed`, no `http.FileServer`, no `apps/server/internal/web/` package. Verified by reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/server/main.go` (no `embed` import; mux only handles `/debug/subs`, `/api/*`, `/ws`) and `ls /Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/server/internal/` (no `web/` directory; only `auth config db http hub ids ratelimit repo wsapi`). The web app source exists at `apps/web/index.html` (`<div id="root"></div>` marker) but is not embedded.
**E2E test directory:** `tests/e2e/phase-3/embedded-web-build/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | The Vite production build of `apps/web` is embedded into the Go server binary using `embed.FS`. | deferred | — |
| AC-2 | When the server runs, requests for non-API paths serve the embedded SPA (with SPA fallback to `index.html`). | deferred | — |
| AC-3 | The build script (`pnpm build`) produces `apps/web/dist`, then `go build` of the server picks up those assets via `//go:embed`. | deferred | — |
| AC-4 | The CSP set in `feature-file-perms-and-headers.md` is compatible with the embedded assets (same-origin scripts/styles). | deferred | — |

## Findings

### Missing E2E tests

None — feature is stub.

### Deferred E2E tests

All 4 ACs deferred. When implementation lands, the test shape should be a single Go test file at `tests/e2e/phase-3/embedded-web-build/serve_test.go`:

- **AC-1 / AC-3 (embed compiles in):** Two-stage shell-out test. Stage 1: `pnpm --filter web build` to produce `apps/web/dist`. Stage 2: `go build -o bin/server ./apps/server`. Both must exit 0. Failure here means `//go:embed` either didn't find the dist tree or the build orchestration is broken. Run from `repoRoot(t)` with `cmd.Dir` set; capture stderr on failure for debugging.
  - Speed-up: skip stage 1 if `apps/web/dist/index.html` already exists and is newer than every file in `apps/web/src/`. Mirrors what a developer's iteration loop expects.
- **AC-2 (SPA serve + fallback):** boot the freshly built binary with NO external web dir mounted, on a random port (`freePort(t)`), with the minimum env (`CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=test-invite`, `CHAT_DB_PATH=` plus a tempfile). Wait for ready (poll `/debug/subs?channel=#general` until 200 or 5s budget). Then:
  - `GET /` -> body must contain `<div id="root">` (the index.html marker observed in `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/web/index.html`) AND `Content-Type: text/html`.
  - `GET /some/spa/route/that/does/not/exist` -> same body (SPA fallback). Asserts the fallback covers arbitrary client-routed paths (the spec is explicit about this in implementation step 2).
  - `GET /api/channels` (with a valid JWT) -> JSON envelope (200 with `{"ok":true,...}`). Asserts the static handler does NOT shadow `/api/*` — this is the spec's `test_api_paths_are_not_shadowed_by_static_handler` case mapped onto an integration test.
  - `GET /ws` (HTTP, no upgrade) -> 426 Upgrade Required or whatever the existing handler returns; key point is the static handler does NOT respond with index.html. Asserts handler precedence per spec implementation step 3.
- **AC-4 (CSP compat):** `GET /` -> assert the `Content-Security-Policy` response header is the verbatim PRD §9 string (already pinned by `apps/server/internal/http/headers_middleware.go` per the changelog entry from 2026-05-03 16:45Z), then assert that the embedded `index.html` body's inline `<script>` / `<style>` references are all same-origin (no `cdn.example`, no `unpkg`, no inline-script that the CSP would refuse). Cheap parse — no headless browser needed; the CSP from the existing middleware is `default-src 'self'`-shaped and the Vite build emits `/assets/index-<hash>.js` references which are same-origin by construction.
  - Optional stricter check (low value, defer): spin up `chromedp` and assert no CSP violation events fire when loading `/`. Heavy; the static parse above catches the realistic regressions.

### Helpers and harness notes

- This feature requires a SHELL-OUT test (build the binary fresh, run it, hit it over real HTTP). In-process `httptest.Server` won't work — it can't exercise `//go:embed` because the embedded FS is fixed at compile time of the binary, not at test compile time.
- Reuse the binary-startup pattern from `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/tests/server-ws-hub/hub_test.go::startServer` (line 45) — it already does `go build`, picks a free port, sets env, waits for ready. Extend it to gate on `pnpm --filter web build` running first when this feature's tests run.
- Speed: cache `apps/web/dist` and `bin/server` between test runs in CI by keying on `pnpm-lock.yaml` + `apps/web/src/**` + `apps/server/**` content hash. Without caching, each run is ~30s of pnpm + go build before the assertions even start.
- Cleanup: `t.Cleanup` must SIGTERM the spawned binary AND remove the temp DB file.
- The embedded `index.html` marker is `<div id="root"></div>` — verified by reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/web/index.html` line 8. This is stable across Vite builds (Vite preserves the source `index.html` and only rewrites the script src).

## Recommendations for /test-implement

1. Land the test file with all 4 cases as `t.Skip("embedded web build not implemented — see specs/plans/phase-3/20-feature-embedded-web-build.md")` until the impl PR merges.
2. After the impl PR merges, un-skip in the order: AC-3 (build) -> AC-1 (binary contains assets, can be verified by `go run ./apps/server` no-DB and curl `/`) -> AC-2 (SPA fallback semantics) -> AC-4 (CSP header).
3. Coordinate with `40-feature-single-binary-demo-verified` — that feature's E2E test will need the embedded web to be working too. Suggest landing this feature's test first and having the single-binary test depend on it (via a small shared helper that builds the binary once per test run).
4. Add a CI job step that runs `pnpm --filter web build` before `go test ./tests/...` so AC-3 doesn't fail spuriously due to a missing dist tree on a fresh checkout.
