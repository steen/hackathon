# Phase 6 handoff

Date: 2026-05-07 11:58Z
Author: Claude (orchestrator session for phase-6 close-out)
Predecessor: `2026-05-07T1032Z-phase-5-handoff.md`

## Executive summary

**Phase 6 (`#768`) drained.** All twelve sub-issues #769–#780 closed via a single PR — `#797`, merged at 11:56Z, base `main`, head `feat/phase-6-chat-ui`, 24 commits preserved (no squash), +2289/−717 across 74 files. Each per-component commit's trailer carries its own `Closes #<sub-issue>` so the epic and every leaf closed atomically on merge.

The phase took the 460-line `apps/web/src/routes/Chat.tsx` monolith apart and put the pieces under a new `@hackathon/chat-ui` workspace package, restyled the app to a dark Slack-like palette, and consolidated identity + connection state into a single `TopBar` surface. `Chat.tsx` is now wiring-only (~150 lines).

Phase 6 was deliberately run as a single long-lived branch rather than per-sub-issue PRs: the work was tightly coupled (cross-component CSS tokens, shared types, JSX rearrangement), per-PR splits would have collided constantly on `Chat.tsx`, and the visual diff is only meaningful end-to-end.

## What landed by area

### Component library — `packages/chat-ui`

New workspace package with the following exports:

- Components: `Sidebar`, `ChannelsList`, `PresenceList`, `PresenceLiveRegion`, `ChannelHeader`, `MessageList`, `MessageItem`, `MessageComposer`, `TopBar`, `DayDivider`, `ConnectionBadge` (the last is internal to the package; not rendered in the new layout — see "Layout & visual" below).
- Utilities: `humanizeTimestamp`, `userColor` (cyrb53 → OKLCH), `MESSAGE_MAX_BYTES`, `IS_AT_BOTTOM_TOLERANCE_PX`.
- Styling: `tokens.css` (dark-theme design tokens) + a CSS barrel in `styles.css`. Per-component `*.css` files own their selectors; the consumer just imports the package barrel.
- Types: `ConnectionStatus` is owned by the package (replaces the legacy `ConnectionState` alias on `Chat.tsx`).

Auto-scroll-to-bottom logic and the IME / Enter / Shift+Enter / byte-counter logic were moved into the components that own them; `Chat.tsx` no longer threads scroll-state booleans through props.

### Layout & visual

- TopBar consolidates identity into one surface: `# Hackathon` workspace name, `●` connection dot (green/red), username, Sign out. The old `ConnectionBadge` row in the channel header and the duplicate username in the sidebar header are gone — connection state and identity each have a single owner.
- Sender colors derive via `cyrb53(username) → OKLCH`. Replaces the four-class palette which collided at the 5th distinct user; OKLCH gives ≥9.5:1 contrast across the hue wheel.
- Day-divider between messages whose local dates differ; first message also gets a leading divider to anchor the reading day. Labels: "Today" / "Yesterday" / short weekday / full date for older entries.
- Message rhythm tightened to match the reference screenshot's density (final tweak in `ddc982c`).
- Mobile single-column layout: sidebar capped at `40vh` so a long channel list can't push the messages region off the bottom. This was a real webkit-only failure observed during Playwright runs, not a hypothetical.

### Backend changes (server)

Two small server-side changes shipped in the same PR, both following from front-end changes:

- `GET /api/users` now returns the full registered-user directory; `usePresence` merges that into the username map so senders who have since gone offline still resolve to their username instead of a raw ULID. Without it, a message from a now-offline user displayed as a 26-char ULID rather than `alex`.
- `apps/server/internal/wsapi/handler.go` downgrades `io.EOF` and `net.ErrClosed` on ungraceful WS disconnects from `WARN` to `DEBUG`. These were firing on every browser-tab close and were drowning out real warnings during Phase 6 manual testing.

### Tests

- 160 vitest unit tests + 9 Playwright tests still pass.
- Added: 2 unit tests for the day-divider rule (cross-midnight inserts a divider; same-day does not), 4 Playwright assertions in a new `chat-ui.spec.ts` (offline-username resolves; meta-line orders `<time>` before `<sender>`; distinct senders render distinct OKLCH colors; day-divider renders).
- `tests/e2e/phase-2/web-app/auth_screens_test.go` rewritten to read both `Chat.tsx` (wiring) and the chat-ui sources (ARIA + DOM contract). The AC-2 contract is unchanged — only the file locator follows the JSX move.

