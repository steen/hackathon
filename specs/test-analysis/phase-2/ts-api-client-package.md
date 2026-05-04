---
feature: ts-api-client-package
phase: phase-2
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 7
covered: 0
partial: 0
missing: 7
deferred: 0
---

# E2E test analysis: `packages/api-client` (TypeScript HTTP + WS + shared types)

**Spec:** `specs/plans/phase-2/30-feature-ts-api-client-package.md`
**Implementation status:** implemented — `packages/api-client/src/{client,http,ws,types,errors,index}.ts` exist with the full surface; `package.json` declares `vitest` as the test runner and points `main`/`types`/`exports` at `./src/index.ts` per the workspace's source-resolution rule.
**E2E test directory:** `tests/e2e/phase-2/ts-api-client-package/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | A TypeScript package at `packages/api-client` exports typed functions for the same server endpoints exposed by the Go client. | missing | — |
| AC-2 | Exports shared TS types for `User`, `Channel`, `Message`, `Event`. | missing | — |
| AC-3 | HTTP requests authenticate with `Authorization: Bearer <jwt>`; the `WebSocketClient` opens the WS connection using the one-shot ticket flow — call `wsTicket()` to mint a ticket, then connect to `?ticket=<hex>` (see `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`). The bearer token is not sent on the WS upgrade. | missing | — |
| AC-4 | Error-envelope decoding mirrors the server shape `{ok, data, error: {code, message}}` (see `apps/server/internal/http/errors.go`). | missing | — |
| AC-5 | Provides a `WebSocketClient` class with an event emitter API and reconnect support. | missing | — |
| AC-6 | Once `50-feature-presence.md` lands, the client must surface the `presence` event type (kind `join` / `leave`) through the same emitter; design the `Event` union with that in mind. | missing | — |
| AC-7 | Builds via `tsc` and is consumable by `apps/web` through pnpm workspace resolution. | missing | — |

## Findings

### Missing E2E tests

The in-package `client.test.ts` / `http.test.ts` / `ws.test.ts` files at this SHA exercise the client against mocked transports (`fetch` mocks, fake `WebSocket`); that's right for unit coverage but not E2E. E2E here means: a vitest suite that spawns a real `apps/server` Go binary as a child process and drives the TypeScript client against it over real HTTP and WebSocket.

The shape is a vitest globalSetup file (`tests/e2e/phase-2/ts-api-client-package/globalSetup.ts`) that:
1. Builds the Go server binary once (`child_process.execFileSync('go', ['build', '-o', tmpBin, './apps/server'], {cwd: repoRoot})`).
2. Picks a free port (open a `net.createServer().listen(0)` then close it and read `address().port`).
3. Spawns the binary with `CHAT_SERVER_PORT`, `CHAT_JWT_SECRET` (from `crypto.randomBytes(32).toString('hex')`), `CHAT_INVITE_CODE` (random hex), and `CHAT_DB_PATH=<tmpdir>/chat.db`.
4. Polls `GET /healthz` (or any route — `/debug/subs` works) until the server is listening, with a 10s timeout.
5. Exposes the URL + invite code via `globalThis.__SERVER_URL__ = ...` or via env vars (`process.env.E2E_SERVER_URL`) — env vars are the saner option because vitest workers don't share globals.
6. Returns a teardown function that sends SIGTERM to the child and awaits exit.

Per-AC sketches:

- **AC-1 — `tests/e2e/phase-2/ts-api-client-package/surface.test.ts`.** Construct `const c = createClient({ baseUrl: process.env.E2E_SERVER_URL!, getToken: () => token })`. Walk the surface: `c.register({ username:"alice", password:"...", inviteCode })` → store token → `c.me()` → `c.createChannel({ name:"#test" })` → `c.listChannels()` → `c.postMessage(channelId, "hi")` → `c.listMessages(channelId)` → `c.logout()`. Assert each method returns the typed shape. The assertion that this surface "matches the Go client" is best expressed as a `describe.each` loop that iterates a fixed array of method names declared once at the top of the file — drift with the Go client surface is then a one-line diff.

- **AC-2 — `tests/e2e/phase-2/ts-api-client-package/types.test.ts`.** This is a type-level AC; runtime E2E can't assert it. Cover with a tiny `expectType<>()`-style check via `tsd` or a `// @ts-expect-error` block: import `User`, `Channel`, `Message`, `Event` from `@hackathon/api-client` and write assignments that would fail typecheck if the exports were missing or shaped wrong. Pair with a runtime test that fetches a `User`/`Channel`/`Message` from the live server and runs a `zod` (or hand-rolled) shape check against the parsed JSON, so type-vs-runtime drift is caught.

