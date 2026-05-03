# Phase 0: Walking skeleton, system test ready

**Status:** planned
**Time estimate:** ~1 hr
**PRD revision:** 7e33be3

## Goal
Server up, two CLI clients exchanging real-time messages over WebSocket. No auth, no DB, hardcoded `#general`. Prove the wire end-to-end.

## Dependencies
None

## Deliverables
- [ ] Monorepo scaffold: `go.work`, `pnpm-workspace.yaml`, root `package.json` with `dev` / `build` / `test` scripts.
- [ ] `apps/server`: `/ws` endpoint with in-memory hub, broadcasts every received message to all subscribers of the channel.
- [ ] `apps/cli`: `chatd send` and `chatd watch` against `/ws` (no login).
- [ ] **System test**: `scripts/smoke.sh` boots server, runs two `chatd watch` processes, pipes a message via `chatd send`, asserts both watchers see it.

## Validation criteria
- `scripts/smoke.sh` passes. This stays green for the rest of the project.

## Features
- [Monorepo scaffold](phase-0/feature-monorepo-scaffold.md)
- [Server WS endpoint with in-memory hub](phase-0/feature-server-ws-endpoint.md)
- [CLI send and watch](phase-0/feature-cli-send-watch.md)
- [Smoke system test](phase-0/feature-smoke-test.md)
