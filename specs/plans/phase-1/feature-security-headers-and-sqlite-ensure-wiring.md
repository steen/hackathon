# Feature: Wire SecurityHeaders middleware and call EnsureFile at startup

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Why this exists

Follow-up to [feature-file-perms-and-headers](./feature-file-perms-and-headers.md) (PR #26, status flipped to `implemented`). Auditing the merged code against the plan's acceptance criteria:

- The `SecurityHeaders` middleware exists in `apps/server/internal/http/headers_middleware.go` and has a unit test, but `apps/server/main.go` does not wrap the mux with it (the current chain is `Handler: httpx.BodyCap(mux)`, no `SecurityHeaders`). The plan AC "Headers are present on both 2xx and error responses" and step 3 ("Wire the middleware as the outermost layer") are unmet at runtime.
- The `db.EnsureFile` helper exists in `apps/server/internal/db/perms.go` and has a unit test, but no caller exists yet. `apps/server/main.go` does not open SQLite or call `EnsureFile`. The plan AC "The SQLite database file is created with mode 0600" therefore has no runtime effect; it relies on the future `feature-sqlite-schema-and-ulid` integration to invoke it.

This plan is companion-scoped with the access-log-fields-and-wiring plan in PR #31 (will land at `./feature-access-log-fields-and-wiring.md` once #31 merges), which already plans the wiring of `RequestIDMiddleware`, `AccessLog`, and `Recover` around the mux. `SecurityHeaders` belongs in the same chain; this plan adds it explicitly so neither follow-up drops it.

## Requirements covered

- PRD §9 SEC-10 — "Response headers include CSP, X-Content-Type-Options, Referrer-Policy, X-Frame-Options."
- PRD §9 SEC-14 — "SQLite file is mode 0600 after first boot."
- Parent plan AC line 12-17 (headers on all responses) and AC line 11 (file mode).

## Acceptance criteria

- The HTTP server's `Handler` is built so every response — including those written by `Recover` after a panic and those produced by the `/ws` upgrade path — carries the four SEC-10 headers (`Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`).
- `SecurityHeaders` is layered as the outermost middleware so even error envelopes written by inner layers inherit the headers (per parent plan AC).
- `db.EnsureFile(path)` is invoked from `apps/server/main.go` at startup before any code opens the SQLite file. The path comes from the same env var (`CHAT_DB_PATH`, PRD §9 default `./chat.db`) that the future SQLite open will read.
- A startup smoke test (or main-level test) asserts that, after `main` boots against a fresh temp dir, the configured DB file exists with mode `0600`.

## Implementation steps

1. Build the handler chain in `apps/server/main.go` such that `SecurityHeaders` is the outermost wrapper. Concretely, on top of the chain planned in `feature-access-log-fields-and-wiring.md`: `handler := SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(mux))))`. If that companion plan lands first, this plan only adds the `SecurityHeaders(...)` wrap; if this plan lands first, the simpler chain `handler := SecurityHeaders(mux)` is acceptable as an interim until the companion plan extends it.
2. In `apps/server/main.go`, read `CHAT_DB_PATH` (default `./chat.db` per PRD §9) and call `db.EnsureFile(path)` before `srv.ListenAndServe()`. Fatal-exit on error so an operator sees a clear startup failure rather than a downstream open error.
3. Add an end-to-end test under `apps/server/` that boots the server against a temp `CHAT_DB_PATH`, issues a single request, and asserts:
   - The response carries all four SEC-10 headers (string-equality on values).
   - The DB file at the configured path exists with `os.Stat().Mode().Perm() == 0o600`.
4. Confirm that the `/ws` upgrade path continues to work through the wrapped chain (the existing `statusRecorder.Hijack` from `middleware.go` is the regression guard; this plan only adds another wrap on top).

## Test plan

- `test_response_includes_all_four_security_headers_through_main_chain` — covers SEC-10 end-to-end.
- `test_panic_handler_response_still_carries_security_headers` — covers parent AC "errors also carry headers".
- `test_main_creates_db_file_with_0600` — covers SEC-14 at runtime.
- `test_main_fails_fast_on_uncreatable_db_path` — covers the fatal-on-error behaviour.
- `test_ws_upgrade_succeeds_through_security_headers_chain` — regression guard for the hijack path.

## Files expected to be touched or created

- `apps/server/main.go` (call `EnsureFile`; wrap mux with `SecurityHeaders`).
- `apps/server/main_security_test.go` *(new)* — startup-level checks for headers + file mode.

## Risks

- Setting headers as the outermost layer means a panic *inside* `Recover` (extremely unlikely) would still get headers via `SecurityHeaders` — desirable. Setting `SecurityHeaders` as innermost would lose the headers on Recover's emergency response, which is why outermost is required.
- `CHAT_DB_PATH` may not yet be read by any other code at the time this plan lands; that is fine — `EnsureFile` only needs the path string. The future SQLite open feature must use the same env var name to keep the file the one that was chmodded.
