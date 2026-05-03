# Feature: Align auth endpoint paths with PRD §10

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Why this exists

Follow-up to [feature-auth-endpoints](./feature-auth-endpoints.md) (PR #38, status flipped to `done`). Auditing the merged code against PRD §10 surfaces a path-naming divergence:

| PRD §10 (lines 362–376) | Plan AC + merged code | Files |
|---|---|---|
| `POST /api/auth/register`   | `POST /api/register`   | `apps/server/internal/http/auth_handlers_test.go:?` (`case "/api/register":`), `scripts/smoke.sh:?` (`/api/register`), `apps/server/main.go:99` |
| `POST /api/auth/login`      | `POST /api/login`      | same as above |
| `GET  /api/auth/me`         | `GET  /api/me`         | `main.go:101` |
| `POST /api/auth/logout`     | `POST /api/logout`     | `main.go:102` |
| `POST /api/auth/ws-ticket`  | `POST /api/ws-ticket`  | `main.go:103` |

The plan's acceptance criteria themselves used the no-`/auth/` paths, so the implementer followed the plan correctly — but the plan diverged from the PRD on this point. The PRD is the contract clients (CLI Phase 2, Web Phase 2) will be built against; if the server keeps the current paths, the future `packages/api-client` and `packages/go-client` will not match the documented API.

This plan calls for aligning the live paths with PRD §10 before the Phase-2 clients wire up.

## Requirements covered

- PRD §10 lines 362–376 — base URL `http://127.0.0.1:8080`, all auth endpoints under `/api/auth/`.
- US-1 (register), US-2 (login), US-12 (logout) — endpoints clients will call to satisfy these stories.

## Acceptance criteria

- The server registers handlers at `/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`, `/api/auth/ws-ticket` (matching PRD §10).
- Existing tests (`apps/server/internal/http/auth_handlers_test.go`) and `scripts/smoke.sh` are updated to use the new paths and continue to pass.
- No other behavioural change — request/response shapes, JWT semantics, ticket TTL, audit-log entries all remain as PR #38 implemented them. This plan is path-only.
- Either the parent plan (`feature-auth-endpoints.md`) is amended to reflect `/api/auth/*` paths, or it is left alone with a one-line note that the PRD wins. Either is acceptable; not aligning the parent plan would be misleading.

## Implementation steps

1. Update the route table in `apps/server/main.go:99-103` from `/api/<verb>` to `/api/auth/<verb>` for register, login, me, logout, ws-ticket. Leave `/ws` and `/debug/subs` untouched.
2. Update `apps/server/internal/http/auth_handlers_test.go` — the `switch path` block (`case "/api/register":` etc.) and every `f.post(t, "/api/...", ...)` call site.
3. Update `scripts/smoke.sh` — the three `${API_URL}/api/...` curl invocations for register, login, ws-ticket.
4. Update `specs/plans/phase-1/feature-auth-endpoints.md` acceptance criteria to use `/api/auth/<verb>` (or add a one-line "paths match PRD §10" note).
5. Sanity-check the CHANGELOG entry in `CHANGELOG.md` for PR #38 — if it references `/api/<verb>`, fix the entry too.

## Test plan

- Re-run `go test ./...` after the path swap; the existing `auth_handlers_test.go` cases must continue to pass against the new paths (the test file is updated in step 2 to use the new paths).
- Re-run `bash scripts/smoke.sh` end-to-end; the script must continue to exit 0.
- Spot-check that no other code references the old `/api/<verb>` paths (grep for `/api/register`, `/api/login`, `/api/me`, `/api/logout`, `/api/ws-ticket` after the change — should return zero hits outside of CHANGELOG history entries).

## Files expected to be touched

- `apps/server/main.go` (5 route registrations)
- `apps/server/internal/http/auth_handlers_test.go` (test paths)
- `scripts/smoke.sh` (curl invocations)
- `specs/plans/phase-1/feature-auth-endpoints.md` (AC text alignment)
- `CHANGELOG.md` (only if the PR #38 entry quotes the old paths)

## Risks

- A reverse proxy or environment variable somewhere assumes the old paths — none observed in the repo today, but worth a `grep -ri '/api/register'` sweep before the change lands.
- The Phase 2 `packages/api-client` and `packages/go-client` plans (`10-feature-go-client-package.md`, `30-feature-ts-api-client-package.md`) do not yet pin path strings; aligning before they implement avoids a coordinated change later.
