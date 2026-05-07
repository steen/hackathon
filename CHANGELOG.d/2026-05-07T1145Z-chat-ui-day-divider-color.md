### Added

- Day-divider rule between messages whose local dates differ. The very first message also gets a divider so the reader anchors "what day am I reading?". Labels: "Today" / "Yesterday" / short weekday for the last week / full date for older. Rendered with `role="separator"` and a long `aria-label` so SR users hear the full date even when the visible label is "Today".
- Two new unit tests in `Chat.test.tsx`: cross-midnight inserts a divider between the day-1 and day-2 rows; same-day messages produce only the leading-anchor divider.
- New Playwright assertion in `chat-ui.spec.ts` that the day-divider renders.

### Changed

- Sender colors now come from `userColor(name)` in `packages/chat-ui/src/colorize.ts` — a cyrb53 hash of the visible username mapped to OKLCH at fixed lightness/chroma. Replaces the four-class `msg__sender--user-{blue|green|purple|yellow}` palette which collided at the 5th distinct user. OKLCH is perceptually uniform, so contrast against `--bg-app` is consistent across the hue wheel (~9.5:1, AAA on every hue).
- `MessageItem` writes the sender color as an inline `style.color`. Drops the `senderId` prop (color now keyed off the visible name).
- `.msg` no longer adds horizontal padding (was 0.4rem on each side, pushing the timestamp column past the channel-header text). Hover highlight uses negative margin + matching panel padding so it bleeds to the panel edges.

### Removed

- `--user-blue`, `--user-green`, `--user-purple`, `--user-yellow` tokens and the four `.msg__sender--user-*` CSS rules — superseded by the inline OKLCH color.
- `tests/e2e/playwright/chat-ui.spec.ts` color-class assertion (now asserts inline `oklch(...)` style + distinct colors per author).

### Fixed

- `helpers.waitForChatShell` selector updated from `.sidebar strong` to `.top-bar__user-name` after the username moved into the TopBar.
