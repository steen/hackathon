### Fixed

- `apps/web/src/test-setup.ts`: TopBar.css was missing from the chat-ui CSS bundle injected into jsdom. Tests that rely on `getComputedStyle` for `.top-bar*` selectors now resolve.
- `packages/chat-ui/src/MessageComposer/MessageComposer.tsx`: clarified `aria-invalid` form. The `overCap || undefined` pattern was correct (`false || undefined === undefined` strips the attribute) but the explicit `overCap ? "true" : undefined` reads more clearly.

### Changed

- Extracted the duplicated `setRef<T>` helper from `MessageList.tsx` and `MessageComposer.tsx` into a shared `packages/chat-ui/src/setRef.ts`. Both components now import it.
- `packages/chat-ui/src/MessageItem/MessageItem.tsx`: tightened `status` prop to `MessageStatus | undefined` (was the ad-hoc `MessageStatus | "sent"` union — `"sent"` is not part of `MessageStatus`). Added a doc comment that explains absent status ≡ successfully sent and that `data-status` exposes all three values verbatim.
- `packages/chat-ui/src/MessageList/MessageList.tsx`: dropped the unused `emptyState` prop. The `showNoChannelsEmpty` + `showEmptyChannelHint` flags cover all current usages; the order is fixed inside the component, not consumer-controlled.
