### Changed

- Web "Load older messages" trigger flips to a disabled, `aria-busy="true"`
  "Loading older messages…" state while the older-history fetch is in flight,
  so the click registers visibly instead of waiting silently for the page to
  land.
- Web older-history pagination gates the trigger on the deduped fresh-row
  count rather than the raw server page size; a full server window that
  mostly overlapped state now hides the trigger immediately instead of
  costing a second (eventually-empty) click.

### Fixed

- Web older-history fetch failures render inline next to the trigger and no
  longer displace the channel-level error banner reserved for initial-history
  and WebSocket-connect failures (#589).
