### Refactor

- `apps/web` — drop the hand-written `as { kind, channel }` cast in the `useChannels` WS handler. The `Event` union includes `UnknownEvent` (`type: string`), so a discriminator check alone leaves `ev.data` as `unknown`; an `isChannelEvent` type predicate now narrows against the shared `ChannelEvent` type so a future field rename in `ChannelEvent.data` (e.g. `kind` → `op`) breaks compilation here instead of slipping past the cast. No user-visible change.