### Process / cleanup

- Phase 6 GitHub epic #768 + 12 sub-issues #769–#780 created up-front (`8ad56ec`). The epic body lists the full plan; each commit closes its own leaf.
- Backwards-compat aliases (`ConnectionState`, `MessageStatus` re-exports, `IS_AT_BOTTOM_TOLERANCE_PX` re-export from `Chat.tsx`, `/api/users` 404 fallback) were deliberately removed in `04776a1` — hackathon repo, no deployed consumers.

## Verification at merge tip (`ddc982c`)

- `pnpm run lint` — clean
- `pnpm -r typecheck` — clean
- `pnpm run format:check` — clean
- `pnpm run check:workspace-exports` — ok (new package's `main` / `types` / `exports` all point at `./src/...`, per the TS workspace rule)
- `pnpm --filter ./apps/web test` (vitest): **160 passed**, 1 skipped
- `pnpm e2e:web` (Playwright, chromium + webkit): **13 passed**
- `go test ./apps/server/... ./tests/...` — green
- `golangci-lint run ./apps/server/... ./tests/...` — zero issues

## What did not land in Phase 6 (filed as follow-ups)

In `lt -p chat-ui-visual-fixes` and `lt -p chat-ui-investigation`:

- Workspace-switcher rail, DM list, member sidebar with avatars, pinned panel, global search (Ctrl+K), composer attach + emoji buttons, real avatar images. All are pure additions — none of them are blocked on anything Phase 6 shipped.
- Spacing-token extraction; deriving hover/active surfaces via `color-mix()` rather than the literal-value tokens we shipped.
- Cleanup of redundant `tsconfig.paths` / `vitest.config.alias` entries — likely redundant under `moduleResolution: Bundler`; verify post-merge before deleting.

## Repo-state notes

- `phase-6-screenshots` is an orphan branch on the GitHub remote carrying the before/after PNGs referenced from the PR body. It is **not** merged into `main` and is **not** part of any release artifact — keep it as-is or delete after the PR is no longer the freshest reference, the call is the maintainer's.
- The single-long-branch strategy worked here but is not a general default. It worked because: (a) the work was visually-coupled and per-PR review would have been low-signal until the dark theme was in; (b) `Chat.tsx` is a known conflict-magnet so per-feature PRs would have rebased on each other constantly; (c) commits were already structured per-sub-issue so the epic still got line-itemed history without N PRs of overhead. The CLAUDE.md "Don't stack PRs on open PRs" rule still holds for normal feature work.

## Standing concerns for the next session

1. **Phase 7 (deployment readiness) is queued.** Epic #786, seven active sub-issues #787–#792 + #795 (#793 + #794 closed as superseded into #792's unified ops doc). Two related follow-ups outside Phase 7 scope: #785 (`CHAT_BCRYPT_COST` env-wire — PRD §9 deviation) and #796 (`--health-probe` flag for the chat-server binary, prerequisite for adding real `HEALTHCHECK` to the Dockerfile/compose once landed). Phase 7 is sized for hackathon/homelab — single-host docker-compose, no registry, no K8s, no metrics, no in-binary TLS. Ready for `/phase-loop`.
2. **No active orchestrator loops.** The pr-review-loop auto cadence was stopped at the end of the Phase 5 close-out and was not restarted for Phase 6 (single-PR phase didn't need it). Restart with `/pr-review-loop auto` when Phase 7 worker PRs start landing.
3. **`@hackathon/chat-ui` is now a contract surface.** Any future feature that touches the chat shell (Phase 7 deployment work won't, but later UX phases will) imports from this package rather than editing `Chat.tsx`. The package's exports are the public surface; the per-component CSS files are not.

## Numbers

- Sub-issues opened in Phase 6: 12 (#769–#780)
- Sub-issues closed in Phase 6: 12 (all via PR #797 merge)
- Epics closed: 1 (#768)
- PRs merged in Phase 6: 1 (#797)
- Commits preserved on merge: 24 (no squash)
- Diff: +2289 / −717 across 74 files
- Branches still on the remote post-merge: `phase-6-screenshots` (orphan, intentional); `feat/phase-6-chat-ui` should be deletable now that #797 is merged.
