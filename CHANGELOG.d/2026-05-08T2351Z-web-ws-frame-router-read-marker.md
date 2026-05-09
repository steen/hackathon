### Added

- Web `useChatSocket` exposes typed `dm` and `read` frame slots in addition to the raw `message` event, letting Phase 9 consumers subscribe with a fully-narrowed `DMEvent` / `ReadEvent` payload (#872).
- New `useReadMarker(scope, scopeId)` hook centralises the channel/DM read-pointer advance with a 250ms trailing debounce per decision-log L22; rapid `markRead` calls collapse to one `POST /read`, and document visibility return / window focus / explicit `flush()` short-circuit the timer (#872).
