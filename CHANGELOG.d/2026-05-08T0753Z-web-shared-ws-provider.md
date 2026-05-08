### Refactor

- **web:** Lift the chat-page WebSocket lifecycle into a shared `useChatSocket` hook.
  `useMessages` no longer constructs its own `WebSocketClient`; it consumes the
  shared hook's connection state and a typed `subscribe` API. No user-visible
  change — same reconnect cadence, same connection-state surface, same message
  reconciliation. Sets up Phase 8's channel-create/rename frame consumers without
  duplicating the connection. (#842)
