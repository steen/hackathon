# Phase 2: Web UI + shared clients

**Status:** planned
**Time estimate:** ~3.5 hrs
**PRD revision:** 7e33be3

## Goal
React web client and full CLI against the same API.

## Dependencies
Phase 1

## Deliverables
- [ ] `packages/go-client`: HTTP + WS client used by CLI.
- [ ] `apps/cli`: full command set (channels, history, login, watch, send).
- [ ] `packages/api-client` (TS): HTTP + WS client + shared types.
- [ ] `apps/web`: Vite + React + TS chat page; login/register; reconnect-on-disconnect with exponential backoff.
- [ ] Tests for US-7 (presence), US-8 (CLI round-trip).

## Validation criteria
- manual cross-client demo (CLI ↔ Web message round-trip).

## Features

Note: `feature-presence.md` is the only Phase-2 feature that modifies server internals (`apps/server/internal/hub/hub.go`); the other four features are client-side only.

Test ownership: US-7 covered by `feature-presence.md`; US-8 covered by `feature-cli-full-commands.md`.

- [Go client package](phase-2/feature-go-client-package.md)
- [CLI full command set](phase-2/feature-cli-full-commands.md)
- [TS api-client package](phase-2/feature-ts-api-client-package.md)
- [Web app](phase-2/feature-web-app.md)
- [Presence (online users)](phase-2/feature-presence.md)
