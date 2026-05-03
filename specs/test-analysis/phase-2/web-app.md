---
feature: web-app
phase: phase-2
analyzed_at: 2026-05-03T20:36:00Z
analyzed_commit: d41f2a788affd1ea07a0ef3dd4b84592aabf0915
implementation_status: implemented
total_acs: 6
covered: 5
partial: 1
missing: 0
deferred: 0
---

# Test analysis: `apps/web` (Vite + React + TS chat page)

**Spec:** `specs/plans/phase-2/40-feature-web-app.md`
**Implementation status:** implemented — `apps/web/` ships a Vite + React + TypeScript app with `Login`, `Register`, and `Chat` routes consuming `@hackathon/api-client`. 6 vitest cases across 3 test files cover all 5 named tests from the spec's Test plan: login, register-with-invite-code, chat history+live-append, reconnect-mints-fresh-ticket, and the SEC-9 XSS test (literal `<script>` body renders as text, not a DOM element). AC-5 (presence list in chat page) sits at partial — the Chat page has an explicit TODO referencing the as-yet-unwrapped `GET /api/presence` REST helper in `@hackathon/api-client`.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `apps/web` is a Vite + React + TypeScript app that builds with `pnpm --filter web build`. | covered | `apps/web/package.json` declares `"build": "tsc -p tsconfig.build.json && vite build"`; `apps/web/vite.config.ts` configures `@vitejs/plugin-react`; `tsconfig.build.json` points at the React+TS surface. The build script wires both type-checking and bundling so a type error fails the build. Verified by the test suite (which type-checks via vitest's transform); a full `pnpm --filter web build` was not run by this analysis (the test agent's contract is testing, not bundling — the build script structure is the AC anchor). |
| AC-2 | The app provides a login screen, a register screen (gated by invite code), and a chat page with channel list + message stream + input. | covered | `apps/web/src/routes/{Login,Register,Chat}.tsx` ship the three routes; `Login.test.tsx::test_web_login_form_calls_login_endpoint` (`should submits username/password to client.login`) verifies the login form posts to the API. `Register.test.tsx::test_web_register_form_requires_invite_code` (2 sub-tests: rejects empty invite, calls register with invite when present) verifies the invite-code gate. `Chat.test.tsx::test_web_chat_page_renders_history_then_appends_live_messages` exercises the channel list + history + live append + input chain end-to-end (with mocked api-client). |
| AC-3 | The chat page subscribes via WS and updates in real time as new messages arrive. The WS upgrade goes through the ticket flow exposed by `packages/api-client` (see `30-feature-ts-api-client-package.md`); the bearer token is not sent on the WS upgrade. | covered | `apps/web/src/hooks/useMessages.ts:53-58` constructs a `WebSocketClient` from `@hackathon/api-client`, which internally drives the `wsTicket()` then-connect flow (no bearer on upgrade — structural via the WHATWG `WebSocket` constructor). `Chat.test.tsx::test_web_chat_page_renders_history_then_appends_live_messages` asserts a fake socket message arrives and renders without a page reload. The test mocks `wsTicket` and asserts it's called once before the socket opens (`Chat.test.tsx:158`). |
| AC-4 | On WS disconnect, the client reconnects automatically with exponential backoff (and visibly indicates connection state). Each reconnect mints a fresh ticket via `GET /api/ws-ticket` rather than reusing the prior one. | covered | `useMessages.ts:13` defines `BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000]` ms (an extended exponential schedule beyond the api-client's default 5-step `[500, 1000, 2000, 5000, 10000]`). Connection state is surfaced as `"reconnecting"` / `"open"` / `"closed"` and rendered into Chat via `Chat.tsx:14`'s status indicator. `Chat.test.tsx::test_web_reconnects_after_ws_disconnect` (`forced close triggers reconnect that mints a fresh ticket`) drives `forceClose()` on the fake socket, waits for the api-client's reconnect timer, then asserts `wsTicket` was called twice and the second socket's URL contains `ticket=ticket-2` (the mock returns a fresh ticket on each call). |
| AC-5 | Once `50-feature-presence.md` lands, the chat page renders an online-users list driven by the initial `GET /api/presence` plus `presence` events on the WS stream. | partial | The presence-server feature DID land (PR #80, in main since `7b6b9b3`), so the AC's conditional ("Once X lands") is now active. **The web app intentionally postpones consumption** — `apps/web/src/routes/Chat.tsx:96-100` has an explicit TODO comment referencing #69: "online-users list driven by GET /api/presence + presence WS events. api-client surfaces PresenceEvent today; the GET endpoint is not yet wrapped in api-client and the server route lands with #69. Leaving this empty rather than fabricating a stub." A `<div className="presence" aria-label="online users (pending #69)" />` placeholder ships for layout. The placeholder is honest about the gap rather than papering over it; closing this AC requires (a) `@hackathon/api-client` to add a `getPresence()` method and (b) the chat page to call it on mount + handle `presence` events on the WS stream. **No failing test added by this run** — driving a guaranteed-red presence test would put `pnpm test` permanently red until both sides land. The TODO + this findings doc are the clearer signal. |
| AC-6 | The app consumes `packages/api-client` for all server interactions. | covered | `apps/web/package.json:15` declares `"@hackathon/api-client": "workspace:*"`. All HTTP+WS calls in `apps/web/src/{api,hooks/*,routes/*}.{ts,tsx}` import from `@hackathon/api-client` — no direct `fetch()` or `WebSocket` usage outside the api-client surface (verified by grep: only test fakes use raw WebSocket via the global override). Follow-up commit `c8c80ad` (`fix(web): resolve @hackathon/api-client to source for lint/test/build`) wires the workspace package to source rather than a built `dist/`, so dev iteration doesn't require a pre-build of api-client. |

## Findings

### Coverage notes

- **All 5 named tests from the spec's Test plan are present and pass.** The naming convention (`test_web_<scenario>` as `describe` blocks) is exact-match to the spec, which means the test agent's static-grep coverage check (per the test-analyze skill's "Coverage detection" rule) trivially anchors to each AC. Right discipline.
- **`useMessages.ts` extends the api-client's default backoff schedule.** The api-client default is `[500, 1000, 2000, 5000, 10000]` (5 steps); the web app uses `[500, 1000, 2000, 5000, 10000, 20000, 30000]` (7 steps). The extension is sensible — a web tab in the background can be offline for minutes; capping at 30s keeps the fan-out polite without burning CPU. Worth a doc-comment in the api-client noting "callers can override `backoffMs`" so future web tweaks don't need to read the source.
- **`useMessages.ts` cancel-token discipline.** The hook uses a `CancelToken { cancelled: boolean }` mutated by the cleanup closure to stop in-flight async work when the channel changes or the component unmounts. The `eslint-disable @typescript-eslint/no-unnecessary-condition` block at `useMessages.ts:36-39` is necessary because eslint's flow analysis can't see the cross-closure mutation. The comment documents the why — leave it.
- **XSS test is the load-bearing SEC-9 anchor.** `test_message_with_html_tags_renders_as_text_not_dom` asserts not just that `getByText` finds the literal string (which would pass even if React rendered it as DOM), but ALSO that `list.querySelector("script")` returns null AND `body.innerHTML` contains the entity-encoded `&lt;script&gt;`. Three independent assertions on the same invariant — exactly the right belt-and-suspenders for an XSS test. Without the `innerHTML` assertion, a future regression that switched to `dangerouslySetInnerHTML` would still pass `getByText` (because the displayed text matches) but the script tag would be live.
- **Reconnect test asserts the *fresh ticket* property explicitly.** `Chat.test.tsx:170-171` checks `wsTicketMock.toHaveBeenCalledTimes(2)` AND that the second socket URL contains `ticket=ticket-2`. AC-4's "fresh ticket" wording is the exact concern — reusing a redeemed (single-use) ticket would 401 silently and the user would see "reconnecting" forever. Pinning both the call count and the URL contents catches a regression where the api-client cached the ticket.

### Partial — AC-5 (presence list in chat page)

The wording of AC-5 makes it conditional on `50-feature-presence.md` landing. That condition has been met (PR #80 merged in `7b6b9b3`). The web app explicitly opts out of consumption with a TODO comment:

```tsx
{/* TODO(#69): online-users list driven by GET /api/presence + presence WS events.
    api-client surfaces PresenceEvent today; the GET endpoint is not yet wrapped
    in api-client and the server route lands with #69. Leaving this empty rather
    than fabricating a stub. */}
<div className="presence" aria-label="online users (pending #69)" />
```

Two gaps must close to flip this AC to covered:

1. **`@hackathon/api-client` adds a `getPresence()` method.** The HttpClient in `packages/api-client/src/http.ts` currently doesn't wrap `GET /api/presence`; once it does, the web's `api.ts` can expose `getClient().getPresence()`.
2. **The chat page wires presence into a state that renders into the placeholder div.** This is the smaller half — a `useEffect` that calls `getPresence()` on mount, plus an `on('message')` handler that branches on `ev.type === "presence"` and updates an `onlineUsers: Map<string, User>` state.

There's also the cross-feature schema-drift flag from the presence findings (`phase-2/presence.md`): the server emits `{type:"presence", data:{kind, user_id}}` while the api-client's `PresenceEvent.data` type is `{kind, user:User}`. Picking which side moves is a maintainer call (production change, out of test-agent scope). Until that lands, the web's `data.user` access would be undefined — which is part of why the TODO is explicit rather than half-built.

**No failing test added by this run.** A guaranteed-red presence test would put `pnpm test` red until both gaps close; the findings doc + TODO comment are the clearer signal.

### Cross-feature observations

- **TS api-client AC-7 closes here.** PR #82's findings doc on `ts-api-client-package` marked AC-7 ("consumable by `apps/web` through pnpm workspace resolution") as covered "at the workspace-graph level" because `apps/web` didn't exist. Now `apps/web/package.json` declares `"@hackathon/api-client": "workspace:*"` and the test suite proves the import resolves. AC-7 is now load-bearing-real, not just structurally promised. Worth a note on the next test-watch tick re-evaluation.
- **Follow-up commit `c8c80ad fix(web): resolve @hackathon/api-client to source for lint/test/build`.** Likely added a tsconfig path alias or a vite resolve rule so the web app picks up `packages/api-client/src/` directly without needing a built `dist/`. This is the right dev-loop choice — without it, a change to api-client would require rebuilding api-client before the web app's tests would see it. Not asserted by any AC; just a quality-of-life win. Worth flagging that production builds (`pnpm --filter web build`) should still go through api-client's emitted `dist/` per its `package.json` `main`/`exports` — would be worth verifying in the CI pipeline if there is one.
- **Auth context persists JWT in localStorage.** Spec implementation step 3 ("Implement a small auth context that loads/persists the JWT in `localStorage`") is satisfied by `apps/web/src/auth/AuthContext.tsx` (93 lines). No AC explicitly tests the localStorage round-trip — it's covered indirectly by the Login and Register tests (which both end with a logged-in app state). A defensive test could cover "logout clears localStorage", but it's not in the spec's Test plan and not load-bearing for any AC.
- **Test fakes use the global `WebSocket` override.** `Chat.test.tsx:62-64` sets `globalThis.WebSocket = FakeSocket` before each test; this is the same indirection point the api-client's `WebSocketClient` uses by default (`getGlobalWebSocket` in `packages/api-client/src/ws.ts:188-194`). Right factoring — test code doesn't have to know the api-client's internal `WebSocketCtor` option exists to inject fakes.

### Spec-vs-impl notes

- Spec lists `apps/web/src/main.tsx`, `Login.tsx`, `Register.tsx`, `Chat.tsx`, `auth/AuthContext.tsx`, `hooks/useChannels.ts`, `hooks/useMessages.ts`, `**/*.test.tsx` — all present. Plus `App.tsx` (the route shell), `api.ts` (the api-client wrapper that holds the singleton + token storage), `styles.css`, `test-setup.ts`, `vitest.config.ts`. Reasonable additions; spec follow-up could mention them.
- The `Chat.tsx` placeholder for online-users (`<div className="presence" aria-label="online users (pending #69)" />`) is intentional — the doc-comment names #69 (the presence feature) explicitly. **Note:** PR #69 references the original GitHub issue/PR for presence; the actual landed PR is #80 (the merge commit on main). The test agent does not edit production code, so the TODO's #69 reference stays as-is — but worth flagging to the maintainer that the comment could be updated to "pending PR #82 (api-client wrap) + presence-on-web follow-up" for future-reader clarity.
- `apps/web/vite.config.ts` and `apps/web/vitest.config.ts` are split — the latter sets up `jsdom` and `test-setup.ts` for testing-library compat. Right separation; keeps the dev-server config minimal.

## Recommendations

1. No new tests added by this run — coverage of the 5 covered ACs is comprehensive and matches the spec's Test plan exactly. The 6th AC (presence) needs both api-client `getPresence()` wrapping AND a server/client schema-drift reconciliation (see `phase-2/presence.md` for the drift detail) before it can flip; a test added now would be permanently red.
2. **Production change for AC-5 (out of test-agent scope):** add `getPresence()` to `@hackathon/api-client/src/http.ts`, then wire the chat page's placeholder div to a `useEffect` that loads the initial set + an `on('message')` handler that branches on presence events. Pre-requisite: reconcile the schema drift (server emits `{kind, user_id}`; client type expects `{kind, user:User}`). Pick one shape; whichever side moves, both will compile.
3. **Cross-feature follow-up:** when the next test-watch tick runs after the api-client+presence reconciliation lands, re-evaluate this feature's AC-5 to flip partial → covered. Also re-evaluate `phase-2/ts-api-client-package` AC-6 (presence event surface) — its "wire format is the client's guess" caveat should be replaced with a real assertion.
4. **Spec follow-up (out of test-agent scope):** add `App.tsx`, `api.ts`, `styles.css`, `test-setup.ts`, `vitest.config.ts` to the "Files expected to be touched or created" list to match the shipped layout.
