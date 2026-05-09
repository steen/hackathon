### Added

- Web sidebar renders per-channel unread badges sourced from the server's
  `unread_count` field (Phase 9 #873). Badges render only when count > 0,
  cap displayed text at `99+`, and expose the exact count to screen readers
  via the row's aria-label.
- `useChannels` subscribes to `{type:"read", scope:"channel"}` WS frames so
  cross-tab/device read advances zero the local badge without a refetch.
- Chat shell wires `useReadMarker("channel", activeChannelId)` and posts
  `/api/channels/{id}/read` whenever the active channel has a new latest
  committed message (debounced 250ms; suppressed while the tab is hidden).
