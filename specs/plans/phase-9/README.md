# Phase 9 — Direct messages + cross-surface read state

**Parent epic:** #860 — _Phase 9: Direct messages + cross-surface read state_
**Decision log:** `lt -p direct-messages 3` (numbered §1–§12, locked-in defaults L1–L28, constraints C1–C2)
**Contract sub-issue:** #861 — this directory + the PRD addendum

## Overview

Phase 9 adds three orthogonal surfaces in one phase:

1. **Direct messages, 1:1 only** — six new HTTP endpoints, two new tables (`dm_conversations`, `dm_messages`), one new WS frame kind (`{type:"dm"}`).
2. **Server-tracked read state** — `channel_reads` + `dm_reads` tables, two new HTTP endpoints (`POST /api/channels/{id}/read`, `POST /api/dms/{id}/read`), additive `unread_count` on listings, one new WS frame kind (`{type:"read"}`).
3. **Multi-topic WS subscription** — every connection now subscribes to BOTH a channel topic AND its `user:<viewer>` inbox topic so DM/read frames reach the right viewer without breaking the existing channel-scoped WS contract.

The PRD addendum (§4, §10, §11, §13) and this directory together are the full contract — every Phase 9 sub-issue cites either a PRD section or a `specs/plans/phase-9/*.md` file (or a numbered decision-log entry) for any cross-feature shape.

## Cross-links

| Topic | File |
|---|---|
| DM wire types + endpoints | [dms.md](dms.md) |
| Read-state envelopes + asymmetric init | [read-state.md](read-state.md) |
| Multi-topic WS subscription | [ws-routing.md](ws-routing.md) |
| Decisions / locked-in defaults | `lt -p direct-messages 3` (numbered) |

## Wave table (planned)

