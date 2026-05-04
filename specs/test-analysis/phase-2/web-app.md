---
feature: web-app
phase: phase-2
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 6
covered: 0
partial: 0
missing: 6
deferred: 0
---

# E2E test analysis: `apps/web` (Vite + React + TS chat page)

**Spec:** `specs/plans/phase-2/40-feature-web-app.md`
**Implementation status:** implemented — `apps/web/package.json` declares the React + Vite + vitest setup; `src/{App.tsx, main.tsx, api.ts, auth/, hooks/, routes/, styles.css}` implement the login/register/chat surface; `@hackathon/api-client` is a workspace dep.
**E2E test directory:** `tests/e2e/phase-2/web-app/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `apps/web` is a Vite + React + TypeScript app that builds with `pnpm --filter web build`. | missing | — |
| AC-2 | The app provides a login screen, a register screen (gated by invite code), and a chat page with channel list + message stream + input. | missing | — |
| AC-3 | The chat page subscribes via WS and updates in real time as new messages arrive. The WS upgrade goes through the ticket flow exposed by `packages/api-client` (see `30-feature-ts-api-client-package.md`); the bearer token is not sent on the WS upgrade. | missing | — |
| AC-4 | On WS disconnect, the client reconnects automatically with exponential backoff (and visibly indicates connection state). Each reconnect mints a fresh ticket via `GET /api/ws-ticket` rather than reusing the prior one. | missing | — |
| AC-5 | Once `50-feature-presence.md` lands, the chat page renders an online-users list driven by the initial `GET /api/presence` plus `presence` events on the WS stream. | missing | — |
| AC-6 | The app consumes `packages/api-client` for all server interactions. | missing | — |

## Findings

### Test-shape choice: Playwright over msw-mocked

The spec test plan names tests like `test_web_chat_page_renders_history_then_appends_live_messages` and `test_web_reconnects_after_ws_disconnect` and `test_message_with_html_tags_renders_as_text_not_dom`. Two viable shapes:

1. **vitest browser mode (Playwright runner)** — drives a real browser at the Vite-built artifact, hits a real `apps/server`. Pros: exercises the whole stack including reconnect + DOM-render-as-text. Cons: heavier setup (Playwright install in CI).
2. **vitest jsdom + msw-mocked** — renders `<App/>` against `jsdom`, mocks `fetch`/`WebSocket` via msw. Pros: fast, no browser dependency. Cons: msw doesn't mock `WebSocket` natively (would need a hand-rolled fake), and reconnect/backoff timing is hard to assert without real timers; the SEC-9 XSS check (`<script>` rendered as text) is technically possible in jsdom but a real-browser run is more credible.

**Pick Playwright** for this E2E suite. Reasons: AC-3 (real WS), AC-4 (reconnect with real backoff), and the SEC-9 XSS test are all real-browser work. Mocking the WS layer for AC-3/AC-4 would test the mock, not the app.

Setup shape:
1. `tests/e2e/phase-2/web-app/playwright.config.ts` declares one project (`chromium`), a `webServer` that runs `pnpm --filter web preview` against the built artifact (or `pnpm --filter web dev` for faster iteration), and a `globalSetup.ts` that boots `apps/server` as a child process (same pattern as the ts-api-client globalSetup) and writes its URL into an env var the Vite server reads at build time (`VITE_SERVER_URL`).
2. The Vite app must be built before Playwright runs (`pnpm --filter web build`) so the preview server has artifacts. Note this in the test setup README.
3. Each test creates its own user via direct REST against `apps/server` (using `playwright`'s `request` fixture) so the UI tests don't depend on shared state.

Per-AC sketches:

- **AC-1 — `tests/e2e/phase-2/web-app/build.test.ts`** (vitest, not Playwright). Run `pnpm --filter web build` via `child_process.execFileSync` and assert exit 0 + that `apps/web/dist/index.html` exists. Smallest possible E2E for the build claim. Vitest can host this even though the rest of the suite is Playwright.

- **AC-2 — `tests/e2e/phase-2/web-app/auth_screens.spec.ts`** (Playwright). Three sub-tests:
  1. Navigate to `/login`, fill username + password, submit → expect navigation to `/` (chat page) with a recognizable element (`getByRole('heading', { name: /chat/i })` or whatever the impl uses).
  2. Navigate to `/register`, fill invite code + username + password, submit → expect navigation to `/`. Also assert the register form has an invite-code input field present (selector check).
  3. Navigate to `/`, expect the chat page renders channel list, message area, and an input box (three explicit selectors).

- **AC-3 — `tests/e2e/phase-2/web-app/realtime.spec.ts`** (Playwright). Login as alice. Navigate to `/`. Wait for the chat page to mount and select `#test` channel (create it via REST first if not present). In a parallel context (Playwright's `request` fixture), POST a message to `#test`. Assert the message appears in the chat DOM within 2s. **No-bearer-on-WS half:** use Playwright's `page.on('websocket')` to capture the WS upgrade; assert the URL contains `?ticket=` and the request headers do not contain `Authorization`. (Headers on the `websocket` event are limited in Playwright; a backstop is to attach an HTTP-request interceptor to `/api/ws-ticket` that asserts the bearer is on that REST call but not on the WS upgrade.)

