---
feature: ts-api-client-package
phase: phase-2
analyzed_at: 2026-05-03T19:55:35Z
analyzed_commit: ff5576d7892382c8a680185251d43e8f9c8554b4
implementation_status: implemented
total_acs: 7
covered: 6
partial: 1
missing: 0
deferred: 0
---

# Test analysis: `packages/api-client` (TypeScript HTTP + WS + shared types)

**Spec:** `specs/plans/phase-2/30-feature-ts-api-client-package.md`
**Implementation status:** implemented — `packages/api-client/src/{client,http,ws,types,errors,index}.ts` ship the full surface (typed REST methods + `WebSocketClient` with reconnect + `watch` async-iterable). Package is published as `@hackathon/api-client` in the pnpm workspace, with `vitest` covering 25 test cases across `client.test.ts` (3), `http.test.ts` (11), and `ws.test.ts` (11) — verified via `pnpm -r --if-present test` at this SHA. AC-7 (presence-event surface) sits at partial pending `50-feature-presence` impl: the type union is in place but the wire format is unverified against a real presence frame.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | A TypeScript package at `packages/api-client` exports typed functions for the same server endpoints exposed by the Go client. | covered | `index.ts` re-exports `Client`/`createClient` (full surface) plus `HttpClient` (low-level). Methods present: `login`, `register`, `me`, `logout`, `wsTicket`, `listChannels`, `createChannel`, `listMessages`, `postMessage`, `watch` — same set as the Go client, verified by `client.test.ts` + `http.test.ts`. The 1:1 surface mapping is enforced by both clients consuming the same envelope shape from `apps/server/internal/http/errors.go`. |
| AC-2 | Exports shared TS types for `User`, `Channel`, `Message`, `Event`. | covered | `src/types.ts` defines all four (lines 1, 6, 12, 55). `Event` is a discriminated union (`MessageEvent | PresenceEvent | UnknownEvent`) so callers `switch (ev.type)` to narrow. Re-exported from `index.ts`. The `vitest` build proves the types compile; type-only assertions live in the test file imports. |
| AC-3 | HTTP requests authenticate with `Authorization: Bearer <jwt>`; the `WebSocketClient` opens the WS connection using the one-shot ticket flow — call `wsTicket()` to mint a ticket, then connect to `?ticket=<hex>` (see `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`). The bearer token is not sent on the WS upgrade. | covered | `http.test.ts::HttpClient > listChannels unwraps {channels:[...]} and includes bearer` (line 163) verifies the `Authorization: Bearer <token>` header is present on outgoing REST. WS half: `ws.test.ts::WebSocketClient > mints a ticket then connects with ?ticket=<hex>&channel=<id>` (line 80) drives a stub `wsTicket()` call followed by the WS connect, asserting the URL has `?ticket=...` and no `Authorization` header (the latter is structural — `ws.ts:117` invokes `new Ctor(url)` with no header argument because the WHATWG WebSocket constructor doesn't accept one). |
| AC-4 | Error-envelope decoding mirrors the server shape `{ok, data, error: {code, message}}` (see `apps/server/internal/http/errors.go`). | covered | `http.test.ts::decodes error envelope into ApiError carrying code+status` (line 237) drives a `{ok:false, error:{code:"unauthorized", message:"..."}}` response and asserts `ApiError.code === "unauthorized"` + `ApiError.status === 401`. `ApiError` is defined in `errors.ts:1-12` with `code`/`status`/`message` fields. `isApiErrorCode` helper (line 13) gives a typed discriminator for callers; tested at line 256. |
| AC-5 | Provides a `WebSocketClient` class with an event emitter API and reconnect support. | covered | `ws.ts:44` exports `WebSocketClient` with `on(event, fn)` / `off(event, fn)` for `"open" | "close" | "message" | "error"`, plus `send(data)`, `connect()`, `close()`. Reconnect support is in `scheduleReconnect` (`ws.ts:139-153`) using `DEFAULT_BACKOFF = [500, 1000, 2000, 5000, 10000]` ms. Tests: `ws.test.ts::reconnects after a forced close (mints a fresh ticket)` (line 124) + `does not reconnect when caller closed` (line 158) + `send throws before the socket is open` (line 178). |
| AC-6 | Once `50-feature-presence.md` lands, the client must surface the `presence` event type (kind `join` / `leave`) through the same emitter; design the `Event` union with that in mind. | partial | `types.ts:40-50` defines `PresenceEvent` (`type:"presence", data:{kind:"join"|"leave", user:User}`) and includes it in the `Event` union. `ws.test.ts::decodeFrame > preserves presence frames as PresenceEvent shape` (line 71) drives a stubbed presence frame through `decodeFrame` and asserts the type is preserved. **Caveat:** the wire format is the client's *guess* at what `50-feature-presence` will emit — no server-side presence frames exist at this SHA, so the contract is unverified end-to-end. When the presence feature lands, the test agent should re-evaluate this AC against the real server envelope. The forward-compatibility intent of AC-6 ("design the `Event` union with that in mind") is met; the runtime conformance check waits on impl. |
| AC-7 | Builds via `tsc` and is consumable by `apps/web` through pnpm workspace resolution. | covered | `package.json` declares `"build": "tsc -p tsconfig.build.json"` and the package is published as `@hackathon/api-client`. The pnpm workspace at `pnpm-workspace.yaml` includes `packages/*`, so workspace-resolution of `@hackathon/api-client` is structural. **Note:** `apps/web/` does not exist yet (only `apps/cli` and `apps/server`); the consumability claim is at the workspace-graph level, not at a real consumer site. The `vitest` test pass at this SHA proves the package compiles. When `40-feature-web-app` lands, the test agent should re-evaluate to confirm the actual import works. |

## Findings

### Coverage notes

- **`HttpClient` is the low-level surface, `Client` is the convenience layer.** `client.ts:21` defines `Client` which holds an in-memory token (`memToken`) and threads `getToken`/`setToken` callbacks into `HttpClient`. This split is the right factoring: callers who want raw control (e.g. their own token storage backend) use `HttpClient` directly; the typical web caller uses `createClient({baseUrl})` and gets memory-backed token storage for free. The `setToken` hook lets a caller persist tokens to localStorage without forking the package.
- **`WebSocketClient` ↔ `watch` async-iterable factoring.** `ws.ts:196-254` exposes `watch(http, channelId, opts)` as an `AsyncGenerator<WsEvent>`, internally driving a `WebSocketClient` with `reconnect:false`. This is the right shape for `for await (const ev of client.watch(channelId)) { ... }`-style consumption, and reconnect-during-iteration is intentionally disabled (a reconnect mid-iteration would silently drop the iterator's queue invariants). Tested by `ws.test.ts::watch async iterable > yields message events and stops on close` (line 194).
- **Reconnect schedule is exposed.** `WebSocketClientOptions.backoffMs?: number[]` (`ws.ts:35`) lets callers override the default schedule. Default is `[500, 1000, 2000, 5000, 10000]` ms; index clamps at length-1 once exhausted. Sensible exponential-ish ramp; the cap at 10s prevents one disconnected client from going silent for minutes.
- **`decodeFrame` mirrors the Go-client `decodeEvent`.** Both unwrap the typed `{type, data}` envelope; the TS version uses a discriminated union, the Go version uses a tagged struct. The shapes are equivalent but the languages favor different idioms — fine. Worth flagging that any future *server-side* schema change must update both decoders in lockstep, which is what the `Risks` section of both specs already calls out.
- **Determinism in tests.** `ws.test.ts` uses `setTimeout`/`clearTimeout` injection (`WebSocketClientOptions.setTimeout`) so reconnect tests can advance time without sleeping. Right design — flaky reconnect tests would be a maintenance tax for years.

### Cross-feature observations

- **Future `apps/web` consumer.** The TS client's typed surface is the contract `40-feature-web-app` will lean on. The `Client` class is a convenience wrapper around `HttpClient` + `WebSocketClient`; the web app is expected to call `createClient({baseUrl, getToken: () => localStorage.getItem("token")})` and use `client.watch(channelId)` for the message stream. The reconnect behavior is web-app-friendly (handles tab-sleep / network-drop) without the caller writing reconnect logic.
- **`prettier` formatting commit (f85f955).** Post-merge follow-up applied prettier to the package; doesn't change AC coverage. Notable because it's the kind of "lint-clean PR after the feature lands" pattern CLAUDE.md's `Linter is PR #0` rule warns about — better to land it pre-feature so the original PR is already clean. Not test-agent's call to flag; just noting the pattern.
- **MSW vs. fetch-mock choice.** Spec mentions `vitest` and `msw` (or `undici`'s mock agent). Implementation uses neither — tests inject a `fetch` stub directly via `HttpClient`'s `fetch` option (`http.ts:14-19`). Functionally equivalent; lighter weight (no msw service-worker shim), and pinning the contract at the `fetch` boundary is the right level for a unit test of an HTTP client.

### Spec-vs-impl notes

- Spec lists `package.json`, `tsconfig.json`, `src/index.ts`, `src/types.ts`, `src/http.ts`, `src/ws.ts`, `src/*.test.ts` — all present. The impl adds `src/client.ts` (the convenience wrapper), `src/errors.ts`, and `tsconfig.build.json` (separate emit config from the dev tsconfig). The split is sound; spec follow-up could mention these as expected.
- Spec implementation step 6 says "Configure build to emit ESM + types." `package.json` declares `"type": "module"` and `"main": "./dist/index.js"` + `"types": "./dist/index.d.ts"` — ESM-only, types emitted. Confirmed.
- The `Event` union (`types.ts:55`) uses `MessageEvent | PresenceEvent | UnknownEvent`. `UnknownEvent` is a useful escape hatch for forward-compat: any future event type the client doesn't know about lands as `{type:"<new>", data:unknown}` rather than throwing. Good design call.
- `WebSocketClient.send(data)` throws `"websocket not open"` synchronously when `readyState !== 1`. Correct: silently dropping pre-open sends would surprise callers. Tested at `ws.test.ts:178`.

## Recommendations

1. No new tests added by this run — coverage is comprehensive across the 6 covered ACs at the unit + integration boundary.
2. **AC-6 stays partial** until `50-feature-presence` lands and the test agent can verify the wire format against a real server-emitted presence frame. The client is forward-compatible by design; the verification gap is the cross-feature dependency.
3. **AC-7 cross-feature follow-up:** when `40-feature-web-app` lands and `apps/web/package.json` declares `"@hackathon/api-client": "workspace:*"`, re-evaluate to confirm the workspace-resolution claim holds at a real consumer site.
4. **Spec follow-up (out of test-agent scope):** add `src/client.ts`, `src/errors.ts`, and `tsconfig.build.json` to the "Files expected to be touched or created" list so the spec matches the shipped layout.
