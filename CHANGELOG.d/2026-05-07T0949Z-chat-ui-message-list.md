### Changed

- `MessageList` and `MessageItem` extracted from `apps/web/src/routes/Chat.tsx` into `@hackathon/chat-ui`. Auto-scroll-on-bottom logic + `IS_AT_BOTTOM_TOLERANCE_PX` move to `MessageList`; the constant is re-exported from `Chat.tsx` so existing tests keep their import path.
- `humanizeTimestamp` moves into chat-ui (`MessageItem/humanizeTimestamp.ts`); `apps/web/src/utils/formatTimestamp.ts` is dropped. `Chat.test.tsx` imports the helper from chat-ui.
- `MessageStatus` type re-exports from chat-ui; `useMessages` keeps a deprecated alias for one cycle.
- Sender names render in a deterministic 4-color palette (blue/green/purple/yellow) via `userColorClass`. Meta line order flipped to `<time> <sender> <badges>` to match the screenshot reference.

### Removed

- `apps/web/src/utils/formatTimestamp.ts` — moved.