The Phase 9 epic (#860) has 14 sub-issues. The contract (this PR) lands first; subsequent waves follow the dependency graph in the decision log.

| Wave | Surface | Sub-issue title pattern | Notes |
|---|---|---|---|
| 0 (this PR) | Contract | Phase 9 — Contract: PRD addendum + `specs/plans/phase-9/*.md` | All other waves cite this. |
| 1 (G0) | Server prep | Phase 9 — Rate limit: `dm-write` + `read-mark` buckets | Pre-requisite for G2 + H to avoid `ratelimit/config.go` race (L17). |
| 1 (B) | Migration | Phase 9 — Migration: `0005_dms_and_read_state.sql` | Single migration covers ALL Phase 9 schema (L23). |
| 2 (R) | Repo | Phase 9 — Repo: `dm_conversations`, `dm_messages`, `*_reads` | Includes `InsertMessageTx`, `InsertDMMessageTx`, `MaterializeChannelReadsTx` (L21). |
| 3 (H) | HTTP — DMs | Phase 9 — HTTP: `POST /api/dms`, `GET /api/dms`, message endpoints | Idempotent create per L18; `MaxMessageBodyBytes = 4096` shared cap. |
| 3 (R) | HTTP — read | Phase 9 — HTTP: `POST /api/channels/{id}/read`, `POST /api/dms/{id}/read` | Advance-only semantics per L5. |
| 3 (W) | WS routing | Phase 9 — WS: multi-topic subscription, `{type:"dm"}`, `{type:"read"}` frames | Implements §10 / L15. |
| 4 (T) | Wire types | Phase 9 — Wire types: TS + Go mirrors for DM/read envelopes | Coordinated multi-package update (L14, L26). |
| 4 (E) | E2E | Phase 9 — E2E: DM + read-state suites under `tests/e2e/phase-9/` | Imports `hackathon/tests/e2e/internal/testsupport` (L27). |
| 5 (C) | CLI | Phase 9 — CLI: `chatd dm list/send/history/read/watch` + `chatd channels read` | Locked-in shape per L25. |
| 5 (W) | Web — discovery | Phase 9 — Web: "+ Direct message" Modal + create flow | Reuses Phase 8 #836 Modal (L24). |
| 5 (W) | Web — sidebar | Phase 9 — Web: DM sidebar section + unread badges | Two sidebar sections (L7); debounced `POST /read` (L22). |
| 5 (W) | Web — viewer | Phase 9 — Web: DM message viewer + history | Mirrors channel viewer; immutable per L9. |
| 6 | Diary | Phase 9 — Diary: handoff entry + `CHANGELOG.d/` fragment | Per-PR fragment per repo convention. |

Wave numbers match the decision-log letters (B/R/H/W/T/E/C); the table is for orientation, not enforcement — sub-issues track their own dependencies.

## Phase 8 surface verification (per AC8)

The decision log C2 requires Phase 9 sub-issues to verify the Phase 8 surface at HEAD before opening, since Phase 9 mirrors Phase 8 patterns (channel-rename + WS broadcast + Modal primitive). The verification below was performed at branch-cut on commit `0742b0b`. Findings:

| Phase 8 PR | Surface | File at HEAD | Delta vs. Phase 8 PR description |
|---|---|---|---|
| #842 (#834 sub) | `useChatSocket` lift | `apps/web/src/hooks/useChatSocket.ts` | **No delta.** Hook exposes the documented `subscribe(event, fn)` API for `open`/`close`/`error`/`message`; backoff ladder `BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000]`; collapses to `idle` when `channelId === null`; Phase 9 DM/read consumers attach via the same `subscribe` API. |
| #837 (#834 sub) | Per-user channel write rate-limit + `{type:"channel"}` frame | `apps/server/internal/http/channels_handlers.go` (lines 163–181 `broadcastChannelEvent`); `Routes` wires `wrapWrite` over POST/PATCH | **No delta.** `Hub.BroadcastAll` fan-out is in place; Phase 9 `dm-write` + `read-mark` buckets follow the same per-user-bucket pattern in `ratelimit/config.go`. |
| #840 (#834 sub) | TS `Event` admits new frame kinds | `packages/api-client/src/types.ts` lines 38–68 | **No delta.** `Event` is `MessageEvent \| PresenceEvent \| ChannelEvent \| UnknownEvent`; the `UnknownEvent` arm absorbs `{type:"dm"}` and `{type:"read"}` until the Phase 9 wire-types PR adds them as named arms (per L26 "optional-first" — types-then-server). |
| #841 (#834 sub) | Go `Event` decoder admits new frame kinds | `packages/go-client/ws.go` lines 53–58 (`Event` struct), 162–186 (`decodeEvent`) | **No delta.** `Event` carries `Type` + `Raw` plus typed `Message`/`Channel` arms; unknown types surface with `Type` set and `Message`/`Channel` nil + `Raw` populated. The Phase 9 wire-types PR adds `DMMessage` + `Read` arms; existing callers see no behavior change. |
| #844 / #845 (#834 sub) | UI / CLI patterns to mirror | `apps/web/src/components/Modal.tsx` + `Modal.test.tsx`; `apps/cli/cmd/channels.go` rename command | **No delta.** Modal supports portal mount, focus trap, Escape close, optional `closeOnBackdrop`, and `initialFocusRef`. The Phase 9 web "+ Direct message" flow (L24) reuses Modal as-is. CLI command shape (`chatd channels rename`) is the template Phase 9 mirrors for `chatd dm` subcommands (L25). |

**Net result:** no Phase 8 shape drift requires Phase 9 sub-issue rewording. The Phase 9 contract is internally consistent with Phase 8 deliverables at `0742b0b`.

### Inferred (not verified at HEAD)

- The decision-log L15 entry asserts a `defaultChannel = "#general"` fallback when `?channel=` is omitted on `/ws`. The current `apps/server/internal/wsapi/handler.go` rejects an empty `?channel=` with HTTP 400 in production paths (the test-only fallback is a `testDefaultChannel = "#test-default"` constant at `handler.go:29`, gated on `cfg.ChannelLookup == nil`). The Phase 9 multi-topic-subscription sub-issue (W) is responsible for either landing the `defaultChannel = "#general"` lookup OR flipping L15 — see [ws-routing.md](ws-routing.md) for the contract this sub-issue commits to. This is the only lock-in marked "Flip if wrong" that the contract surfaces a known mismatch on; it is documented here so the W sub-issue cannot miss it.
