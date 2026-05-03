# Feature: `packages/api-client` (TypeScript HTTP + WS + shared types)

**Parent phase:** [Phase 2: Web UI + shared clients](../phase-2-web-ui-shared-clients.md)
**Status:** planned

## Requirements covered
- (foundation for US-9; the web app in `40-feature-web-app.md` consumes this package)

## Acceptance criteria
- A TypeScript package at `packages/api-client` exports typed functions for the same server endpoints exposed by the Go client.
- Exports shared TS types for `User`, `Channel`, `Message`, `Event`.
- Provides a `WebSocketClient` class with an event emitter API and reconnect support.
- Builds via `tsc` and is consumable by `apps/web` through pnpm workspace resolution.

## Implementation steps
1. Create `packages/api-client/package.json`, `tsconfig.json`, and an `src/` tree.
2. Define types in `src/types.ts` mirroring server JSON shapes.
3. Implement `src/http.ts` with `fetch`-based methods and error-envelope decoding.
4. Implement `src/ws.ts` with `WebSocketClient` exposing `on('message', ...)`, `on('open', ...)`, `on('close', ...)`, and a `send` method.
5. Provide a `createClient({ baseUrl, getToken })` factory that returns the HTTP + WS surface.
6. Configure build to emit ESM + types.

## Test plan
- Unit tests using `vitest` and `msw` (or `undici`'s mock agent) for each HTTP method.
- Unit test: `WebSocketClient` reconnects after a forced close.

## Files expected to be touched or created
- `packages/api-client/package.json`
- `packages/api-client/tsconfig.json`
- `packages/api-client/src/index.ts`
- `packages/api-client/src/types.ts`
- `packages/api-client/src/http.ts`
- `packages/api-client/src/ws.ts`
- `packages/api-client/src/*.test.ts`

## Risks
- Type drift from the Go server; mitigated by treating the server JSON shape as the contract and reviewing both clients on schema changes.
