### Added

- Web: "+ New channel" sidebar button + Rename trigger on the active channel header (hidden on `#general`), wiring `useChannels.create` and `useChannels.rename` through the existing Modal primitive. Validation mirrors the server regex client-side; submit is gated locally and server 400/403/409/429 messages render inline. (#844)
- Web: `useChannels` now subscribes to the shared chat-page WebSocket for `channel` events and merges by id (`create` → upsert, `rename` → in-place name update). On every WS open (initial + reconnect) it triggers a `reload()` as catch-up against frames missed while the snapshot was idle. (#844)

### Changed

- Web: `Chat.tsx` lifts `useChatSocket(activeChannel)` so `useChannels` and `useMessages` share a single WebSocketClient per chat-page session; `useMessages` accepts an optional external socket and parks its internal hook idle when one is provided. (#844)
