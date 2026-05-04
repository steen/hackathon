# Feature: Fix message history rendered in reverse chronological order on first load

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Tracking issue:** #99
**Status:** in progress

## Problem

On first load of a channel the message list renders newest-first / oldest-last.
After live messages start arriving via WS they append to the bottom of that
already-reversed list, so the visible thread is incoherent: a fresh user sees
old messages at the top, the *first* historical message just above the
composer, and live messages stacked below that.

## Root cause

`apps/server/internal/repo/messages.go` `ListMessages` returns rows
`ORDER BY id DESC` (newest first) — correct for the cursor-paginated REST
contract since pagination uses `?before=<ulid>`. The web client
(`apps/web/src/hooks/useMessages.ts`) does `setMessages(history)` without
reversing, then live events do `setMessages((prev) => [...prev, m])`. Result on
screen (top → bottom): newest history → oldest history → live messages.

## Acceptance criteria

- On first load of a channel, the visible message order is **oldest at top,
  newest at bottom** — the composer sits directly under the most recent
  message.
- Live messages received via WS continue to append below the most recent
  message (no regression in the live tail).
- When older history is paginated in (cursor `before`), it prepends above the
  existing top message; visible order stays oldest→newest. _Pagination wiring
  is separate work — this spec only locks the contract that the prepend path
  must honour._
- No change to the REST contract — `GET /api/channels/{id}/messages` still
  returns newest-first to match the `before` cursor semantics.
- Reopen catchup (PR #115 / #108) keeps merging server frames by id and
  ordering them oldest→newest in state — reversal must happen at the boundary,
  not on the rendered output, so the existing dedup logic stays intact.
- Optimistic send (PR #129 / #125) keeps `pending-<uuid>` entries at the
  bottom; the reversal must not flip them above older history.

## Implementation steps

1. In `apps/web/src/hooks/useMessages.ts`, reverse the initial history at the
   boundary: `setMessages([...history].reverse())`. The reopen-catchup path
   already reverses fresh additions in `mergeFetched` before appending — leave
   it alone. Pending entries are appended after history loads, so they stay at
   the tail.
2. Pagination (separate PR): when an older page lands via the `before` cursor,
   prepend the reversed page to `messages` rather than appending. Out of scope
   for this issue.

## Test plan

- `apps/web/src/hooks/useMessages.test.ts`
  - `reverses initial history (server newest-first → state oldest-first)`
  - `appends a live WS frame below the most recent history entry`
  - `optimistic pending entry sits at the bottom, below older history`
  - existing `multi-message catchup lands in chronological order` continues to
    pass — the catchup boundary already reverses.
- `apps/web/src/routes/Chat.test.tsx`
  - `renders history rows oldest→newest (composer under the newest)`
  - `a live WS frame lands below the newest history row`
- Manual: open the chat with a 3-message history, confirm the order in the
  DOM. Live broadcasts append at the bottom.

## Files touched

- `apps/web/src/hooks/useMessages.ts`
- `apps/web/src/hooks/useMessages.test.ts`
- `apps/web/src/routes/Chat.test.tsx`
- `specs/plans/phase-3/05-feature-fix-message-history-order.md` (this file)
- `CHANGELOG.d/2026-05-03T21:50Z-fix-web-history-chrono-order.md`

## Risks

- Reversing the initial history without touching the catchup-merge path could
  re-introduce the bug if a future refactor folds the two boundaries — the
  test cases cover both, so a regression there fails the suite.
- Pagination is deferred. Until that lands, scrolling above the initial 50-row
  window shows nothing; the visible order remains correct.