- **AC-3 — `tests/e2e/phase-2/ts-api-client-package/auth_transport.test.ts`.** Two halves:
  1. **Bearer on REST:** wrap `globalThis.fetch` in a sniffer that records request headers, then call `c.me()` after `getToken` returns a real token from `c.login()`. Assert the captured request had `Authorization: Bearer <token>`. Without `getToken`, assert no `Authorization` header.
  2. **Ticket on WS, no bearer on WS:** subclass or monkey-patch the global `WebSocket` constructor in this test file (`vi.stubGlobal('WebSocket', SniffingWS)`) to record the upgrade URL and headers. Open the client's `WebSocketClient`, wait for `'open'` event, assert URL contains `?ticket=` (and the value matches `/[0-9a-f]{32,}/`) and that the upgrade did not include an `Authorization` header. Server is the real `apps/server`; the test is the only one substituting the global `WebSocket`.

- **AC-4 — `tests/e2e/phase-2/ts-api-client-package/envelope.test.ts`.** Trigger real server errors against the live server: bad-creds login (401), unknown-channel post (404), oversized body (413). For each, assert the rejected promise is an error instance whose serialized fields match `{ok:false, error:{code, message}}` per the server's `apps/server/internal/http/errors.go`. The instance shape depends on `packages/api-client/src/errors.ts` — read that first before pinning the assertion (likely something like `expect(err).toBeInstanceOf(ApiError); expect(err.code).toBe('...'); expect(err.status).toBe(401)`).

- **AC-5 — `tests/e2e/phase-2/ts-api-client-package/ws_emitter.test.ts`.** Open a `WebSocketClient`, `client.on('open', resolve)` then `client.on('message', collect)`. Post a message via REST. Assert the emitter fires with the typed event payload. **Reconnect half:** force-close the underlying socket via `client._socket?.close()` (or call `client.disconnect()` if exposed), then post another REST message and assert (within a backoff-shaped wait) `'open'` fires again and the new message arrives. The test should also assert `'close'` fired once between the two `'open'` events.

- **AC-6 — `tests/e2e/phase-2/ts-api-client-package/presence_event.test.ts`.** Skipped initially (presence feature is a separate spec; mark as `it.skip` or guard with `if (!process.env.E2E_PRESENCE)`). Once presence ships: open two `WebSocketClient`s (two distinct users); on the first, register a `'message'` (or `'event'`) handler that filters `type === 'presence'`. Assert a `kind:"join"` event fires when the second connects, and a `kind:"leave"` fires when the second disconnects. Type-shape check: `Event` union must include `PresenceEvent` so the handler argument is typed.

- **AC-7 — `tests/e2e/phase-2/ts-api-client-package/build_consumable.test.ts`.** Spawn `pnpm --filter @hackathon/api-client build` and assert exit 0. Then spawn `pnpm --filter web typecheck` (or a tiny scratch script that imports from `@hackathon/api-client`) and assert exit 0. This tests the workspace-resolution + tsc-build half together. Caveat: if vitest runs inside the same node process and CWD assumptions matter, prefer running these checks in a separate test that uses `child_process.execFileSync` from the repo root.

### Helpers and harness notes

- The harness lives in `tests/e2e/phase-2/ts-api-client-package/globalSetup.ts`. Wire it via `vitest.config.ts`'s `globalSetup` field. Place a vitest config alongside the tests if the package's own `vitest.config.ts` would conflict — the E2E tree should not depend on the package's unit test config.
- Add a small `helpers.ts` with `register(client, suffix)` that creates a unique user (`alice-${randomHex(4)}`) and returns its token — most tests need a fresh authenticated user and inlining the calls is noisy.
- For the WebSocket sniffer in AC-3, prefer `vi.stubGlobal('WebSocket', SniffingWebSocket)` scoped per-test; reset in `afterEach`. Don't mutate the real global without scoping.
- Server boot via `child_process.spawn` must `unref()` and the teardown must SIGTERM + wait for `'exit'` — otherwise vitest hangs at the end. Set a 5s timeout on the wait and SIGKILL after.

## Recommendations for /test-implement

1. **First:** write `globalSetup.ts` + a minimal smoke test that registers a user and calls `me()`. Get the harness green before adding more.
2. **Then:** AC-1 (surface walk) and AC-4 (envelope decode) — they reuse the harness directly.
3. **Then:** AC-3 (auth transport) — needs the WebSocket-stub plumbing.
4. **Then:** AC-5 (emitter + reconnect) — biggest test, needs careful timing.
5. **Then:** AC-2 (types) and AC-7 (build consumable) — small, independent.
6. **Defer:** AC-6 (presence event) until the `presence` feature lands; leave a skipped placeholder so the test count is honest.
