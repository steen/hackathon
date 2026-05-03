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

Implementation order is encoded in the filename prefix (10, 20, 30…). Insert new features by picking an unused number between neighbours (e.g. `15-`).

- [10 — Go client package](phase-2/10-feature-go-client-package.md)
- [20 — CLI full command set](phase-2/20-feature-cli-full-commands.md)
- [30 — TS api-client package](phase-2/30-feature-ts-api-client-package.md)
- [40 — Web app](phase-2/40-feature-web-app.md)
- [50 — Presence (online users)](phase-2/50-feature-presence.md)