- **AC-4 — `tests/e2e/phase-2/web-app/reconnect.spec.ts`** (Playwright). Boot `apps/server` behind a small TCP proxy that the test can `Close()`. Login + open chat. Wait for WS open. Close the proxy → assert a "disconnected" indicator appears in the DOM within 1s. Reopen the proxy. Assert the indicator clears within ~10s (backoff window). To verify a fresh ticket is minted: count `GET /api/ws-ticket` calls server-side (via the audit log or a custom server-side counter middleware) and assert ≥ 2 over the test duration. To check the backoff is exponential, the test could close the proxy three times rapidly and assert the inter-attempt delays grow — but that's tricky to time deterministically; document as a soft check.

- **AC-5 — `tests/e2e/phase-2/web-app/presence.spec.ts`** (Playwright). Skipped initially (depends on `50-feature-presence.md`). Once presence ships: open two browser contexts (alice, bob). Wait for both chat pages to load. In alice's context, assert the online-users list contains both `alice` and `bob`. Close bob's context. Assert bob disappears from alice's list within 2s.

- **AC-6 — `tests/e2e/phase-2/web-app/api_client_consumption.test.ts`** (vitest). Two halves:
  1. Workspace-dep check: `pnpm --filter web list --depth=0 --json` includes `@hackathon/api-client` with `link:` or `workspace:` resolution.
  2. Source-grep check: every `fetch(` call site in `apps/web/src/` is inside an api-client wrapper or comes via `@hackathon/api-client`. A lint-rule-as-test using `eslint-plugin-no-restricted-imports` (or a hand-rolled grep that fails if it finds `fetch(` outside `apps/web/src/api.ts`) covers this. The "all server interactions" claim is strong — read `apps/web/src/api.ts` to see whether the app re-wraps the client or calls it directly.

  **Bonus SEC-9 test (per spec's `test_message_with_html_tags_renders_as_text_not_dom`)**: not in the AC list above but the spec test plan includes it. Add `tests/e2e/phase-2/web-app/xss_safe_render.spec.ts`: post `<script>window.__pwn=true</script>` via REST; load chat; wait for the message in the DOM; assert the chat DOM `textContent` contains the literal string `<script>` and that `page.evaluate(() => (window as any).__pwn)` returns `undefined`.

### Helpers and harness notes

- Reuse the same Go-server boot helper from the ts-api-client globalSetup (factor it into `tests/e2e/internal/serverharness.ts` so both vitest globalSetups and Playwright globalSetups can import it).
- Build-artifact dependency: `pnpm --filter web build` must run before the Playwright suite. Encode this in `playwright.config.ts`'s `globalSetup` (run the build there if `apps/web/dist` is missing or older than `apps/web/src/`).
- For reconnect testing (AC-4), write a tiny TS TCP proxy in the harness (`createProxy({ target, listenPort })` returning `{ close, restart }`). Cleaner than restarting the Go server.
- Keep tests deterministic: every test creates a fresh user via REST. Don't rely on a `beforeAll` shared fixture that other tests mutate.
- For SEC-9, prefer `expect(page.locator('.message-body').first()).toHaveText('<script>...</script>')` over checking innerHTML — `toHaveText` reads textContent which is the right contract.

## Recommendations for /test-implement

1. **First:** the harness — `tests/e2e/internal/serverharness.ts` (TS), Playwright config, globalSetup that boots Go + builds web. Smoke test: a page that loads `/` and asserts the title.
2. **Then:** AC-1 (build) — trivial; validates the harness's build step works.
3. **Then:** AC-2 (auth screens) and AC-6 (api-client consumption) — straightforward DOM and lint checks.
4. **Then:** AC-3 (realtime + WS-no-bearer) — the load-bearing test for the chat experience.
5. **Then:** the SEC-9 XSS-safe-render test — small, high-value, pairs with AC-3.
6. **Then:** AC-4 (reconnect + fresh ticket) — needs the TCP proxy helper.
7. **Defer:** AC-5 (presence) until the presence feature lands; placeholder skipped test.
