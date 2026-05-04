---
feature: single-binary-demo-verified
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

# E2E test analysis: Single-binary demo path verified

**Spec:** `specs/plans/phase-3/40-feature-single-binary-demo-verified.md`
**Implementation status:** stub — the binary builds and runs, but it does NOT serve the web app (no `embed.FS`, see `embedded-web-build` findings) and the README does not document the demo path (`README.md` is 3 lines, see `readme-quick-start` findings). There is no `pnpm demo` script (verified by reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/package.json` is feasible but the spec confirms "Neither target exists today" in implementation step 2). The auth-enabled boot path does work end-to-end at the API+WS level (extensively covered by phase-1 tests), so AC-1 is partially supportable today modulo the embedded web piece.
**E2E test directory:** `tests/e2e/phase-3/single-binary-demo-verified/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | A single Go binary, configured solely via env vars, serves both the API/WS and the embedded web app. | deferred | — |
| AC-2 | The binary's required env vars to enter the auth-enabled boot path are: `CHAT_JWT_SECRET`, `CHAT_INVITE_CODE`, and `CHAT_DB_PATH` (no default — `apps/server/main.go` falls back to phase-0 mode without persistence if unset). `CHAT_LISTEN_ADDR` defaults to `127.0.0.1:8080`. | deferred | — |
| AC-3 | A documented manual demo path: build the binary -> set the three required env vars -> run -> register via web -> send a message -> see it in CLI watch. | deferred | — |
| AC-4 | The Phase 3 validation criterion is met: clean clone -> `pnpm dev` -> full demo flow under 5 minutes. | deferred | — |

## Findings

### Missing E2E tests

None — feature is stub.

### Deferred E2E tests

All 4 ACs deferred. This feature is integration-flavored — it asserts the whole single-binary path works, not any one piece. Suggested test file: `tests/e2e/phase-3/single-binary-demo-verified/demo_test.go` (single Go test that shells out for the build and curl/WS for the assertions; bash is acceptable but Go is more portable across the repo's existing test infra).

- **AC-1 (one binary serves both):** depends on `embedded-web-build` shipping. After it does:
  - `go build -o bin/server ./apps/server`. Boot with the three required env vars on a random port.
  - `curl http://127.0.0.1:<port>/` -> body contains `<div id="root">` (web served).
  - `curl http://127.0.0.1:<port>/api/channels` with a JWT -> JSON envelope (API served).
  - `websocket dial ws://127.0.0.1:<port>/ws?ticket=<t>&channel=<id>` -> 101 Switching Protocols (WS served).
  - All three from the SAME binary, SAME port. That's the AC.
- **AC-2 (env-var contract):** four sub-cases:
  - All three set + valid -> server enters auth-enabled mode (verifiable by `POST /api/auth/register` returning 200 with the test invite code).
  - `CHAT_JWT_SECRET` unset, `CHAT_DB_PATH` set -> binary exits non-zero with the error from `apps/server/main.go:111-113` (`config: CHAT_JWT_SECRET must be set when CHAT_DB_PATH is set`). Pin the error text or at least the substring `CHAT_JWT_SECRET must be set` so a refactor doesn't silently drop the check.
  - `CHAT_INVITE_CODE` unset (others set) -> per `apps/server/internal/config/config.go` validation (changelog 2026-05-03 17:45Z) the binary should refuse to boot. Verify the actual behavior by reading `config.go::Validate()` at impl time — the spec phrases it as "required" but the current Validate may treat it as required-only-when-registration-enabled.
  - `CHAT_DB_PATH` unset (others set) -> binary boots in phase-0 mode (no auth, no channels). Verify by `GET /api/channels` returning 404 (no handler mounted) and `GET /ws` accepting an unauthenticated upgrade. This is the documented "fallback" path the spec calls out.
- **AC-3 (demo flow end-to-end):** the canonical happy path. One test that:
  - Boots the single binary on a random port.
  - `POST /api/auth/register` with the test invite -> capture token.
  - Open WS client #1 (the "CLI watch" stand-in): `POST /api/auth/ws-ticket` -> `ws://.../ws?ticket=<t>&channel=<general-channel-id>` -> read first frame.
  - `POST /api/channels/<general-id>/messages` with body `{"body":"hello from demo"}` (the "register via web -> send a message" stand-in; the actual UI click is out of scope for an E2E test, but the API call the UI makes is in scope).
  - Assert WS client #1 receives `{"type":"message","data":{...,"body":"hello from demo"}}` within 2s.
  - Plus: `curl /` returns `<div id="root">` proving the web is also served (the "register via web" half).
- **AC-4 ("under 5 minutes"):** this is half a marketing claim and half a test. The honest test:
  - From a fresh `git clone` (or `git worktree` simulation), shell out: `time (pnpm install && pnpm dev &)` (or whatever the README ends up documenting). Wait for ready by polling `/`. Assert wall-clock time from clone-equivalent to first successful `GET /` is `< 5 * 60 * time.Second`.
  - This test is brittle to CI runner speed. Suggest gating it behind a build tag (`//go:build slowdemo`) so it runs nightly, not on every PR. The faster ACs above (1-3) are the regression-catching tests; AC-4 is the periodic sanity check.
  - Skip the `pnpm install` measurement entirely if the test runner has a warm `node_modules` — measure only the build + boot + first-request leg, and make the 5-minute claim against a documented "pnpm install must complete first" preamble.

### Helpers and harness notes

- This feature's E2E test depends on TWO other phase-3 features being implemented: `embedded-web-build` (for the `/` -> index.html assertion) and `seed-general-channel` (so the demo flow has a channel to post to without an extra setup step). If those are stubs at test-implement time, the demo test should `t.Skip` with explicit references to the blocker specs.
- For shell-out, mirror `tests/server-ws-hub/hub_test.go::startServer` shape but with the FULL env-var triple (JWT secret + invite + DB path), not the phase-0 minimal env. The existing helper sets only `CHAT_SERVER_PORT`.
- The "second WS client to observe broadcast" pattern is already implemented in `tests/server-ws-hub/hub_test.go::TestAC2_ServerWsHub_HardcodedGeneralChannel` (line 132) — copy the dial + read-frame plumbing from there. Note that it uses raw text frames (phase-0 contract); this feature's test must use the JSON envelope contract (`{"type":"message","data":<Message>}`) per the channels-and-messages changelog entry.
- `pnpm demo` (mentioned in implementation step 2) does not exist yet. The test should NOT depend on it; instead, drive the equivalent commands directly. If `pnpm demo` lands later, a follow-up test can assert it works (small static check that `package.json` contains the script).
- AC-4's timing assertion needs `t.Helper()` discipline and a generous slack — the spec says "under 5 minutes" not "under exactly 5 minutes." Use `4*time.Minute` as the assertion budget to leave headroom for CI variability.

## Recommendations for /test-implement

1. This feature is the LAST one to test-implement in phase-3 because it depends on `embedded-web-build` and `seed-general-channel`. Order: `embedded-web-build` -> `seed-general-channel` -> this one.
2. Land all 4 ACs as skipped tests at the same time as the test files for the dependency features. Un-skip in dependency order.
3. AC-4 is borderline-untestable in a normal CI loop (5-minute wall-clock). Either gate behind a build tag or split into "boot time only" (the ~10s leg) and a manual checklist for the "clean clone" leg.
4. Don't recreate phase-1 coverage — register/login/post/broadcast are extensively tested in `apps/server/internal/http/*_test.go` and `ws_broadcast_test.go`. This test asserts the COMBINATION (single binary + embedded web + seed + auth + WS) works in one go, not the individual pieces.
5. Use bash only if Go shell-out becomes painful — both are valid per the spec, but Go gives better error messages, structured assertions, and integrates with the existing `tests/server-ws-hub/` patterns.
